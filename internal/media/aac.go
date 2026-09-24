package media

import "fmt"

// AAC 采样率索引表(ADTS header 使用)
var aacSampleRates = []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}

// AACConfig AAC 音频配置(AudioSpecificConfig)
type AACConfig struct {
	SampleRate int
	Channels   int
	Profile    byte // 0=AAC-LC(默认), 1=AAC-Main, 2=AAC-SSR, 3=AAC-LTP
}

// SampleRateIndex 返回 ADTS 采样率索引
func (c *AACConfig) SampleRateIndex() byte {
	for i, sr := range aacSampleRates {
		if sr == c.SampleRate {
			return byte(i)
		}
	}
	return 4 // 默认 44100
}

// EncodeADTS 为 AAC 原始帧添加 ADTS 头
// payload: AAC raw frame (无 ADTS 头)
// 返回带 ADTS 头的完整帧
func (c *AACConfig) EncodeADTS(payload []byte) ([]byte, error) {
	if len(payload) > 65535-7 {
		return nil, fmt.Errorf("AAC 帧过大: %d", len(payload))
	}
	frameLen := len(payload) + 7
	header := make([]byte, 7)
	// syncword 12 bits = 0xFFF
	header[0] = 0xFF
	header[1] = 0xF0
	// ID 1 bit(0=MPEG4) + layer 2 bits(00) + protection_absent 1 bit(1)
	header[1] |= 0x01
	// profile 2 bits + sampling_freq_index 4 bits + private 1 bit
	header[2] = ((c.Profile + 1) << 6) | (c.SampleRateIndex() << 2)
	// channel_config 3 bits + original_copy 1 + home 1 + copyright_id_bit 1 + copyright_id_start 1 + frame_length 2 bits(high)
	chCfg := byte(c.Channels)
	if chCfg == 0 {
		chCfg = 2 // 默认双声道
	}
	header[3] = (chCfg << 6) | byte((frameLen>>11)&0x03)
	// frame_length 8 bits(mid)
	header[4] = byte((frameLen >> 3) & 0xFF)
	// frame_length 3 bits(low) + buffer_fullness 5 bits(high) = 0x7F(可变码率)
	header[5] = byte((frameLen&0x07)<<5) | 0x1F
	// buffer_fullness 6 bits(low) + number_of_raw_data_blocks 2 bits(0)
	header[6] = 0xFC

	out := make([]byte, 7+len(payload))
	copy(out, header)
	copy(out[7:], payload)
	return out, nil
}

// G711ToPCM A-law/μ-law 转 PCM(16bit signed)
func G711ToPCM(g711 []byte, isALaw bool) []byte {
	pcm := make([]int16, len(g711))
	for i, b := range g711 {
		if isALaw {
			pcm[i] = alawDecode(b)
		} else {
			pcm[i] = ulawDecode(b)
		}
	}
	out := make([]byte, len(pcm)*2)
	for i, v := range pcm {
		out[i*2] = byte(v)
		out[i*2+1] = byte(v >> 8)
	}
	return out
}

func alawDecode(b byte) int16 {
	b ^= 0x55
	sign := b & 0x80
	exponent := (b >> 4) & 0x07
	mantissa := b & 0x0F
	sample := int16((mantissa<<4 + 8) << uint(exponent))
	if exponent != 0 {
		sample += 0x100
	}
	if sign == 0 {
		sample = -sample
	}
	return sample
}

func ulawDecode(b byte) int16 {
	b = ^b
	sign := b & 0x80
	exponent := (b >> 4) & 0x07
	mantissa := b & 0x0F
	sample := int16(((mantissa<<3) + 0x84) << uint(exponent))
	sample -= 0x84
	if sign == 0 {
		sample = -sample
	}
	return sample
}
