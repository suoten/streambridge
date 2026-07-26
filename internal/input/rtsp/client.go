// RTSP 客户端,基于 gortsplib v4
package rtsp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtph265"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtpmpeg4audio"
	"github.com/pion/rtp"

	m "github.com/streambridge/streambridge/internal/media"
)

// 视频时钟频率(H264/H265 RTP 标准为 90000)
const videoClockRate = 90000

// Client RTSP 输入客户端
type Client struct {
	client      gortsplib.Client
	videoTrack  *m.Track
	audioTrack  *m.Track
	url         string
	frameCh     chan *m.Frame
	h264Decoder *rtph264.Decoder
	h265Decoder *rtph265.Decoder
	aacDecoder  *rtpmpeg4audio.Decoder
	started     atomic.Bool
	droppedFrames atomic.Int64

	// 调试统计
	rtpPacketsIn     atomic.Int64
	decodeErrors     atomic.Int64
	framesDecoded    atomic.Int64
	lastStatsLog     time.Time
	lastRtpPacketsIn int64
	lastFramesDecoded int64

	// 时间戳基准(用于将绝对 RTP 时间戳转为相对时间)
	// RTP 时间戳是 32 位无符号,会回绕;用 uint32 减法天然处理单次回绕
	tsMu          sync.Mutex
	videoBaseTS   uint32
	videoBaseSet  bool
	audioBaseTS   uint32
	audioBaseSet  bool
	audioClockRate uint32 // AAC 通常是采样率(如 44100/48000)
}

// New 创建 RTSP 客户端
func New() *Client { return &Client{} }

// Start 连接 RTSP 服务器并开始拉流
func (c *Client) Start(ctx context.Context, url string) (<-chan *m.Frame, error) {
	c.url = url
	c.frameCh = make(chan *m.Frame, 512)

	if err := c.connect(); err != nil {
		return nil, err
	}

	c.started.Store(true)
	go c.run(ctx)
	return c.frameCh, nil
}

// connect 建立RTSP连接并设置轨道
func (c *Client) connect() error {
	// 重置时间戳基准(重连后重新基准)
	c.tsMu.Lock()
	c.videoBaseSet = false
	c.audioBaseSet = false
	c.tsMu.Unlock()

	u, err := base.ParseURL(c.url)
	if err != nil {
		return fmt.Errorf("解析 RTSP URL 失败: %w", err)
	}

	// 配置超时
	c.client = gortsplib.Client{
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
	}

	// 优先 TCP,失败再尝试 UDP
	tcp := gortsplib.TransportTCP
	c.client.Transport = &tcp
	transportUsed := "TCP"
	if err := c.client.Start(u.Scheme, u.Host); err != nil {
		udp := gortsplib.TransportUDP
		c.client.Transport = &udp
		transportUsed = "UDP"
		if err2 := c.client.Start(u.Scheme, u.Host); err2 != nil {
			return fmt.Errorf("RTSP 连接失败(TCP/UDP): %w / %v", err, err2)
		}
	}
	log.Printf("[RTSP] 传输协议: %s, URL: %s", transportUsed, c.url)

	desc, _, err := c.client.Describe(u)
	if err != nil {
		c.client.Close()
		return fmt.Errorf("DESCRIBE 失败: %w", err)
	}

	// 遍历所有媒体,寻找 H264/H265 视频与 AAC 音频
	for _, medi := range desc.Medias {
		// H264 视频
		var h264 *format.H264
		if medi.FindFormat(&h264) {
			if _, err := c.client.Setup(u, medi, 0, 0); err != nil {
				c.client.Close()
				return fmt.Errorf("SETUP(H264) 失败: %w", err)
			}
			c.videoTrack = &m.Track{
				Type:  m.TrackVideo,
				Codec: m.CodecH264,
				SPS:   h264.SPS,
				PPS:   h264.PPS,
				FPS:   25,
			}
			if w, h, fps, err := m.H264SPSResolve(h264.SPS); err == nil {
				c.videoTrack.Width = w
				c.videoTrack.Height = h
				c.videoTrack.FPS = fps
			}
			c.h264Decoder = &rtph264.Decoder{}
			if err := c.h264Decoder.Init(); err != nil {
				c.client.Close()
				return fmt.Errorf("H264 解码器初始化失败: %w", err)
			}
			c.client.OnPacketRTP(medi, h264, func(pkt *rtp.Packet) { c.onH264(pkt) })
			continue
		}

		// H265 视频
		var h265 *format.H265
		if medi.FindFormat(&h265) {
			if _, err := c.client.Setup(u, medi, 0, 0); err != nil {
				c.client.Close()
				return fmt.Errorf("SETUP(H265) 失败: %w", err)
			}
			c.videoTrack = &m.Track{
				Type:  m.TrackVideo,
				Codec: m.CodecH265,
				VPS:   h265.VPS,
				SPS:   h265.SPS,
				PPS:   h265.PPS,
				FPS:   25,
			}
			c.h265Decoder = &rtph265.Decoder{}
			if err := c.h265Decoder.Init(); err != nil {
				c.client.Close()
				return fmt.Errorf("H265 解码器初始化失败: %w", err)
			}
			c.client.OnPacketRTP(medi, h265, func(pkt *rtp.Packet) { c.onH265(pkt) })
			continue
		}

		// AAC 音频
		var aac *format.MPEG4Audio
		if medi.FindFormat(&aac) {
			if _, err := c.client.Setup(u, medi, 0, 0); err != nil {
				// 音频 Setup 失败不致命,继续
				continue
			}
			clockRate := uint32(aac.ClockRate())
			if clockRate == 0 {
				clockRate = 44100
			}
			c.audioClockRate = clockRate
			c.audioTrack = &m.Track{
				Type:       m.TrackAudio,
				Codec:      m.CodecAAC,
				SampleRate: int(clockRate),
				Channels:   2,
			}
			c.aacDecoder = &rtpmpeg4audio.Decoder{}
			if err := c.aacDecoder.Init(); err != nil {
				c.audioTrack = nil
				continue
			}
			c.client.OnPacketRTP(medi, aac, func(pkt *rtp.Packet) { c.onAAC(pkt) })
		}
	}

	if c.videoTrack == nil {
		c.client.Close()
		return fmt.Errorf("RTSP 流中未找到视频轨道")
	}

	if _, err := c.client.Play(nil); err != nil {
		c.client.Close()
		return fmt.Errorf("PLAY 失败: %w", err)
	}
	return nil
}

