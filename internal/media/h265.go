package media

// H265 NALU 类型常量(6 位)
const (
	H265NALTRAILN    = 0
	H265NALTRAILR    = 1
	H265NALIDRWRADL  = 19
	H265NALIDRNLP    = 20
	H265NALCRANUT    = 21
	H265NALVPS       = 32
	H265NALSPS       = 33
	H265NALPPS       = 34
	H265NALAUD       = 35
	H265NALPREFIXSEI = 39
)

// H265NALUType 返回 NALU 类型(高 6 位)
func H265NALUType(nalu []byte) byte {
	if len(nalu) == 0 {
		return 0
	}
	return (nalu[0] >> 1) & 0x3F
}

// H265IsKeyFrame 判断是否关键帧(IDR/CRA)
func H265IsKeyFrame(nalu []byte) bool {
	t := H265NALUType(nalu)
	return t == H265NALIDRWRADL || t == H265NALIDRNLP || t == H265NALCRANUT
}

// H265IsVPS 是否 VPS
func H265IsVPS(nalu []byte) bool { return H265NALUType(nalu) == H265NALVPS }

// H265IsSPS 是否 SPS
func H265IsSPS(nalu []byte) bool { return H265NALUType(nalu) == H265NALSPS }

// H265IsPPS 是否 PPS
func H265IsPPS(nalu []byte) bool { return H265NALUType(nalu) == H265NALPPS }

// H265ExtractVPSSPSSPPS 从 NALU 列表提取 VPS/SPS/PPS
func H265ExtractVPSSPSSPPS(nalus [][]byte) (vps, sps, pps []byte) {
	for _, n := range nalus {
		switch H265NALUType(n) {
		case H265NALVPS:
			vps = n
		case H265NALSPS:
			sps = n
		case H265NALPPS:
			pps = n
		}
	}
	return
}

// H265SplitNALUs 与 H264 一致(都是 AnnexB 起始码)
func H265SplitNALUs(data []byte) [][]byte {
	return H264SplitNALUs(data)
}

// H265EncodeNALUs 与 H264 一致
func H265EncodeNALUs(nalus [][]byte) []byte {
	return H264EncodeNALUs(nalus)
}
