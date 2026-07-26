package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// H264 NALU 类型常量
const (
	H264NALSLICE    = 1  // P 帧
	H264NALDPA      = 2
	H264NALDPB      = 3
	H264NALDPC      = 4
	H264NALIDRSLICE = 5  // I 帧(关键帧)
	H264NALSEI      = 6
	H264NALSPS      = 7
	H264NALPPS      = 8
	H264NALAUD      = 9  // Access Unit Delimiter
)

// AnnexBStartCode4 4 字节起始码
var AnnexBStartCode4 = []byte{0x00, 0x00, 0x00, 0x01}

// AnnexBStartCode3 3 字节起始码
var AnnexBStartCode3 = []byte{0x00, 0x00, 0x01}

// H264NALUType 返回 NALU 类型(低 5 位)
func H264NALUType(nalu []byte) byte {
	if len(nalu) == 0 {
		return 0
	}
	return nalu[0] & 0x1F
}

// H264IsKeyFrame 判断是否关键帧(IDR)
func H264IsKeyFrame(nalu []byte) bool {
	return H264NALUType(nalu) == H264NALIDRSLICE
}

// H264IsSPS 是否 SPS
func H264IsSPS(nalu []byte) bool { return H264NALUType(nalu) == H264NALSPS }

// H264IsPPS 是否 PPS
func H264IsPPS(nalu []byte) bool { return H264NALUType(nalu) == H264NALPPS }

// H264IsAUD 是否 AUD
func H264IsAUD(nalu []byte) bool { return H264NALUType(nalu) == H264NALAUD }

// H264SplitNALUs 按 AnnexB 起始码切分 NALU
// 输入: 带 00 00 00 01 起始码的 H264 流
// 输出: 不含起始码的 NALU 列表
func H264SplitNALUs(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	var nalus [][]byte
	start := 0
	for i := 0; i < len(data)-3; i++ {
		if data[i] == 0 && data[i+1] == 0 {
			if data[i+2] == 1 {
				// 3 字节起始码 00 00 01
				if start < i {
					nalus = append(nalus, data[start:i])
				}
				start = i + 3
				i += 2
				continue
			}
			if i+3 < len(data) && data[i+2] == 0 && data[i+3] == 1 {
				// 4 字节起始码 00 00 00 01
				if start < i {
					nalus = append(nalus, data[start:i])
				}
				start = i + 4
				i += 3
				continue
			}
		}
	}
	if start < len(data) {
		nalus = append(nalus, data[start:])
	}
	return nalus
}

// H264EncodeNALUs 把 NALU 列表打包为 AnnexB 格式(每个加 4 字节起始码)
func H264EncodeNALUs(nalus [][]byte) []byte {
	var buf bytes.Buffer
	for _, nalu := range nalus {
		buf.Write(AnnexBStartCode4)
		buf.Write(nalu)
	}
	return buf.Bytes()
}

// H264SPSResolve 解析 SPS 提取宽高与 FPS
// 完整支持 Baseline/Main/High profile
// 返回 (width, height, fps, err)
func H264SPSResolve(sps []byte) (int, int, float64, error) {
	if len(sps) < 4 {
		return 0, 0, 0, fmt.Errorf("SPS 过短")
	}
	r := newBitReader(sps[1:]) // 跳过 NALU header
	// profile_idc(8) constraint_flags(8) level_idc(8)
	profileIDC := r.readBits(8)
	r.readBits(8) // constraint_set_flags
	r.readBits(8) // level_idc
	// seq_parameter_set_id (ue)
	r.readUE()

	// High profile 及以上需要解析额外字段
	if profileIDC == 100 || profileIDC == 110 || profileIDC == 122 ||
		profileIDC == 244 || profileIDC == 44 || profileIDC == 83 ||
		profileIDC == 86 || profileIDC == 118 || profileIDC == 128 ||
		profileIDC == 138 || profileIDC == 139 || profileIDC == 134 ||
		profileIDC == 135 {
		chromaFormatIDC := r.readUE()
		if chromaFormatIDC == 3 {
			r.readBit() // separate_colour_plane_flag
		}
		r.readUE() // bit_depth_luma_minus8
		r.readUE() // bit_depth_chroma_minus8
		r.readBit() // qpprime_y_zero_transform_bypass_flag
		if r.readBit() == 1 { // seq_scaling_matrix_present_flag
			numLists := 8
			if chromaFormatIDC != 3 {
				numLists = 12
			}
			for i := 0; i < numLists; i++ {
				if r.readBit() == 1 { // seq_scaling_list_present_flag
					size := 16
					if i >= 6 {
						size = 64
					}
					lastScale := 8
					nextScale := 8
					for j := 0; j < size; j++ {
						if nextScale != 0 {
							deltaScale := int(r.readSE())
							nextScale = (lastScale + deltaScale + 256) % 256
						}
						if nextScale != 0 {
							lastScale = nextScale
						}
					}
				}
			}
		}
	}

	// log2_max_frame_num_minus4
	r.readUE()
	// pic_order_cnt_type
	pocType := r.readUE()
	if pocType == 0 {
		r.readUE() // log2_max_pic_order_cnt_lsb_minus4
	} else if pocType == 1 {
		r.readBit() // delta_pic_order_always_zero_flag
		r.readSE()  // offset_for_non_ref_pic
		r.readSE()  // offset_for_top_to_bottom_field
		numRefs := r.readUE()
		for i := uint32(0); i < numRefs; i++ {
			r.readSE()
		}
	}
	// max_num_ref_frames
	r.readUE()
	// gaps_in_frame_num_value_allowed_flag
	r.readBit()
	// pic_width_in_mbs_minus1
	picW := r.readUE()
	// pic_height_in_map_units_minus1
	picH := r.readUE()
	// frame_mbs_only_flag
	frameMbsOnly := r.readBit()
	if frameMbsOnly == 0 {
		r.readBit() // mb_adaptive_frame_field_flag
	}
	// direct_8x8_inference_flag
	r.readBit()
	// frame_cropping
	cropLeft, cropRight, cropTop, cropBottom := 0, 0, 0, 0
	if r.readBit() == 1 { // frame_cropping_flag
		cropUnitX := 2
		cropUnitY := 2 - int(frameMbsOnly)
		cropLeft = int(r.readUE()) * cropUnitX
		cropRight = int(r.readUE()) * cropUnitX
		cropTop = int(r.readUE()) * cropUnitY
		cropBottom = int(r.readUE()) * cropUnitY
	}

	width := int((picW + 1) * 16)
	height := int((2-int(frameMbsOnly)) * (int(picH) + 1) * 16)
	width -= (cropLeft + cropRight)
	height -= (cropTop + cropBottom)
	if width <= 0 || height <= 0 {
		return 0, 0, 0, fmt.Errorf("非法分辨率: %dx%d", width, height)
	}

	// VUI parameters:尝试解析 FPS
	fps := parseVUIForFPS(r)
	if fps <= 0 {
		fps = 25
	}
	return width, height, fps, nil
}

