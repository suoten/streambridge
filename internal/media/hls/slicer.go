// Package hls 实现简易 HLS 实时切片服务
// 输入 H264/H265/AAC 媒体帧,输出 .m3u8 + .ts 切片
// 注意: 社区版仅提供基础 HLS(标准格式,非 LL-HLS),满足浏览器播放需求
package hls

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
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
	streamID    string        // 流 ID,用于生成切片 URL 路径
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

// SetStreamID 设置流 ID(用于生成切片 URL 路径)
func (s *Slicer) SetStreamID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamID = id
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

	// 切片名前缀: {streamID}/seg-x.ts
	// 这样 hls.js 解析相对 URL 时会生成正确的路径 /hls/{streamID}/seg-x.ts
	prefix := ""
	if s.streamID != "" {
		prefix = s.streamID + "/"
	}

	for _, seg := range s.segments {
		buf.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration.Seconds()))
		buf.WriteString(prefix + seg.Name + "\n")
	}
	return buf.Bytes()
}

// GetSegment 按名称获取切片
func (s *Slicer) GetSegment(name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, seg := range s.segments {
		if seg.Name == name || strings.HasSuffix(seg.Name, "/"+name) || strings.HasSuffix(name, seg.Name) {
			return seg.Data, true
		}
	}
	return nil, false
}