func (c *Client) run(ctx context.Context) {
	defer close(c.frameCh)

	reconnectDelay := 2 * time.Second
	maxReconnectDelay := 30 * time.Second

	for {
		// 使用 Wait() 等待连接断开或上下文取消
		waitCh := make(chan error, 1)
		go func() { waitCh <- c.client.Wait() }()

		select {
		case <-ctx.Done():
			c.started.Store(false)
			c.client.Close()
			return
		case err := <-waitCh:
			if err != nil {
				log.Printf("[RTSP] 连接断开: %v,尝试重连 %s", err, c.url)
			}
			c.started.Store(false)
			c.client.Close()

			if ctx.Err() != nil {
				return
			}

			// 重连循环
			reconnected := false
			for retry := 0; ; retry++ {
				select {
				case <-ctx.Done():
					return
				default:
				}
				time.Sleep(reconnectDelay)
				if err := c.connect(); err != nil {
					log.Printf("[RTSP] 重连失败(%d): %v", retry+1, err)
					reconnectDelay = reconnectDelay * 2
					if reconnectDelay > maxReconnectDelay {
						reconnectDelay = maxReconnectDelay
					}
					continue
				}
				log.Printf("[RTSP] 重连成功 %s", c.url)
				c.started.Store(true)
				reconnectDelay = 2 * time.Second
				reconnected = true
				break
			}
			if !reconnected {
				return
			}
		}
	}
}

// relativeVideoTS 将绝对 RTP 视频时间戳转为相对时间(秒)
// 处理 32 位回绕:uint32 减法天然处理单次回绕
func (c *Client) relativeVideoTS(ts uint32) time.Duration {
	c.tsMu.Lock()
	defer c.tsMu.Unlock()
	if !c.videoBaseSet {
		c.videoBaseTS = ts
		c.videoBaseSet = true
		return 0
	}
	diff := ts - c.videoBaseTS // uint32 减法,天然处理回绕
	return time.Duration(diff) * time.Second / videoClockRate
}

// relativeAudioTS 将绝对 RTP 音频时间戳转为相对时间
func (c *Client) relativeAudioTS(ts uint32) time.Duration {
	c.tsMu.Lock()
	defer c.tsMu.Unlock()
	if !c.audioBaseSet {
		c.audioBaseTS = ts
		c.audioBaseSet = true
		return 0
	}
	diff := ts - c.audioBaseTS
	clockRate := c.audioClockRate
	if clockRate == 0 {
		clockRate = 44100
	}
	return time.Duration(diff) * time.Second / time.Duration(clockRate)
}

