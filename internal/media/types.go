// Package media 定义 StreamBridge 内部统一的媒体数据模型
// 所有输入协议(RTSP/RTMP/RTP/文件)解封装后都转为该模型,再由输出层封装为 FLV/HLS/WebRTC
package media

import (
	"time"
)

// CodecType 编解码类型
type CodecType int

const (
	CodecH264 CodecType = iota + 1
	CodecH265
	CodecAAC
	CodecG711A // G.711 A-law
	CodecG711U // G.711 mu-law
	CodecPCM
)

// String 字符串表示
func (c CodecType) String() string {
	switch c {
	case CodecH264:
		return "H264"
	case CodecH265:
		return "H265"
	case CodecAAC:
		return "AAC"
	case CodecG711A:
		return "G711A"
	case CodecG711U:
		return "G711U"
	case CodecPCM:
		return "PCM"
	default:
		return "Unknown"
	}
}

// Track 媒体轨道描述
type Track struct {
	Type     TrackType
	Codec    CodecType
	// 视频参数(VideoTrack 使用)
	SPS      []byte // H264 SPS / H265 VPS+SPS+PPS 合并
	PPS      []byte
	VPS      []byte // H265 专用
	Width    int
	Height   int
	FPS      float64
	// 音频参数(AudioTrack 使用)
	SampleRate   int
	Channels     int
	BitsPerSample int
}

// TrackType 轨道类型
type TrackType int

const (
	TrackVideo TrackType = iota + 1
	TrackAudio
)

// Frame 统一媒体帧
type Frame struct {
	Track      *Track
	Codec      CodecType
	IsKeyFrame bool
	Payload    []byte // NALU 或 AAC 帧数据(不含起始码)
	PTS        time.Duration // 显示时间戳
	DTS        time.Duration // 解码时间戳
}

// StreamInfo 流信息(用于统计与展示)
type StreamInfo struct {
	Source    string
	VideoCodec string
	AudioCodec string
	Width     int
	Height    int
	FPS       float64
	Bitrate   int // kbps
	StartedAt time.Time
}

// IsVideo 是否视频帧
func (f *Frame) IsVideo() bool {
	return f.Track != nil && f.Track.Type == TrackVideo
}

// IsAudio 是否音频帧
func (f *Frame) IsAudio() bool {
	return f.Track != nil && f.Track.Type == TrackAudio
}
