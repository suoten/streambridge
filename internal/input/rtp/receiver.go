// RTP/PS 接收器(对接 GB28181 平台媒体转发)
// StreamBridge 不做 SIP 信令,只接收上游平台 INVITE 时转发过来的 RTP/PS 媒体流
package rtp

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"

	"github.com/streambridge/streambridge/internal/media"
)

// Receiver RTP/PS 媒体接收器
type Receiver struct {
	conn        *net.UDPConn
	videoTrack  *media.Track
	audioTrack  *media.Track
	url         string
	frameCh     chan *media.Frame
	started     atomic.Bool
	port        int
	psDepack    *PSDepacketizer
	startTime   time.Time
}

// New 创建 RTP 接收器
func New() *Receiver {
	return &Receiver{
		frameCh: make(chan *media.Frame, 256),
	}
}

// Start 在指定端口监听 RTP/PS 流
// URL 格式: rtp://0.0.0.0:20000
func (r *Receiver) Start(ctx context.Context, url string) (<-chan *media.Frame, error) {
	r.url = url
	r.startTime = time.Now()

	host, port, err := parseRTPURL(url)
	if err != nil {
		return nil, err
	}
	r.port = port

	addr := &net.UDPAddr{IP: net.ParseIP(host), Port: port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("监听 UDP %s:%d 失败: %w", host, port, err)
	}
	r.conn = conn
	r.psDepack = NewPSDepacketizer()
	r.videoTrack = &media.Track{
		Type: media.TrackVideo,
		Codec: media.CodecH264,
		FPS: 25,
	}
	r.audioTrack = &media.Track{
		Type: media.TrackAudio,
		Codec: media.CodecAAC,
		SampleRate: 8000,
		Channels: 1,
	}

	r.started.Store(true)
	go r.run(ctx)
	return r.frameCh, nil
}

func (r *Receiver) run(ctx context.Context) {
	defer close(r.frameCh)
	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		r.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, _, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if !r.started.Load() {
				return
			}
			continue
		}
		r.handlePacket(buf[:n])
	}
}

func (r *Receiver) handlePacket(data []byte) {
	pkt := &rtp.Packet{}
	if err := pkt.Unmarshal(data); err != nil {
		return
	}
	// PS 解封装
	frames := r.psDepack.Depacketize(pkt)
	for _, f := range frames {
		r.sendFrame(f)
	}
}

func (r *Receiver) sendFrame(f *media.Frame) {
	if f.Track == nil {
		if f.IsVideo() {
			f.Track = r.videoTrack
		} else {
			f.Track = r.audioTrack
		}
	}
	select {
	case r.frameCh <- f:
	default:
	}
}

// Tracks 返回轨道
func (r *Receiver) Tracks() (video, audio *media.Track) {
	return r.videoTrack, r.audioTrack
}

// Stop 停止
func (r *Receiver) Stop() error {
	r.started.Store(false)
	if r.conn != nil {
		r.conn.Close()
	}
	return nil
}

// Type 类型
func (r *Receiver) Type() string { return "rtp" }

// SourceURL 源 URL
func (r *Receiver) SourceURL() string { return r.url }

func parseRTPURL(url string) (string, int, error) {
	// rtp://0.0.0.0:20000
	if len(url) < 7 || url[:6] != "rtp://" {
		return "", 0, fmt.Errorf("非法 RTP URL: %s", url)
	}
	rest := url[6:]
	idx := -1
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i] == ':' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", 0, fmt.Errorf("URL 缺少端口: %s", url)
	}
	host := rest[:idx]
	port := 0
	for i := idx + 1; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return "", 0, fmt.Errorf("URL 端口非法: %s", url)
		}
		port = port*10 + int(rest[i]-'0')
	}
	if port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("端口越界: %d", port)
	}
	if host == "" {
		host = "0.0.0.0"
	}
	return host, port, nil
}
