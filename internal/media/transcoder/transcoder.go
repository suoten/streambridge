// Package transcoder 实现 H265→H264 软件转码
// 通过 FFmpeg 子进程管道实现,无需 cgo,保持单二进制部署
//
// 工作原理:
//   1. 启动 ffmpeg 进程,stdin 读 H265 AnnexB,stdout 写 H264 AnnexB
//   2. 输入帧写入 stdin,从 stdout 读取转码后的 H264 NALU
//   3. 自动解析 SPS/PPS,生成 H264 Track
//
// 环境要求:目标机器需安装 ffmpeg(在 PATH 中可用)
package transcoder

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/streambridge/streambridge/internal/media"
)

// Transcoder H265→H264 转码器
type Transcoder struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	closed  bool

	// 输出
	outputCh chan *media.Frame
	videoTrack *media.Track

	// SPS/PPS 缓存(从 ffmpeg 输出中提取)
	sps []byte
	pps []byte
	spsSeen bool
	ppsSeen bool

	// 输入帧统计
	inputFrames  int64
	outputFrames int64
	startTime    time.Time
}

// New 创建转码器
// videoTrack 是原始 H265 轨道(用于获取分辨率信息)
func New(videoTrack *media.Track) *Transcoder {
	t := &Transcoder{
		outputCh:  make(chan *media.Frame, 256),
		startTime: time.Now(),
	}

	// 创建输出 H264 轨道(继承原始分辨率)
	t.videoTrack = &media.Track{
		Type:   media.TrackVideo,
		Codec:  media.CodecH264,
		Width:  videoTrack.Width,
		Height: videoTrack.Height,
		FPS:    videoTrack.FPS,
	}

	return t
}

// Start 启动 ffmpeg 进程
func (t *Transcoder) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel

	// ffmpeg 参数:
	// -f hevc: 输入格式为 HEVC AnnexB
	// -i -: 从 stdin 读取
	// -c:v libx264: 使用 libx264 编码 H264
	// -preset ultrafast: 最快速度(低延迟优先)
	// -tune zerolatency: 零延迟调优
	// -profile:v baseline: Baseline profile(最大兼容性,所有浏览器支持)
	// -level 3.1: 兼容 1080p
	// -bf 0: 无 B 帧(降低延迟)
	// -g 60: GOP 大小(2秒@30fps)
	// -f h264: 输出格式为 H264 AnnexB
	// -an: 无音频(音频另行处理)
	// pipe:1: 输出到 stdout
	args := []string{
		"-f", "hevc",
		"-i", "-",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-profile:v", "baseline",
		"-level", "3.1",
		"-bf", "0",
		"-g", "60",
		"-f", "h264",
		"-an",
		"pipe:1",
	}

	t.cmd = exec.CommandContext(ctx, "ffmpeg", args...)

	var err error
	t.stdin, err = t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("创建 stdin 管道失败: %w", err)
	}
	t.stdout, err = t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	t.stderr, err = t.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("创建 stderr 管道失败: %w", err)
	}

	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("启动 ffmpeg 失败: %w (请确认 ffmpeg 已安装并在 PATH 中)", err)
	}

	// 启动 stderr 读取(日志)
	t.wg.Add(1)
	go t.readStderr()

	// 启动 stdout 读取(解析 H264 NALU)
	t.wg.Add(1)
	go t.readStdout()

	log.Printf("[Transcoder] ffmpeg 已启动, PID=%d", t.cmd.Process.Pid)
	return nil
}

// WriteFrame 写入 H265 帧(AnnexB 格式)
func (t *Transcoder) WriteFrame(f *media.Frame) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("转码器已关闭")
	}
	t.mu.Unlock()

	t.inputFrames++

	// 将帧转为 AnnexB 格式写入 stdin
	// 帧的 Payload 已经是 AnnexB 格式(带起始码)
	_, err := t.stdin.Write(f.Payload)
	if err != nil {
		return fmt.Errorf("写入 ffmpeg stdin 失败: %w", err)
	}
	return nil
}

