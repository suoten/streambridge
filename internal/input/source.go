// Package input 实现所有输入协议的统一接口
// 所有输入源(RTSP/RTMP/RTP/文件)实现 Source 接口,输出统一的 media.Frame
package input

import (
	"context"

	"github.com/streambridge/streambridge/internal/media"
)

// Source 输入源接口
// 所有输入协议实现该接口,由 session.Manager 调用
type Source interface {
	// Start 开始拉流,frameCh 返回媒体帧,直到 Stop 或 ctx.Done
	Start(ctx context.Context, url string) (<-chan *media.Frame, error)
	// Tracks 返回轨道信息(Start 后可用)
	Tracks() (video, audio *media.Track)
	// Stop 停止拉流
	Stop() error
	// Type 输入类型名
	Type() string
	// SourceURL 原始 URL
	SourceURL() string
}

// ParseURL 根据协议头选择输入源
func ParseURL(url string) Source {
	switch {
	case hasPrefix(url, "rtsp://"):
		return NewRTSPClient()
	case hasPrefix(url, "rtsps://"):
		return NewRTSPClient()
	case hasPrefix(url, "rtmp://"):
		return NewRTMPClient()
	case hasPrefix(url, "rtp://"):
		return NewRTPReceiver()
	case hasPrefix(url, "file://") || hasPrefix(url, "/"):
		return NewFilePlayer()
	case hasPrefix(url, "http://") || hasPrefix(url, "https://"):
		return NewHTTPInput()
	}
	// 默认当 RTSP 处理
	return NewRTSPClient()
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