// HasSegments 是否有已完成的切片
func (s *Slicer) HasSegments() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.segments) > 0
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
	pcrSent    bool
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
	// 在关键帧时写 PCR(时钟参考),hls.js 需要 PCR 进行时钟同步
	if f.IsVideo() && f.IsKeyFrame {
		m.writePCR(f.PTS.Microseconds())
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
	// PAT section: table_id(1) + section_length(2) + section_data(13) = 16 bytes
	pat := make([]byte, 16)
	pat[0] = 0x00 // table_id
	// section_length=13, section_syntax_indicator=1
	binary.BigEndian.PutUint16(pat[1:3], 0xB00D)
	binary.BigEndian.PutUint16(pat[3:5], 0x0001)  // transport_stream_id
	pat[5] = 0xC1                                // version=0, current_next=1
	pat[6] = 0x00                                // section_number
	pat[7] = 0x00                                // last_section_number
	binary.BigEndian.PutUint16(pat[8:10], 0x0001)  // program_number=1
	binary.BigEndian.PutUint16(pat[10:12], 0xF000) // reserved(111) + PMT PID=0x1000
	crc := crc32MPEG(pat[:12])                    // CRC over first 12 bytes
	binary.BigEndian.PutUint32(pat[12:16], crc)    // CRC at bytes 12-15
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

	// PMT section
	pmt := bytes.Buffer{}
	pmt.WriteByte(0x02) // table_id PMT
	// 计算流数量
	numStreams := 0
	if m.videoTrack != nil {
		numStreams++
	}
	if m.audioTrack != nil {
		numStreams++
	}
	// section_length = 5(header) + 4(PCR+info) + 5*streams + 4(CRC) = 13 + 5*numStreams
	sectionLen := byte(13 + 5*numStreams)
	pmt.Write([]byte{0xB0, sectionLen}) // section_syntax=1, section_length
	pmt.Write([]byte{0x00, 0x01})      // program_number=1
	pmt.WriteByte(0xC1)                // version=0, current_next=1
	pmt.WriteByte(0x00)                // section_number
	pmt.WriteByte(0x00)                // last_section_number
	pmt.Write([]byte{0xE1, 0x00})      // reserved(111) + PCR_PID=0x0100(video)
	pmt.Write([]byte{0xF0, 0x00})      // reserved(1111) + program_info_length=0
	// 视频流
	if m.videoTrack != nil {
		st := byte(streamH264)
		if m.videoTrack.Codec == media.CodecH265 {
			st = streamH265
		}
		pmt.WriteByte(st)
		pmt.Write([]byte{0xE1, 0x00}) // reserved(111) + PID=0x0100
		pmt.Write([]byte{0xF0, 0x00}) // reserved(1111) + ES_info_length=0
	}
	// 音频流
	if m.audioTrack != nil {
		pmt.WriteByte(streamAAC)
		pmt.Write([]byte{0xE1, 0x01}) // reserved(111) + PID=0x0101
		pmt.Write([]byte{0xF0, 0x00}) // reserved(1111) + ES_info_length=0
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

// writePCR 写入 PCR(节目时钟参考) 包
// PCR 用于 TS 解码器时钟同步,hls.js 需要 PCR 才能正确解析 TS
func (m *TSMuxer) writePCR(ptsMicroseconds int64) {
	pkt := make([]byte, tsPacketSize)
	pkt[0] = 0x47
	// PID=0x0100(video), payload_unit_start=0
	pkt[1] = 0x01
	pkt[2] = 0x00
	// adaptation_field_control=10(adaptation only), CC=continuity counter
	cc := m.cc[pidVideo]
	pkt[3] = 0x20 | (cc & 0x0F)
	// 不递增 CC(adaptation-only 包不递增 CC)

	// adaptation field
	// adaptation_field_length = 188 - 4(header) - 1(length byte) = 183
	pkt[4] = 183 // adaptation_field_length = 183 (flags + PCR + stuffing)
	// flags: PCR_flag=1, other flags=0
	pkt[5] = 0x10

	// PCR: 6 bytes
	// PCR_base (33 bits) | reserved (6 bits) | PCR_extension (9 bits)
	// PTS is in microseconds, PCR is in 90kHz units (1/90000 second)
	pcrBase := ptsMicroseconds * 90 / 1000 // convert microseconds to 90kHz units
	// PCR_base (33 bits)
	pkt[6] = byte((pcrBase >> 25) & 0xFF)
	pkt[7] = byte((pcrBase >> 17) & 0xFF)
	pkt[8] = byte((pcrBase >> 9) & 0xFF)
	pkt[9] = byte((pcrBase >> 1) & 0xFF)
	pkt[10] = byte((pcrBase&0x01)<<7) | 0x7E // reserved(6 bits=111111) + PCR_ext high bit
	pkt[11] = 0x00 | 0x01                     // PCR_extension low 8 bits + marker

	// stuffing
	for i := 12; i < tsPacketSize; i++ {
		pkt[i] = 0xFF
	}
	m.buf.Write(pkt)
}

func (m *TSMuxer) writeVideoPES(f *media.Frame) error {
	var payload []byte
	if f.Codec == media.CodecH264 {
		nalus := media.H264SplitNALUs(f.Payload)
		// 添加 AUD (Access Unit Delimiter) 前缀,帮助 TS demuxer 识别 access unit 边界
		audNalu := []byte{0x09, 0xF0} // AUD NALU type=9, primary_pic_type=0
		nalus = append([][]byte{audNalu}, nalus...)
		payload = media.H264EncodeNALUs(nalus)
	} else {
		payload = media.H265EncodeNALUs(media.H265SplitNALUs(f.Payload))
	}
	// PTS/DTS 转换为 90kHz 单位(MPEG-TS 标准)
	pts := f.PTS.Microseconds() * 90 / 1000
	dts := f.DTS.Microseconds() * 90 / 1000
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
	// PTS 转换为 90kHz 单位(MPEG-TS 标准)
	pts := f.PTS.Microseconds() * 90 / 1000
	return m.writePES(pidAudio, payload, pts, pts, false)
}

func (m *TSMuxer) writePES(pid uint16, payload []byte, pts, dts int64, isVideo bool) error {
	// PES header
	header := make([]byte, 9)
	header[0] = 0x00
	header[1] = 0x00
	header[2] = 0x01 // start code
	header[3] = byte(pidVideoStreamID(pid)) // stream_id

	hasDTS := isVideo && pts != dts
	hdrDataLen := 5 // PTS only
	if hasDTS {
		hdrDataLen = 10 // PTS + DTS
	}

	// PES_packet_length: 尽量设置实际长度(包括视频),帮助 TS demuxer 确定 PES 边界
	// 当 payload + overhead > 65535 时,视频允许使用 0(不定长)
	pesLen := uint16(0)
	overhead := 3 + hdrDataLen
	if len(payload)+overhead <= 0xFFFF {
		pesLen = uint16(len(payload) + overhead)
	}
	binary.BigEndian.PutUint16(header[4:6], pesLen)
	header[6] = 0x80 // 10 00 0000
	// PTS_DTS_flags: 11 表示有 PTS+DTS, 10 表示只有 PTS
	if hasDTS {
		header[7] = 0xC0
		header[8] = byte(hdrDataLen)
	} else {
		header[7] = 0x80
		header[8] = byte(hdrDataLen)
	}

	pesBuf := bytes.Buffer{}
	pesBuf.Write(header)
	// PTS: '0010' when PTS-only, '0011' when DTS follows
	if hasDTS {
		m.writePTS(&pesBuf, pts, 0x30) // '0011' for PTS with DTS
		m.writePTS(&pesBuf, dts, 0x10) // '0001' for DTS
	} else {
		m.writePTS(&pesBuf, pts, 0x20) // '0010' for PTS only
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
			// 确保 adaptation field 至少有 1 字节(flags),避免 afLen=0 导致解析器异常
			if afLen == 0 {
				afLen = 1
			}
			pkt[offset] = byte(afLen)
			offset++
			// flags 字节
			pkt[offset] = 0x00
			offset++
			// stuffing
			for i := 1; i < afLen; i++ {
				pkt[offset] = 0xFF
				offset++
			}
			// 复制数据(可能比 len(data) 少 1 字节,如果 afLen 从 0 改为 1)
			copyLen := tsPacketSize - offset
			if copyLen > len(data) {
				copyLen = len(data)
			}
			copy(pkt[offset:], data[:copyLen])
			data = data[copyLen:]
		} else {
			copy(pkt[offset:], data[:avail])
			data = data[avail:]
		}
		m.buf.Write(pkt)
	}
	return nil
}

// writePTS 写入 PTS/DTS 字段
// prefix: 0x20='0010'(PTS only), 0x30='0011'(PTS with DTS), 0x10='0001'(DTS)
func (m *TSMuxer) writePTS(buf *bytes.Buffer, pts int64, prefix byte) {
	b1 := prefix | byte((pts>>30)&0x07)<<1 | 0x01
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

// crc32MPEG MPEG-2 CRC32
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
	return ^crc // 最终取反(MPEG-2 标准)
}
