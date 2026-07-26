// 文件输入:把录像文件(MP4/FLV)作为输入源播放
// 注意: 这是"播放"功能,不是"录像"功能
package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/streambridge/streambridge/internal/media"
)

// Player 文件播放器
type Player struct {
	url        string
	frameCh    chan *media.Frame
	videoTrack *media.Track
	audioTrack *media.Track
	started    atomic.Bool
	loop       bool
	startTime  time.Time
}

// New 创建文件播放器
func New() *Player {
	return &Player{}
}

// Start 打开文件并开始推送帧
func (p *Player) Start(ctx context.Context, url string) (<-chan *media.Frame, error) {
	p.url = url
	p.frameCh = make(chan *media.Frame, 256)
	p.startTime = time.Now()

	path := strings.TrimPrefix(url, "file://")
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".flv":
		return p.startFLV(ctx, path)
	case ".mp4", ".m4v":
		return p.startMP4(ctx, path)
	case ".h264", ".264":
		return p.startAnnexB(ctx, path, true)
	case ".h265", ".265":
		return p.startAnnexB(ctx, path, false)
	default:
		return nil, fmt.Errorf("不支持的视频格式: %s", ext)
	}
}

// startFLV 播放 FLV 文件
func (p *Player) startFLV(ctx context.Context, path string) (<-chan *media.Frame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	if len(data) < 13 || data[0] != 'F' || data[1] != 'L' || data[2] != 'V' {
		return nil, fmt.Errorf("非法 FLV 文件头")
	}
	p.videoTrack = &media.Track{Type: media.TrackVideo, Codec: media.CodecH264, FPS: 25}
	p.audioTrack = &media.Track{Type: media.TrackAudio, Codec: media.CodecAAC, SampleRate: 44100, Channels: 2}
	p.started.Store(true)
	go p.playFLV(ctx, data)
	return p.frameCh, nil
}

func (p *Player) playFLV(ctx context.Context, data []byte) {
	defer close(p.frameCh)
	// 跳过 9 字节头 + 4 字节 prev size
	pos := 13
	var lastTS uint32
	for pos < len(data)-11 {
		select {
		case <-ctx.Done():
			return
		default:
		}
		tagType := data[pos]
		dataSize := uint32(data[pos+1])<<16 | uint32(data[pos+2])<<8 | uint32(data[pos+3])
		ts := uint32(data[pos+4])<<16 | uint32(data[pos+5])<<8 | uint32(data[pos+6])
		ts |= uint32(data[pos+7]) << 24
		body := data[pos+11 : pos+11+int(dataSize)]
		switch tagType {
		case 9: // video
			p.parseFLVVideo(body, ts)
		case 8: // audio
			p.parseFLVAudio(body, ts)
		}
		// 模拟时间轴
		if ts > lastTS {
			dur := time.Duration(ts-lastTS) * time.Millisecond
			time.Sleep(dur)
			lastTS = ts
		}
		pos += 11 + int(dataSize) + 4
	}
}

func (p *Player) parseFLVVideo(body []byte, ts uint32) {
	if len(body) < 5 {
		return
	}
	codecID := body[0] & 0x0F
	packetType := body[1]
	dur := time.Duration(ts) * time.Millisecond
	if codecID == 7 && packetType == 0 {
		// AVC SeqHeader
		return
	}
	if codecID == 7 && packetType == 1 {
		// AVC NALU
		naluData := body[5:]
		nalus := media.H264SplitNALUs(naluData)
		if len(nalus) == 0 {
			// 可能是 AVCC 格式(4字节长度前缀)
			nalus = parseAVCC(naluData)
		}
		for _, n := range nalus {
			if media.H264IsSPS(n) {
				p.videoTrack.SPS = n
				if w, h, fps, err := media.H264SPSResolve(n); err == nil {
					p.videoTrack.Width = w
					p.videoTrack.Height = h
					p.videoTrack.FPS = fps
				}
			}
			if media.H264IsPPS(n) {
				p.videoTrack.PPS = n
			}
		}
		p.send(&media.Frame{
			Track:      p.videoTrack,
			Codec:      media.CodecH264,
			IsKeyFrame: body[0]>>4 == 1,
			Payload:    media.H264EncodeNALUs(nalus),
			PTS:        dur,
			DTS:        dur,
		})
	}
}

