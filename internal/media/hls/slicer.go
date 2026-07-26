// Package hls 实现简易 HLS 实时切片服务
// 输入 H264/H265/AAC 媒体帧,输出 .m3u8 + .ts 切片
// 注意: 社区版仅提供基础 HLS(标准格式,非 LL-HLS),满足浏览器播放需求
package hls

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/streambridge/streambridge/internal/media"
)

// Slicer HLS 切片器
// 每个 Slicer 对应一个流,按 GOP 或固定时长切片
type Slicer struct {
	mu          sync.Mutex
	videoTrack  *media.Track
	audioTrack  *media.Track
	segmentDur  time.Duration // 单个切片目标时长(默认 3s)
	maxSegments int           // 保留的切片数(滑动窗口)
	segments    []*Segment
	seq         uint64
	curTS       *TSMuxer
	curStart    time.Duration
	curDuration time.Duration
	hasKeyFrame bool
}

// Segment 一个 TS 切片
type Segment struct {
	Seq      uint64
	Duration time.Duration
	Data     []byte
	Name     string
}

// NewSlicer 创建 HLS 切片器
func NewSlicer(video, audio *media.Track, segDur time.Duration) *Slicer {
	if segDur <= 0 {
		segDur = 3 * time.Second
	}
	return &Slicer{
		videoTrack:  video,
		audioTrack:  audio,
		segmentDur:  segDur,
		maxSegments: 10,
	}
}

// WriteFrame 写入一帧,返回新的切片(如果有)
func (s *Slicer) WriteFrame(f *media.Frame) (*Segment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 第一个关键帧才开始切片
	if s.curTS == nil {
		if !f.IsVideo() || !f.IsKeyFrame {
			return nil, nil
		}
		s.curTS = NewTSMuxer(s.videoTrack, s.audioTrack)
		s.curStart = f.DTS
		s.hasKeyFrame = true
	}

	var finishedSeg *Segment
	// 关键帧且超过目标时长,关闭当前切片
	if f.IsVideo() && f.IsKeyFrame && s.curDuration >= s.segmentDur {
		data := s.curTS.Bytes()
		finishedSeg = &Segment{
			Seq:      s.seq,
			Duration: s.curDuration,
			Data:     data,
			Name:     fmt.Sprintf("seg-%d.ts", s.seq),
		}
		s.seq++
		s.segments = append(s.segments, finishedSeg)
		if len(s.segments) > s.maxSegments {
			s.segments = s.segments[len(s.segments)-s.maxSegments:]
		}
		// 开始新切片
		s.curTS = NewTSMuxer(s.videoTrack, s.audioTrack)
		s.curStart = f.DTS
		s.curDuration = 0
	}

	if err := s.curTS.WriteFrame(f); err != nil {
		return nil, err
	}
	if f.DTS > s.curStart {
		s.curDuration = f.DTS - s.curStart
	}
	return finishedSeg, nil
}

// Playlist 生成 m3u8 播放列表
func (s *Slicer) Playlist() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	var buf bytes.Buffer
	buf.WriteString("#EXTM3U\n")
	buf.WriteString("#EXT-X-VERSION:3\n")
	buf.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(s.segmentDur.Seconds())))
	buf.WriteString("#EXT-X-MEDIA-SEQUENCE:" + fmt.Sprintf("%d", s.firstSeq()) + "\n")

	for _, seg := range s.segments {
		buf.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration.Seconds()))
		buf.WriteString(seg.Name + "\n")
	}
	return buf.Bytes()
}

// GetSegment 按名称获取切片
func (s *Slicer) GetSegment(name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, seg := range s.segments {
		if seg.Name == name {
			return seg.Data, true
		}
	}
	return nil, false
}

func (s *Slicer) firstSeq() uint64 {
	if len(s.segments) == 0 {
		return s.seq
	}
	return s.segments[0].Seq
}

// Close 关闭切片器,输出最后一个切片(可选)
func (s *Slicer) Close() *Segment {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.curTS == nil || !s.hasKeyFrame {
		return nil
	}
	seg := &Segment{
		Seq:      s.seq,
		Duration: s.curDuration,
		Data:     s.curTS.Bytes(),
		Name:     fmt.Sprintf("seg-%d.ts", s.seq),
	}
	s.seq++
	s.curTS = nil
	return seg
}

// ==================== TS Muxer ====================

// TSMuxer 简易 MPEG-TS 封装器
// 实现 H264/H265 AnnexB + AAC ADTS 的 TS 封装
type TSMuxer struct {
	videoTrack *media.Track
	audioTrack *media.Track
	buf        bytes.Buffer
	patSent    bool
	pmtSent    bool
	pesPid     uint16
	cc         map[uint16]byte // continuity_counter
}

