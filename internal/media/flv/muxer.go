// Package flv 实现 FLV 封装与解封装
// 参考: Adobe Flash Video File Format Specification v10.1
package flv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/streambridge/streambridge/internal/media"
)

// FLV Tag 类型
const (
	TagAudio  = 8
	TagVideo  = 9
	TagScript = 18
)

// Video FrameType(高 4 位)
const (
	FrameKey       = 1 // 关键帧
	FrameInter     = 2 // P/B 帧
	FrameDisp      = 3 // Disposable inter frame
	FrameGenKey    = 4 // Generated key frame
	FrameVideoInfo = 5
)

// Video CodecID(低 4 位)
const (
	CodecSorensonH263  = 2
	CodecScreenVideo   = 3
	CodecVP6           = 4
	CodecVP6Alpha      = 5
	CodecScreenVideoV2 = 6
	CodecAVC           = 7  // H264
	CodecHEVC          = 12 // H265(社区版透传)
)

// AVC Packet Type
const (
	AVCSeqHeader = 0
	AVCNALU      = 1
	AVCEndOfSeq  = 2
)

// Audio SoundFormat(高 4 位)
const (
	SoundAAC   = 10
	SoundG711A = 7
	SoundG711U = 8
	SoundMP3   = 2
)

// AACPacketType
const (
	AACSeqHeader = 0
	AACRaw       = 1
)

// Header FLV 文件头(9 字节) + PreviousTagSize0(4 字节)
var Header = []byte{
	'F', 'L', 'V', 0x01,
	0x05,                         // 有音频+有视频
	0x00, 0x00, 0x00, 0x09,       // HeaderSize
	0x00, 0x00, 0x00, 0x00,       // PreviousTagSize0
}

// bufPool 复用 bytes.Buffer,减少 GC 压力
var bufPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// Muxer FLV 封装器
type Muxer struct {
	w          io.Writer
	wroteHead  bool
	videoTrack *media.Track
	audioTrack *media.Track
	avcSent    bool
	aacSent    bool
}

// NewMuxer 创建 FLV 封装器
func NewMuxer(w io.Writer) *Muxer {
	return &Muxer{w: w}
}

// SetTracks 设置音视频轨道
func (m *Muxer) SetTracks(video, audio *media.Track) {
	m.videoTrack = video
	m.audioTrack = audio
}

// UpdateVideoTrack 更新视频轨道参数(当 in-band SPS/PPS 到达时调用)
// 如果 SPS/PPS 发生变化,重置 avcSent 以便重新发送 AVCSeqHeader
func (m *Muxer) UpdateVideoTrack(video *media.Track) {
	if video == nil || m.videoTrack == nil {
		return
	}
	// 检查 SPS/PPS 是否变化
	spsChanged := !bytes.Equal(m.videoTrack.SPS, video.SPS) && len(video.SPS) > 0
	ppsChanged := !bytes.Equal(m.videoTrack.PPS, video.PPS) && len(video.PPS) > 0
	if spsChanged {
		m.videoTrack.SPS = video.SPS
		m.videoTrack.Width = video.Width
		m.videoTrack.Height = video.Height
		m.videoTrack.FPS = video.FPS
	}
	if ppsChanged {
		m.videoTrack.PPS = video.PPS
	}
	if spsChanged || ppsChanged {
		m.avcSent = false // 重置以便重新发送 AVCSeqHeader
	}
}

// WriteHeader 写入 FLV 头与 onMetaData
func (m *Muxer) WriteHeader() error {
	if _, err := m.w.Write(Header); err != nil {
		return fmt.Errorf("写 FLV 头失败: %w", err)
	}
	if err := m.writeScriptTag(); err != nil {
		return err
	}
	m.wroteHead = true
	return nil
}

func (m *Muxer) writeScriptTag() error {
	body := encodeAMFMeta(m.buildMetaData())
	tag := buildTag(TagScript, 0, body)
	return m.writeTag(tag)
}

func (m *Muxer) buildMetaData() map[string]interface{} {
	meta := map[string]interface{}{
		"creator":  "StreamBridge",
		"hasVideo": m.videoTrack != nil,
		"hasAudio": m.audioTrack != nil,
	}
	if m.videoTrack != nil {
		meta["width"] = float64(m.videoTrack.Width)
		meta["height"] = float64(m.videoTrack.Height)
		meta["videocodecid"] = float64(CodecAVC)
		if m.videoTrack.FPS > 0 {
			meta["framerate"] = m.videoTrack.FPS
		}
	}
	if m.audioTrack != nil {
		meta["audiocodecid"] = float64(SoundAAC)
		meta["audiosamplerate"] = float64(m.audioTrack.SampleRate)
		meta["audiochannels"] = float64(m.audioTrack.Channels)
	}
	return meta
}