func (p *Player) parseFLVAudio(body []byte, ts uint32) {
	if len(body) < 2 {
		return
	}
	soundFormat := body[0] >> 4
	dur := time.Duration(ts) * time.Millisecond
	if soundFormat == 10 { // AAC
		if body[1] == 0 {
			return // AAC SeqHeader
		}
		p.send(&media.Frame{
			Track:   p.audioTrack,
			Codec:   media.CodecAAC,
			Payload: body[2:],
			PTS:     dur,
			DTS:     dur,
		})
	}
}

// startAnnexB 播放 H264/H265 裸流
func (p *Player) startAnnexB(ctx context.Context, path string, isH264 bool) (<-chan *media.Frame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	codec := media.CodecH264
	if !isH264 {
		codec = media.CodecH265
	}
	p.videoTrack = &media.Track{Type: media.TrackVideo, Codec: codec, FPS: 25}
	p.started.Store(true)
	go p.playAnnexB(ctx, data, isH264)
	return p.frameCh, nil
}

func (p *Player) playAnnexB(ctx context.Context, data []byte, isH264 bool) {
	defer close(p.frameCh)
	// 按帧推送(简化:每 40ms 一帧)
	var nalus [][]byte
	if isH264 {
		nalus = media.H264SplitNALUs(data)
	} else {
		nalus = media.H265SplitNALUs(data)
	}
	for i, n := range nalus {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var keyFrame bool
		if isH264 {
			if media.H264IsSPS(n) {
				p.videoTrack.SPS = n
				if w, h, fps, err := media.H264SPSResolve(n); err == nil {
					p.videoTrack.Width = w
					p.videoTrack.Height = h
					p.videoTrack.FPS = fps
				}
			}
			if media.H264IsPPS(n) {
				p.videoTrack.PPS = n
			}
			keyFrame = media.H264IsKeyFrame(n)
		} else {
			keyFrame = media.H265IsKeyFrame(n)
		}
		dur := time.Duration(i) * 40 * time.Millisecond
		p.send(&media.Frame{
			Track:      p.videoTrack,
			Codec:      p.videoTrack.Codec,
			IsKeyFrame: keyFrame,
			Payload:    media.H264EncodeNALUs([][]byte{n}),
			PTS:        dur,
			DTS:        dur,
		})
		time.Sleep(40 * time.Millisecond)
	}
}

// startMP4 播放 MP4 文件(简化:不支持直接解析,需要 ffmpeg remux)
// 社区版建议先把 MP4 转 FLV 再用
func (p *Player) startMP4(ctx context.Context, path string) (<-chan *media.Frame, error) {
	// 简化:MP4 解析需要 box 解析器,社区版暂用 FLV 通道
	// 实际生产可调用 mp4ff 或 mp4parse
	return nil, fmt.Errorf("MP4 直接解析暂不支持,请先用 ffmpeg -i input.mp4 -c copy output.flv 转封装")
}

func parseAVCC(data []byte) [][]byte {
	var nalus [][]byte
	for len(data) > 4 {
		n := int(uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3]))
		data = data[4:]
		if n > len(data) || n <= 0 {
			break
		}
		nalus = append(nalus, data[:n])
		data = data[n:]
	}
	return nalus
}

func (p *Player) send(f *media.Frame) {
	if !p.started.Load() {
		return
	}
	select {
	case p.frameCh <- f:
	default:
	}
}

// Tracks 返回轨道
func (p *Player) Tracks() (video, audio *media.Track) {
	return p.videoTrack, p.audioTrack
}

// Stop 停止
func (p *Player) Stop() error {
	p.started.Store(false)
	return nil
}

// Type 类型
func (p *Player) Type() string { return "file" }

// SourceURL 源 URL
func (p *Player) SourceURL() string { return p.url }