const (
	tsPacketSize = 188
	pidPAT       = 0x0000
	pidPMT       = 0x1000
	pidVideo     = 0x0100
	pidAudio     = 0x0101
	streamH264   = 0x1B // StreamType H264
	streamH265   = 0x24 // StreamType H265
	streamAAC    = 0x0F // StreamType ADTS AAC
)

// NewTSMuxer 创建 TS 封装器
func NewTSMuxer(video, audio *media.Track) *TSMuxer {
	return &TSMuxer{
		videoTrack: video,
		audioTrack: audio,
		cc:         make(map[uint16]byte),
	}
}

// WriteFrame 写入一帧
func (m *TSMuxer) WriteFrame(f *media.Frame) error {
	if !m.patSent {
		m.writePAT()
		m.writePMT()
		m.patSent = true
		m.pmtSent = true
	}
	if f.IsVideo() {
		return m.writeVideoPES(f)
	}
	if f.IsAudio() {
		return m.writeAudioPES(f)
	}
	return nil
}

// Bytes 返回 TS 数据
func (m *TSMuxer) Bytes() []byte {
	return m.buf.Bytes()
}

// WriteTo 写入 io.Writer
func (m *TSMuxer) WriteTo(w io.Writer) (int64, error) {
	return m.buf.WriteTo(w)
}

func (m *TSMuxer) writePAT() {
	pkt := make([]byte, tsPacketSize)
	pkt[0] = 0x47 // sync byte
	// PID=0x0000, payload_unit_start=1, priority=0
	pkt[1] = 0x40
	pkt[2] = 0x00
	// adaptation_field_control=01(payload only), CC=0
	pkt[3] = 0x10
	// pointer_field
	pkt[4] = 0x00
	// PAT section
	pat := make([]byte, 13)
	pat[0] = 0x00 // table_id
	// section_length=13, section_syntax_indicator=1
	binary.BigEndian.PutUint16(pat[1:3], 0xB00D)
	binary.BigEndian.PutUint16(pat[3:5], 0x0001) // transport_stream_id
	pat[5] = 0xC1                               // version=0, current_next=1
	pat[6] = 0x00                               // section_number
	pat[7] = 0x00                               // last_section_number
	binary.BigEndian.PutUint16(pat[8:10], 0x1000) // program_number=1
	binary.BigEndian.PutUint16(pat[10:12], 0xE100) // PMT PID=0x1000
	crc := crc32MPEG(pat)
	binary.BigEndian.PutUint32(pat[9:13], crc) // 简化:覆盖最后 4 字节
	copy(pkt[5:], pat)
	// 填充
	for i := 5 + len(pat); i < tsPacketSize; i++ {
		pkt[i] = 0xFF
	}
	m.buf.Write(pkt)
}

func (m *TSMuxer) writePMT() {
	pkt := make([]byte, tsPacketSize)
	pkt[0] = 0x47
	pkt[1] = 0x50 // PID=0x1000, payload_start=1
	pkt[2] = 0x00
	pkt[3] = 0x10
	pkt[4] = 0x00 // pointer

	// PMT section(简化)
	pmt := bytes.Buffer{}
	pmt.WriteByte(0x02) // table_id PMT
	// section_length 后续填
	pmt.Write([]byte{0xB0, 0x12})
	pmt.Write([]byte{0x00, 0x01}) // program_number
	pmt.WriteByte(0xC1)
	pmt.Write([]byte{0x10, 0x00}) // PCR_PID = video
	// program_info_length=0
	pmt.Write([]byte{0xF0, 0x00})
	// 视频流
	if m.videoTrack != nil {
		st := byte(streamH264)
		if m.videoTrack.Codec == media.CodecH265 {
			st = streamH265
		}
		pmt.WriteByte(st)
		pmt.Write([]byte{0xE1, 0x00}) // PID=0x0100
		pmt.Write([]byte{0xF0, 0x00}) // ES_info_length=0
	}
	// 音频流
	if m.audioTrack != nil {
		pmt.WriteByte(streamAAC)
		pmt.Write([]byte{0xE1, 0x01}) // PID=0x0101
		pmt.Write([]byte{0xF0, 0x00})
	}
	crc := crc32MPEG(pmt.Bytes())
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], crc)
	pmt.Write(crcBuf[:])

	copy(pkt[5:], pmt.Bytes())
	for i := 5 + pmt.Len(); i < tsPacketSize; i++ {
		pkt[i] = 0xFF
	}
	m.buf.Write(pkt)
}