func (c *Client) onH264(pkt *rtp.Packet) {
	if !c.started.Load() {
		return
	}
	c.rtpPacketsIn.Add(1)
	nalus, err := c.h264Decoder.Decode(pkt)
	if err != nil {
		// ErrMorePacketsNeeded 是正常的分片累积信号,不是真正的错误
		if !errors.Is(err, rtph264.ErrMorePacketsNeeded) {
			c.decodeErrors.Add(1)
			if c.decodeErrors.Load()%100 == 1 {
				log.Printf("[RTSP] H264 解码错误(第 %d 次): %v", c.decodeErrors.Load(), err)
			}
		}
		return
	}
	if len(nalus) == 0 {
		return
	}
	c.framesDecoded.Add(1)

	// 从 in-band NALU 提取/更新 SPS/PPS
	// 始终用 in-band SPS/PPS 覆盖 DESCRIBE 的值,因为某些摄像头的 DESCRIBE SPS 可能不完整
	spsUpdated := false
	ppsUpdated := false
	for _, n := range nalus {
		if m.H264IsSPS(n) && len(n) > len(c.videoTrack.SPS) {
			c.videoTrack.SPS = n
			spsUpdated = true
			if w, h, fps, err := m.H264SPSResolve(n); err == nil {
				c.videoTrack.Width = w
				c.videoTrack.Height = h
				c.videoTrack.FPS = fps
			}
		}
		if m.H264IsPPS(n) && len(n) > len(c.videoTrack.PPS) {
			c.videoTrack.PPS = n
			ppsUpdated = true
		}
	}
	if spsUpdated || ppsUpdated {
		log.Printf("[RTSP] 从 in-band NALU 提取 SPS/PPS: SPS=%d bytes, PPS=%d bytes",
			len(c.videoTrack.SPS), len(c.videoTrack.PPS))
	}

	keyFrame := false
	for _, n := range nalus {
		if m.H264IsKeyFrame(n) {
			keyFrame = true
			break
		}
	}

	dur := c.relativeVideoTS(pkt.Timestamp)
	c.sendFrame(&m.Frame{
		Track:      c.videoTrack,
		Codec:      m.CodecH264,
		IsKeyFrame: keyFrame,
		Payload:    m.H264EncodeNALUs(nalus),
		PTS:        dur,
		DTS:        dur,
	})

	// 每 5 秒输出一次统计
	c.logStats()
}

func (c *Client) onH265(pkt *rtp.Packet) {
	if !c.started.Load() {
		return
	}
	nalus, err := c.h265Decoder.Decode(pkt)
	if err != nil || len(nalus) == 0 {
		return
	}

	// 从 in-band NALU 提取 VPS/SPS/PPS
	for _, n := range nalus {
		naluType := (n[0] >> 1) & 0x3F
		switch naluType {
		case 32: // VPS
			if len(c.videoTrack.VPS) == 0 {
				c.videoTrack.VPS = n
			}
		case 33: // SPS
			if len(c.videoTrack.SPS) == 0 {
				c.videoTrack.SPS = n
			}
		case 34: // PPS
			if len(c.videoTrack.PPS) == 0 {
				c.videoTrack.PPS = n
			}
		}
	}

	keyFrame := false
	for _, n := range nalus {
		if m.H265IsKeyFrame(n) {
			keyFrame = true
			break
		}
	}

	dur := c.relativeVideoTS(pkt.Timestamp)
	c.sendFrame(&m.Frame{
		Track:      c.videoTrack,
		Codec:      m.CodecH265,
		IsKeyFrame: keyFrame,
		Payload:    m.H265EncodeNALUs(nalus),
		PTS:        dur,
		DTS:        dur,
	})
}

func (c *Client) onAAC(pkt *rtp.Packet) {
	if !c.started.Load() || c.audioTrack == nil {
		return
	}
	aus, err := c.aacDecoder.Decode(pkt)
	if err != nil || len(aus) == 0 {
		return
	}
	baseDur := c.relativeAudioTS(pkt.Timestamp)
	for i, au := range aus {
		// 每个 AU 间隔 1024 个采样
		auDur := baseDur + time.Duration(i)*1024*time.Second/time.Duration(c.audioTrack.SampleRate)
		c.sendFrame(&m.Frame{
			Track:   c.audioTrack,
			Codec:   m.CodecAAC,
			Payload: au,
			PTS:     auDur,
			DTS:     auDur,
		})
	}
}

func (c *Client) sendFrame(f *m.Frame) {
	select {
	case c.frameCh <- f:
	default:
		c.droppedFrames.Add(1)
	}
}

// logStats 定期输出 RTP 接收/解码统计
func (c *Client) logStats() {
	now := time.Now()
	if c.lastStatsLog.IsZero() {
		c.lastStatsLog = now
		return
	}
	if now.Sub(c.lastStatsLog) < 5*time.Second {
		return
	}
	elapsed := now.Sub(c.lastStatsLog).Seconds()
	packets := c.rtpPacketsIn.Load()
	frames := c.framesDecoded.Load()
	errors := c.decodeErrors.Load()
	dropped := c.droppedFrames.Load()
	// 计算增量速率
	lastPackets := c.lastRtpPacketsIn
	lastFrames := c.lastFramesDecoded
	pktRate := float64(packets-lastPackets) / elapsed
	frameRate := float64(frames-lastFrames) / elapsed
	c.lastRtpPacketsIn = packets
	c.lastFramesDecoded = frames
	log.Printf("[RTSP] 统计(%.0fs): RTP包=%d 解码帧=%d 错误=%d 丢弃=%d | 包率=%.1f/s 帧率=%.1f/s",
		elapsed, packets, frames, errors, dropped, pktRate, frameRate)
	c.lastStatsLog = now
}

// Tracks 返回轨道
func (c *Client) Tracks() (video, audio *m.Track) {
	return c.videoTrack, c.audioTrack
}

// Stop 停止
func (c *Client) Stop() error {
	c.started.Store(false)
	c.client.Close()
	return nil
}

// Type 类型
func (c *Client) Type() string { return "rtsp" }

// SourceURL 源 URL
func (c *Client) SourceURL() string { return c.url }

// description 包用于 RTSP DESCRIBE 响应解析
var _ = description.Session{}