// WriteFrame 写入一个媒体帧
func (m *Muxer) WriteFrame(f *media.Frame) error {
	if !m.wroteHead {
		return fmt.Errorf("FLV 头未写入")
	}
	if f.IsVideo() {
		return m.writeVideoFrame(f)
	}
	if f.IsAudio() {
		return m.writeAudioFrame(f)
	}
	return nil
}

func (m *Muxer) writeVideoFrame(f *media.Frame) error {
	// H264: 单次切分 NALU,同时检查 SPS/PPS 更新并构建 body
	var nalus [][]byte
	if f.Codec == media.CodecH264 && m.videoTrack != nil {
		nalus = media.H264SplitNALUs(f.Payload)
		// 同一遍遍历中检查 in-band SPS/PPS 更新
		for _, n := range nalus {
			if media.H264IsSPS(n) && len(n) > len(m.videoTrack.SPS) {
				m.videoTrack.SPS = n
				if w, h, fps, err := media.H264SPSResolve(n); err == nil {
					m.videoTrack.Width = w
					m.videoTrack.Height = h
					m.videoTrack.FPS = fps
				}
				m.avcSent = false
			}
			if media.H264IsPPS(n) && len(n) > len(m.videoTrack.PPS) {
				m.videoTrack.PPS = n
				m.avcSent = false
			}
		}
	}
	if f.Codec == media.CodecH264 && !m.avcSent && m.videoTrack != nil && len(m.videoTrack.SPS) > 0 {
		if err := m.writeAVCSeqHeader(); err != nil {
			return err
		}
		m.avcSent = true
	}

	// 从 pool 获取 buffer
	body := bufPool.Get().(*bytes.Buffer)
	body.Reset()
	defer func() {
		bufPool.Put(body)
	}()

	codecID := byte(CodecAVC)
	if f.Codec == media.CodecH265 {
		codecID = byte(CodecHEVC)
	}
	frameType := byte(FrameInter)
	if f.IsKeyFrame {
		frameType = byte(FrameKey)
	}
	body.WriteByte(frameType<<4 | codecID)
	body.WriteByte(AVCNALU)
	ct := uint32(0)
	if f.PTS > f.DTS {
		ct = uint32((f.PTS - f.DTS).Milliseconds())
	}
	body.WriteByte(byte(ct >> 16))
	body.WriteByte(byte(ct >> 8))
	body.WriteByte(byte(ct))

	if f.Codec == media.CodecH264 {
		// 复用已切分的 nalus,避免第二次 H264SplitNALUs
		var lenBuf [4]byte
		for _, n := range nalus {
			if media.H264IsSPS(n) || media.H264IsPPS(n) {
				continue
			}
			binary.BigEndian.PutUint32(lenBuf[:], uint32(len(n)))
			body.Write(lenBuf[:])
			body.Write(n)
		}
	} else {
		nalus := media.H265SplitNALUs(f.Payload)
		var lenBuf [4]byte
		for _, n := range nalus {
			if media.H265IsVPS(n) || media.H265IsSPS(n) || media.H265IsPPS(n) {
				continue
			}
			binary.BigEndian.PutUint32(lenBuf[:], uint32(len(n)))
			body.Write(lenBuf[:])
			body.Write(n)
		}
	}

	tag := buildTag(TagVideo, uint32(f.DTS.Milliseconds()), body.Bytes())
	return m.writeTag(tag)
}

func (m *Muxer) writeAVCSeqHeader() error {
	var body bytes.Buffer
	body.WriteByte(FrameKey<<4 | CodecAVC)
	body.WriteByte(AVCSeqHeader)
	body.WriteByte(0)
	body.WriteByte(0)
	body.WriteByte(0)
	body.WriteByte(0x01)
	body.WriteByte(m.videoTrack.SPS[1])
	body.WriteByte(m.videoTrack.SPS[2])
	body.WriteByte(m.videoTrack.SPS[3])
	body.WriteByte(0xFF)
	body.WriteByte(0xE1)
	var spsLen [2]byte
	binary.BigEndian.PutUint16(spsLen[:], uint16(len(m.videoTrack.SPS)))
	body.Write(spsLen[:])
	body.Write(m.videoTrack.SPS)
	body.WriteByte(0x01)
	var ppsLen [2]byte
	binary.BigEndian.PutUint16(ppsLen[:], uint16(len(m.videoTrack.PPS)))
	body.Write(ppsLen[:])
	body.Write(m.videoTrack.PPS)

	tag := buildTag(TagVideo, 0, body.Bytes())
	return m.writeTag(tag)
}