// OutputCh 返回转码后的 H264 帧通道
func (t *Transcoder) OutputCh() <-chan *media.Frame {
	return t.outputCh
}

// VideoTrack 返回转码后的 H264 轨道
func (t *Transcoder) VideoTrack() *media.Track {
	return t.videoTrack
}

// Stats 返回转码统计
func (t *Transcoder) Stats() (inputFrames, outputFrames int64, uptime time.Duration) {
	return t.inputFrames, t.outputFrames, time.Since(t.startTime)
}

// readStdout 从 ffmpeg stdout 读取 H264 AnnexB 流,拆分为 NALU 并发送帧
func (t *Transcoder) readStdout() {
	defer t.wg.Done()
	defer close(t.outputCh)

	reader := bufio.NewReaderSize(t.stdout, 256*1024)
	var frameBuf bytes.Buffer
	var frameDTS time.Duration
	var hasFrame bool

	// 逐字节扫描 AnnexB 起始码
	var buf [4096]byte
	remaining := []byte{}

	for {
		n, err := reader.Read(buf[:])
		if n > 0 {
			remaining = append(remaining, buf[:n]...)
			// 解析 NALU
			remaining = t.parseAnnexB(remaining, &frameBuf, &frameDTS, &hasFrame)
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("[Transcoder] 读取 ffmpeg stdout 错误: %v", err)
			}
			break
		}
	}
}

// parseAnnexB 从数据中解析 AnnexB NALU
// 返回未消费的剩余数据
func (t *Transcoder) parseAnnexB(data []byte, frameBuf *bytes.Buffer, frameDTS *time.Duration, hasFrame *bool) []byte {
	for {
		// 查找起始码 (00 00 00 01 或 00 00 01)
		startIdx, startLen := findStartCode(data, 0)
		if startIdx < 0 {
			// 没有找到起始码,所有数据保留
			return data
		}

		// 查找下一个起始码
		nextIdx, _ := findStartCode(data, startIdx+startLen)
		if nextIdx < 0 {
			// 没有下一个起始码,保留从当前起始码开始的数据
			return data[startIdx:]
		}

		// 提取 NALU(从起始码后到下一个起始码前)
		nalu := data[startIdx+startLen : nextIdx]
		if len(nalu) > 0 {
			t.processNALU(nalu, frameBuf, frameDTS, hasFrame)
		}

		// 移动到下一个起始码
		data = data[nextIdx:]
	}
}

// processNALU 处理单个 NALU
func (t *Transcoder) processNALU(nalu []byte, frameBuf *bytes.Buffer, frameDTS *time.Duration, hasFrame *bool) {
	if len(nalu) == 0 {
		return
	}

	naluType := media.H264NALUType(nalu)

	switch naluType {
	case media.H264NALSPS:
		t.sps = make([]byte, len(nalu))
		copy(t.sps, nalu)
		t.spsSeen = true
		// 更新轨道信息
		if w, h, fps, err := media.H264SPSResolve(nalu); err == nil {
			t.videoTrack.SPS = nalu
			t.videoTrack.Width = w
			t.videoTrack.Height = h
			t.videoTrack.FPS = fps
		}
		// SPS 不单独成帧,加入帧缓冲
		frameBuf.Write(media.AnnexBStartCode4)
		frameBuf.Write(nalu)

	case media.H264NALPPS:
		t.pps = make([]byte, len(nalu))
		copy(t.pps, nalu)
		t.ppsSeen = true
		t.videoTrack.PPS = nalu
		frameBuf.Write(media.AnnexBStartCode4)
		frameBuf.Write(nalu)

	case media.H264NALIDRSLICE:
		// IDR 关键帧:先刷新旧帧,再开始新帧
		if *hasFrame {
			t.emitFrame(frameBuf, *frameDTS, true)
			frameBuf.Reset()
		}
		frameBuf.Write(media.AnnexBStartCode4)
		frameBuf.Write(nalu)
		*hasFrame = true
		*frameDTS = time.Duration(t.outputFrames) * time.Second / time.Duration(maxFPS(t.videoTrack.FPS))

	case media.H264NALSLICE, media.H264NALDPA, media.H264NALDPB, media.H264NALDPC:
		// P/B 帧:加入当前帧
		if *hasFrame {
			frameBuf.Write(media.AnnexBStartCode4)
			frameBuf.Write(nalu)
		}

	case media.H264NALAUD:
		// AUD(Access Unit Delimiter):帧边界
		if *hasFrame {
			t.emitFrame(frameBuf, *frameDTS, false)
			frameBuf.Reset()
			*hasFrame = false
		}
		// AUD 本身加入新帧
		frameBuf.Write(media.AnnexBStartCode4)
		frameBuf.Write(nalu)
		*hasFrame = true
		*frameDTS = time.Duration(t.outputFrames) * time.Second / time.Duration(maxFPS(t.videoTrack.FPS))

	default:
		// 其他 NALU(SEI 等):加入当前帧
		if *hasFrame {
			frameBuf.Write(media.AnnexBStartCode4)
			frameBuf.Write(nalu)
		}
	}
}