func (m *TSMuxer) writeVideoPES(f *media.Frame) error {
	var payload []byte
	if f.Codec == media.CodecH264 {
		payload = media.H264EncodeNALUs(media.H264SplitNALUs(f.Payload))
	} else {
		payload = media.H265EncodeNALUs(media.H265SplitNALUs(f.Payload))
	}
	pts := f.PTS.Microseconds()
	dts := f.DTS.Microseconds()
	return m.writePES(pidVideo, payload, pts, dts, true)
}

func (m *TSMuxer) writeAudioPES(f *media.Frame) error {
	var payload []byte
	if f.Codec == media.CodecAAC {
		cfg := &media.AACConfig{
			SampleRate: m.audioTrack.SampleRate,
			Channels:   m.audioTrack.Channels,
		}
		adts, err := cfg.EncodeADTS(f.Payload)
		if err != nil {
			return err
		}
		payload = adts
	} else {
		payload = f.Payload
	}
	pts := f.PTS.Microseconds()
	return m.writePES(pidAudio, payload, pts, pts, false)
}

func (m *TSMuxer) writePES(pid uint16, payload []byte, pts, dts int64, isVideo bool) error {
	// PES header
	header := make([]byte, 9)
	header[0] = 0x00
	header[1] = 0x00
	header[2] = 0x01 // start code
	header[3] = byte(pidVideoStreamID(pid)) // stream_id
	// PES_packet_length (16bit, 可为 0 表示不定长,视频用 0)
	pesLen := uint16(0)
	if !isVideo && len(payload)+3 <= 0xFFFF {
		pesLen = uint16(len(payload) + 3)
	}
	binary.BigEndian.PutUint16(header[4:6], pesLen)
	header[6] = 0x80 // 10 00 0000
	// PTS_DTS_flags: 11 表示有 PTS+DTS, 10 表示只有 PTS
	if isVideo && pts != dts {
		header[7] = 0xC0
		header[8] = 10 // header_data_length
	} else {
		header[7] = 0x80
		header[8] = 5
	}

	pesBuf := bytes.Buffer{}
	pesBuf.Write(header)
	// PTS
	m.writePTS(&pesBuf, pts, pts != dts)
	if pts != dts {
		m.writePTS(&pesBuf, dts, false)
	}
	pesBuf.Write(payload)

	// 切成 TS 包
	data := pesBuf.Bytes()
	first := true
	for len(data) > 0 {
		pkt := make([]byte, tsPacketSize)
		pkt[0] = 0x47
		pkt[1] = byte(pid>>8) | 0x40
		pkt[2] = byte(pid & 0xFF)
		cc := m.cc[pid]
		pkt[3] = 0x10 | (cc & 0x0F)
		m.cc[pid] = (cc + 1) & 0x0F

		offset := 4
		if first {
			// payload_unit_start_indicator 已设置
			// 加 1 字节 pointer(对 PES 不需要,但 payload_unit_start=1 时需注意)
			// 实际 PES 不需要 pointer,直接放数据
			first = false
		} else {
			pkt[1] &^= 0x40 // 清 payload_unit_start
		}

		// 计算可写入大小
		avail := tsPacketSize - offset
		if len(data) < avail {
			// 需要填充:加 adaptation field
			pkt[3] = (pkt[3] & 0x0F) | 0x30 // adaptation + payload
			afLen := tsPacketSize - offset - 1 - len(data)
			pkt[offset] = byte(afLen)
			offset++
			if afLen > 0 {
				pkt[offset] = 0x00 // flags
				offset++
				for i := 1; i < afLen; i++ {
					pkt[offset] = 0xFF
					offset++
				}
			}
			copy(pkt[offset:], data)
			data = nil
		} else {
			copy(pkt[offset:], data[:avail])
			data = data[avail:]
		}
		m.buf.Write(pkt)
	}
	return nil
}

func (m *TSMuxer) writePTS(buf *bytes.Buffer, pts int64, isDTS bool) {
	marker := byte(0x30) // 0011 0000 (PTS)
	if isDTS {
		marker = 0x10 // 0001 0000 (DTS)
	}
	b1 := marker | byte((pts>>30)&0x07)<<1 | 0x01
	buf.WriteByte(b1)
	buf.WriteByte(byte(pts >> 22))
	buf.WriteByte(byte((pts>>14)&0xFE) | 0x01)
	buf.WriteByte(byte(pts >> 7))
	buf.WriteByte(byte((pts<<1)&0xFE) | 0x01)
}

func pidVideoStreamID(pid uint16) byte {
	if pid == pidVideo {
		return 0xE0 // video stream
	}
	return 0xC0 // audio stream
}

// crc32MPEG MPEG-2 CRC32(简化实现)
func crc32MPEG(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
