// PS 流解封装(GB28181 平台媒体转发)
// 参考: GB/T 28181-2022 附录 A
package rtp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/pion/rtp"

	"github.com/streambridge/streambridge/internal/media"
)

// PSDepacketizer PS 流解封装器
// 状态机:接收 RTP 包 -> 重组 PS 流 -> 解析 PS 包头 -> 提取 H264/AAC
type PSDepacketizer struct {
	buf       bytes.Buffer
	curPTS    uint32
	streamMap map[byte]byte // stream_id -> stream_type
	// 时间戳基准(将绝对 PTS 转为相对时间)
	basePTS   uint32
	baseSet   bool
}

// NewPSDepacketizer 创建 PS 解封装器
func NewPSDepacketizer() *PSDepacketizer {
	return &PSDepacketizer{
		streamMap: make(map[byte]byte),
	}
}

// Depacketize 处理一个 RTP 包,返回完整帧(若有)
func (p *PSDepacketizer) Depacketize(pkt *rtp.Packet) []*media.Frame {
	p.buf.Write(pkt.Payload)
	frames := []*media.Frame{}
	// 简化实现:扫描 PS 包起始码
	data := p.buf.Bytes()
	for len(data) >= 4 {
		start, packType, packLen, err := p.parsePackHeader(data)
		if err != nil {
			p.buf.Reset()
			return nil
		}
		if start+packLen > len(data) {
			break // 数据不完整
		}
		if packType == PackTypePESHeader {
			frame, consumed, err := p.parsePES(data[start : start+packLen])
			if err == nil && frame != nil {
				frames = append(frames, frame)
			}
			data = data[start+consumed:]
			continue
		}
		data = data[start+packLen:]
	}
	// 保留未消费的数据
	if len(data) > 0 {
		copy(p.buf.Bytes(), data)
		p.buf.Truncate(len(data))
	} else {
		p.buf.Reset()
	}
	return frames
}

const (
	PackTypePackHeader = 0xBA
	PackTypeSystemHeader = 0xBB
	PackTypePESHeader   = 0xE0 // 视频
	PackTypeAudioPES    = 0xC0 // 音频
	PackTypeMapHeader   = 0xBC
)

func (p *PSDepacketizer) parsePackHeader(data []byte) (start, packType, packLen int, err error) {
	// 找起始码 00 00 01 XX
	for i := 0; i < len(data)-3; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			pt := int(data[i+3])
			switch pt {
			case PackTypePackHeader:
				// pack header 固定 14 字节(简化)
				if i+14 > len(data) {
					return 0, 0, 0, fmt.Errorf("pack header 不完整")
				}
				return i, pt, 14, nil
			case PackTypeSystemHeader:
				if i+6 > len(data) {
					return 0, 0, 0, fmt.Errorf("system header 不完整")
				}
				l := int(binary.BigEndian.Uint16(data[i+4:i+6])) + 6
				return i, pt, l, nil
			case PackTypeMapHeader:
				if i+6 > len(data) {
					return 0, 0, 0, fmt.Errorf("map header 不完整")
				}
				l := int(binary.BigEndian.Uint16(data[i+4:i+6])) + 6
				return i, pt, l, nil
			case PackTypePESHeader, PackTypeAudioPES:
				if i+9 > len(data) {
					return 0, 0, 0, fmt.Errorf("PES header 不完整")
				}
				l := int(binary.BigEndian.Uint16(data[i+4:i+6])) + 6
				if l < 9 {
					l = 9
				}
				return i, pt, l, nil
			}
		}
	}
	return 0, 0, 0, fmt.Errorf("未找到起始码")
}

func (p *PSDepacketizer) parsePES(data []byte) (*media.Frame, int, error) {
	if len(data) < 9 {
		return nil, 0, fmt.Errorf("PES 过短")
	}
	streamID := data[3]
	pesLen := int(binary.BigEndian.Uint16(data[4:6]))
	consumed := 6 + pesLen
	if consumed > len(data) {
		consumed = len(data)
	}
	// PES header flags
	flags := data[7]
	headerDataLen := int(data[8])
	if 9+headerDataLen > len(data) {
		return nil, consumed, fmt.Errorf("PES header 过长")
	}
	payload := data[9+headerDataLen : consumed]
	// PTS
	if flags&0x80 != 0 && headerDataLen >= 5 {
		pts := p.parsePTS(data[9:])
		p.curPTS = pts
	}

	// 视频流(0xE0)
	if streamID >= 0xE0 && streamID <= 0xEF {
		nalus := media.H264SplitNALUs(payload)
		if len(nalus) == 0 {
			return nil, consumed, nil
		}
		sps, pps := media.H264ExtractSPSPPS(nalus)
		keyFrame := false
		for _, n := range nalus {
			if media.H264IsKeyFrame(n) {
				keyFrame = true
				break
			}
		}
		dur := p.relativePTS(p.curPTS)
		return &media.Frame{
			Codec:      media.CodecH264,
			IsKeyFrame: keyFrame,
			Payload:    media.H264EncodeNALUs(nalus),
			PTS:        dur,
			DTS:        dur,
			Track: &media.Track{
				Type:  media.TrackVideo,
				Codec: media.CodecH264,
				SPS:   sps,
				PPS:   pps,
				FPS:   25,
			},
		}, consumed, nil
	}
	// 音频流(0xC0-0xDF, G.711)
	if streamID >= 0xC0 && streamID <= 0xDF {
		dur := p.relativePTS(p.curPTS)
		return &media.Frame{
			Codec:   media.CodecG711A,
			Payload: payload,
			PTS:     dur,
			DTS:     dur,
			Track: &media.Track{
				Type:       media.TrackAudio,
				Codec:      media.CodecG711A,
				SampleRate: 8000,
				Channels:   1,
			},
		}, consumed, nil
	}
	return nil, consumed, nil
}

func (p *PSDepacketizer) parsePTS(data []byte) uint32 {
	if len(data) < 5 {
		return 0
	}
	pts := uint32(data[0]&0x0E) << 29
	pts |= uint32(data[1]) << 22
	pts |= uint32(data[2]&0xFE) << 14
	pts |= uint32(data[3]) << 7
	pts |= uint32(data[4]&0xFE) >> 1
	return pts
}

// relativePTS 将绝对 PTS 转为相对时间(处理 32 位回绕)
func (p *PSDepacketizer) relativePTS(pts uint32) time.Duration {
	if !p.baseSet {
		p.basePTS = pts
		p.baseSet = true
		return 0
	}
	diff := pts - p.basePTS // uint32 减法,天然处理回绕
	return time.Duration(diff) * time.Second / 90000
}
