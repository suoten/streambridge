package input

import (
	"github.com/streambridge/streambridge/internal/input/file"
	"github.com/streambridge/streambridge/internal/input/http"
	"github.com/streambridge/streambridge/internal/input/rtmp"
	"github.com/streambridge/streambridge/internal/input/rtsp"
	"github.com/streambridge/streambridge/internal/input/rtp"
)

// 构造函数(避免在 source.go 中循环 import)

// NewRTSPClient 创建 RTSP 客户端
func NewRTSPClient() Source { return rtsp.New() }

// NewRTMPClient 创建 RTMP 客户端
func NewRTMPClient() Source { return rtmp.New() }

// NewRTPReceiver 创建 RTP 接收器
func NewRTPReceiver() Source { return rtp.New() }

// NewFilePlayer 创建文件播放器
func NewFilePlayer() Source { return file.New() }

// NewHTTPInput 创建 HTTP 输入器
func NewHTTPInput() Source { return http.New() }