// parseVUIForFPS 从 VUI parameters 中解析帧率
func parseVUIForFPS(r *bitReader) float64 {
	if r.readBit() == 0 { // vui_parameters_present_flag
		return 0
	}
	// aspect_ratio_info_present_flag
	if r.readBit() == 1 {
		aspectRatioIDC := r.readBits(8)
		if aspectRatioIDC == 255 { // Extended_SAR
			r.readBits(16) // sar_width
			r.readBits(16) // sar_height
		}
	}
	// overscan_info_present_flag
	if r.readBit() == 1 {
		r.readBit() // overscan_appropriate_flag
	}
	// video_signal_type_present_flag
	if r.readBit() == 1 {
		r.readBits(3) // video_format
		r.readBit()   // video_full_range_flag
		if r.readBit() == 1 { // colour_description_present_flag
			r.readBits(8) // colour_primaries
			r.readBits(8) // transfer_characteristics
			r.readBits(8) // matrix_coefficients
		}
	}
	// chroma_loc_info_present_flag
	if r.readBit() == 1 {
		r.readUE() // chroma_sample_loc_type_top_field
		r.readUE() // chroma_sample_loc_type_bottom_field
	}
	// timing_info_present_flag
	if r.readBit() == 1 {
		numUnitsInTick := r.readBits(32)
		timeScale := r.readBits(32)
		r.readBit() // fixed_frame_rate_flag
		if numUnitsInTick > 0 && timeScale > 0 {
			return float64(timeScale) / float64(2*numUnitsInTick)
		}
	}
	return 0
}

// bitReader 简易位读取器
type bitReader struct {
	data []byte
	pos  int
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

func (r *bitReader) readBit() uint32 {
	if r.pos/8 >= len(r.data) {
		return 0
	}
	bit := (r.data[r.pos/8] >> (7 - uint(r.pos%8))) & 1
	r.pos++
	return uint32(bit)
}

func (r *bitReader) readBits(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		v = (v << 1) | r.readBit()
	}
	return v
}

// readUE 读 Exp-Golomb 无符号整数
func (r *bitReader) readUE() uint32 {
	zeroCount := 0
	for r.readBit() == 0 && zeroCount < 32 {
		zeroCount++
	}
	if zeroCount == 0 {
		return 0
	}
	return (1<<uint(zeroCount) - 1) + r.readBits(zeroCount)
}

// readSE 读 Exp-Golomb 有符号整数
func (r *bitReader) readSE() int32 {
	v := r.readUE()
	if v%2 == 0 {
		return -int32(v / 2)
	}
	return int32((v + 1) / 2)
}

// H264ExtractSPSPPS 从 NALU 列表中提取 SPS 与 PPS
func H264ExtractSPSPPS(nalus [][]byte) (sps, pps []byte) {
	for _, n := range nalus {
		switch H264NALUType(n) {
		case H264NALSPS:
			sps = n
		case H264NALPPS:
			pps = n
		}
	}
	return
}

// H264AVCToAnnexB AVCC(4字节长度前缀)转 AnnexB(起始码)
func H264AVCToAnnexB(data []byte, lengthSize int) []byte {
	var out bytes.Buffer
	for len(data) > lengthSize {
		var n int
		if lengthSize == 4 {
			n = int(binary.BigEndian.Uint32(data[:4]))
		} else if lengthSize == 2 {
			n = int(binary.BigEndian.Uint16(data[:2]))
		} else {
			n = int(data[0])
		}
		data = data[lengthSize:]
		if n > len(data) {
			break
		}
		out.Write(AnnexBStartCode4)
		out.Write(data[:n])
		data = data[n:]
	}
	return out.Bytes()
}
