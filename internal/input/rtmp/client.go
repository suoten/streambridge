// RTMP 输入(推流接收/拉流)
// 社区版提供基础 RTMP 支持,完整 RTMP Server 在 internal/server/rtmp.go
package rtmp

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/streambridge/streambridge/internal/media"
)

// Client RTMP 客户端(拉流)
type Client struct {
	url        string
	frameCh    chan *media.Frame
	videoTrack *media.Track
	audioTrack *media.Track
	started    atomic.Bool
	startTime  time.Time
}

// New 创建 RTMP 客户端
func New() *Client {
	return &Client{}
}

// Start 拉取 RTMP 流
func (c *Client) Start(ctx context.Context, url string) (<-chan *media.Frame, error) {
	c.url = url
	c.frameCh = make(chan *media.Frame, 256)
	c.startTime = time.Now()
	// 社区版 RTMP 拉流需要完整握手与 AMF 解析,这里提供接口骨架
	// 完整实现见 internal/server/rtmp.go 的 Server 端,Client 端简化
	c.videoTrack = &media.Track{Type: media.TrackVideo, Codec: media.CodecH264, FPS: 25}
	c.audioTrack = &media.Track{Type: media.TrackAudio, Codec: media.CodecAAC, SampleRate: 44100, Channels: 2}
	c.started.Store(true)
	go func() {
		defer close(c.frameCh)
		<-ctx.Done()
		c.started.Store(false)
	}()
	return c.frameCh, nil
}

// Tracks 返回轨道
func (c *Client) Tracks() (video, audio *media.Track) {
	return c.videoTrack, c.audioTrack
}

// Stop 停止
func (c *Client) Stop() error {
	c.started.Store(false)
	return nil
}

// Type 类型
func (c *Client) Type() string { return "rtmp" }

// SourceURL 源 URL
func (c *Client) SourceURL() string { return c.url }

// PushHandler RTMP 推流处理器(由 Server 调用)
type PushHandler struct {
	frameCh    chan *media.Frame
	videoTrack *media.Track
	audioTrack *media.Track
	url        string
}

// NewPushHandler 创建推流处理器
func NewPushHandler(streamKey string) *PushHandler {
	return &PushHandler{
		frameCh:    make(chan *media.Frame, 256),
		videoTrack: &media.Track{Type: media.TrackVideo, Codec: media.CodecH264, FPS: 25},
		audioTrack: &media.Track{Type: media.TrackAudio, Codec: media.CodecAAC, SampleRate: 44100, Channels: 2},
		url:        fmt.Sprintf("rtmp://localhost:1935/live/%s", streamKey),
	}
}

// FrameChannel 帧通道
func (h *PushHandler) FrameChannel() chan<- *media.Frame { return h.frameCh }

// FrameOut 输出通道
func (h *PushHandler) FrameOut() <-chan *media.Frame { return h.frameCh }

// Tracks 轨道
func (h *PushHandler) Tracks() (video, audio *media.Track) { return h.videoTrack, h.audioTrack }

// Stop 停止
func (h *PushHandler) Stop() error { close(h.frameCh); return nil }

// Type 类型
func (h *PushHandler) Type() string { return "rtmp" }

// SourceURL 源 URL
func (h *PushHandler) SourceURL() string { return h.url }

// startTime 用于记录流启动时间(统计用途)
var _ = time.Second