func (m *Muxer) writeAudioFrame(f *media.Frame) error {
	if f.Codec == media.CodecAAC && !m.aacSent && m.audioTrack != nil {
		if err := m.writeAACSeqHeader(); err != nil {
			return err
		}
		m.aacSent = true
	}

	body := bufPool.Get().(*bytes.Buffer)
	body.Reset()
	defer func() {
		bufPool.Put(body)
	}()

	switch f.Codec {
	case media.CodecAAC:
		body.WriteByte(SoundAAC<<4 | 0x0F)
		body.WriteByte(AACRaw)
		body.Write(f.Payload)
	case media.CodecG711A:
		body.WriteByte(SoundG711A<<4 | 0x0F)
		body.Write(f.Payload)
	case media.CodecG711U:
		body.WriteByte(SoundG711U<<4 | 0x0F)
		body.Write(f.Payload)
	default:
		return nil
	}

	tag := buildTag(TagAudio, uint32(f.DTS.Milliseconds()), body.Bytes())
	return m.writeTag(tag)
}

func (m *Muxer) writeAACSeqHeader() error {
	cfg := &media.AACConfig{
		SampleRate: m.audioTrack.SampleRate,
		Channels:   m.audioTrack.Channels,
	}
	profile := byte(cfg.Profile + 1)
	byte1 := (profile << 3) | (cfg.SampleRateIndex() >> 1)
	byte2 := (cfg.SampleRateIndex()&0x01)<<7 | (byte(cfg.Channels)&0x0F)<<3

	var body bytes.Buffer
	body.WriteByte(SoundAAC<<4 | 0x0F)
	body.WriteByte(AACSeqHeader)
	body.WriteByte(byte1)
	body.WriteByte(byte2)

	tag := buildTag(TagAudio, 0, body.Bytes())
	return m.writeTag(tag)
}

func (m *Muxer) writeTag(tag []byte) error {
	if _, err := m.w.Write(tag); err != nil {
		return err
	}
	var sizeBuf [4]byte
	binary.BigEndian.PutUint32(sizeBuf[:], uint32(len(tag)-4))
	_, err := m.w.Write(sizeBuf[:])
	return err
}

// buildTag 构建 FLV Tag(11 字节头 + body),不含 prevSize
func buildTag(tagType byte, timestamp uint32, body []byte) []byte {
	tag := make([]byte, 11+len(body))
	tag[0] = tagType
	tag[1] = byte(len(body) >> 16)
	tag[2] = byte(len(body) >> 8)
	tag[3] = byte(len(body))
	tag[4] = byte(timestamp >> 16)
	tag[5] = byte(timestamp >> 8)
	tag[6] = byte(timestamp)
	tag[7] = byte(timestamp >> 24)
	copy(tag[11:], body)
	return tag
}

// encodeAMFMeta AMF0 编码 onMetaData
func encodeAMFMeta(meta map[string]interface{}) []byte {
	var buf bytes.Buffer
	buf.WriteByte(0x02) // string
	writeAMFString(&buf, "onMetaData")
	buf.WriteByte(0x08) // ecma array
	var arrLen [4]byte
	binary.BigEndian.PutUint32(arrLen[:], uint32(len(meta)))
	buf.Write(arrLen[:])
	for k, v := range meta {
		writeAMFString(&buf, k)
		switch val := v.(type) {
		case string:
			buf.WriteByte(0x02)
			writeAMFString(&buf, val)
		case float64:
			buf.WriteByte(0x00)
			var b [8]byte
			binary.BigEndian.PutUint64(b[:], math.Float64bits(val))
			buf.Write(b[:])
		case bool:
			buf.WriteByte(0x01)
			if val {
				buf.WriteByte(0x01)
			} else {
				buf.WriteByte(0x00)
			}
		}
	}
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x09)
	return buf.Bytes()
}

func writeAMFString(buf *bytes.Buffer, s string) {
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(s)))
	buf.Write(l[:])
	buf.WriteString(s)
}