// emitFrame 发送一帧到输出通道
func (t *Transcoder) emitFrame(frameBuf *bytes.Buffer, dts time.Duration, isKey bool) {
	if frameBuf.Len() == 0 {
		return
	}

	// 复制帧数据
	payload := make([]byte, frameBuf.Len())
	copy(payload, frameBuf.Bytes())

	frame := &media.Frame{
		Track:      t.videoTrack,
		Codec:      media.CodecH264,
		IsKeyFrame: isKey,
		Payload:    payload,
		PTS:        dts,
		DTS:        dts,
	}

	t.outputFrames++

	select {
	case t.outputCh <- frame:
	default:
		// 通道满,丢帧(避免阻塞)
		log.Printf("[Transcoder] 输出通道满,丢帧")
	}
}

// readStderr 读取 ffmpeg stderr 输出(用于调试)
func (t *Transcoder) readStderr() {
	defer t.wg.Done()
	scanner := bufio.NewScanner(t.stderr)
	for scanner.Scan() {
		// 只在 debug 模式下打印 ffmpeg 日志
		// log.Printf("[ffmpeg] %s", scanner.Text())
	}
}

// Close 关闭转码器
func (t *Transcoder) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	// 关闭 stdin(让 ffmpeg 自然退出)
	if t.stdin != nil {
		t.stdin.Close()
	}

	// 取消上下文(强制杀死 ffmpeg)
	if t.cancel != nil {
		t.cancel()
	}

	// 等待 ffmpeg 退出
	if t.cmd != nil {
		t.cmd.Wait()
	}

	// 等待读取 goroutine 退出
	t.wg.Wait()

	log.Printf("[Transcoder] 已关闭, 输入帧=%d 输出帧=%d 运行时长=%v",
		t.inputFrames, t.outputFrames, time.Since(t.startTime))
	return nil
}

// IsAvailable 检查 ffmpeg 是否可用
func IsAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// findStartCode 在数据中查找 AnnexB 起始码
// 返回起始码位置和长度(3 或 4),未找到返回 -1, 0
func findStartCode(data []byte, offset int) (int, int) {
	for i := offset; i < len(data)-2; i++ {
		if data[i] == 0 && data[i+1] == 0 {
			if data[i+2] == 1 {
				return i, 3
			}
			if i+3 < len(data) && data[i+2] == 0 && data[i+3] == 1 {
				return i, 4
			}
		}
	}
	return -1, 0
}

// maxFPS 返回 FPS,最小 1
func maxFPS(fps float64) float64 {
	if fps <= 0 {
		return 25
	}
	return fps
}
