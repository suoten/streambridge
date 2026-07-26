// Demo stream generator: generates a valid H264 test pattern for the demo stream
package server

import (
	"bytes"
	"context"
	"math/bits"
	"time"

	"github.com/streambridge/streambridge/internal/media"
)

// Demo stream configuration
const (
	demoWidth         = 64
	demoHeight        = 64
	demoFPS           = 10
	demoMBsX          = demoWidth / 16
	demoMBsY          = demoHeight / 16
	demoMBs           = demoMBsX * demoMBsY
	demoFrameInterval = 100 * time.Millisecond // 10fps
)

// SPS for 64x64 Baseline H264 with standard constraint flags and level
// constraint_set0=1, constraint_set1=1, constraint_set2=1 → 0xE0 (standard Baseline)
// level_idc = 30 (0x1E) → widely supported
// This produces codec string "avc1.42E01E" which is recognized by all browsers
// 67 42 E0 1E F8 84 C8
var demoSPS = []byte{0x67, 0x42, 0xE0, 0x1E, 0xF8, 0x84, 0xC8}

// PPS: 68 CE 38 80
var demoPPS = []byte{0x68, 0xCE, 0x38, 0x80}

// newDemoTrack creates the demo video track
func newDemoTrack() *media.Track {
	return &media.Track{
		Type:   media.TrackVideo,
		Codec:  media.CodecH264,
		SPS:    demoSPS,
		PPS:    demoPPS,
		Width:  demoWidth,
		Height: demoHeight,
		FPS:    demoFPS,
	}
}

// bitWriter writes individual bits to a byte buffer
type bitWriter struct {
	buf           bytes.Buffer
	current       byte
	bitsInCurrent int
}

func (bw *bitWriter) writeBit(b byte) {
	bw.current = (bw.current << 1) | (b & 1)
	bw.bitsInCurrent++
	if bw.bitsInCurrent == 8 {
		bw.buf.WriteByte(bw.current)
		bw.current = 0
		bw.bitsInCurrent = 0
	}
}

func (bw *bitWriter) writeU(val uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		bw.writeBit(byte((val >> uint(i)) & 1))
	}
}

func (bw *bitWriter) writeUE(val uint32) {
	val++                      // val+1 in binary
	n := bits.Len32(val)       // number of bits to represent val+1
	for i := 0; i < n-1; i++ { // n-1 leading zeros
		bw.writeBit(0)
	}
	bw.writeU(val, n) // write val in n bits
}

func (bw *bitWriter) writeSE(val int32) {
	var ue uint32
	if val <= 0 {
		ue = uint32(-val * 2)
	} else {
		ue = uint32(val*2 - 1)
	}
	bw.writeUE(ue)
}

func (bw *bitWriter) alignToByte() {
	for bw.bitsInCurrent > 0 {
		bw.writeBit(0)
	}
}

// bytes returns the complete byte slice, flushing any pending bits
func (bw *bitWriter) bytes() []byte {
	if bw.bitsInCurrent > 0 {
		bw.buf.WriteByte(bw.current << (8 - uint(bw.bitsInCurrent)))
		bw.current = 0
		bw.bitsInCurrent = 0
	}
	return bw.buf.Bytes()
}

// generateDemoIDRFrame generates an H264 IDR frame using I_16x16_0_0_0 macroblocks.
// All macroblocks use DC prediction (mode 0) with zero residual, producing a gray frame.
// This encoding is widely supported by browser H264 decoders (unlike I_PCM).
func generateDemoIDRFrame(pts time.Duration) *media.Frame {
	bw := &bitWriter{}

	// NALU header: IDR slice (nal_ref_idc=3, type=5)
	bw.writeU(0x65, 8)

	// Slice header (Baseline profile, I-slice)
	bw.writeUE(0)   // first_mb_in_slice = 0
	bw.writeUE(7)   // slice_type = 7 (I-slice)
	bw.writeUE(0)   // pic_parameter_set_id = 0
	bw.writeU(0, 4) // frame_num = 0 (log2_max_frame_num_minus4=0 -> 4 bits)
	bw.writeUE(0)   // idr_pic_id = 0
	bw.writeU(0, 4) // pic_order_cnt_lsb = 0 (log2_max_pic_order_cnt_lsb_minus4=0 -> 4 bits)
	bw.writeBit(0)  // no_output_of_prior_pics_flag
	bw.writeBit(0)  // long_term_reference_flag
	bw.writeSE(0)   // slice_qp_delta = 0

	// Macroblock data: 16 macroblocks (4x4), each using I_16x16_0_0_0
	// mb_type=1 in I-slice: Intra16x16, pred_mode=0(DC), CBP_chroma=0, CBP_luma=0
	// With zero residual, this produces a gray frame (Y=128, Cb=128, Cr=128)
	for mb := 0; mb < demoMBs; mb++ {
		bw.writeUE(1)  // mb_type = 1 (I_16x16_0_0_0)
		bw.writeUE(0)  // intra_chroma_pred_mode = 0 (DC)
		// mb_qp_delta is always present for I_16x16 (per H.264 spec 7.3.5)
		bw.writeSE(0)  // mb_qp_delta = 0
		// Luma DC block: 16 coefficients, all zero
		// coeff_token for TotalCoeff=0, TrailingOnes=0 with nC=0: 1 (1 bit)
		bw.writeBit(1)
		// No luma AC blocks (CBP_luma=0)
		// No chroma residual (CBP_chroma=0)
	}

	// RBSP stop bit
	bw.writeBit(1)
	// Align to byte boundary
	bw.alignToByte()

	naluData := bw.bytes()

	// Wrap in AnnexB start code so H264SplitNALUs correctly identifies the NALU
	payload := make([]byte, 4+len(naluData))
	copy(payload, media.AnnexBStartCode4)
	copy(payload[4:], naluData)

	return &media.Frame{
		Track: &media.Track{
			Type:  media.TrackVideo,
			Codec: media.CodecH264,
		},
		Codec:      media.CodecH264,
		IsKeyFrame: true,
		Payload:    payload,
		PTS:        pts,
		DTS:        pts,
	}
}

// serveDemoStream sends demo FLV frames via the WebSocket connection.
// Generates a gray test pattern at 10fps.
func serveDemoStream(flvMuxer *flvWriter, ctx context.Context) {
	ticker := time.NewTicker(demoFrameInterval)
	defer ticker.Stop()

	frameIdx := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pts := time.Duration(frameIdx) * demoFrameInterval
			frame := generateDemoIDRFrame(pts)
			if err := flvMuxer.WriteFrame(frame); err != nil {
				return
			}
			frameIdx++
		}
	}
}
