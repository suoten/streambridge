/*!
 * StreamBridge FLV→fMP4 转封装器 v1.2.0
 *
 * 将 WebSocket 收到的 FLV 流实时转封装为 fMP4 片段,喂给 MediaSource SourceBuffer
 *
 * v1.1 修复:
 *   - DTS 读取使用无符号算术(修复高位时间戳变负数)
 *   - sequenceNumber 改为实例级(修复多次播放序号不重置)
 *   - 解析 AudioSpecificConfig 获取真实采样率
 *   - 处理 DTS 回退/重置(重连场景)
 *
 * 工作流程:
 *   1. 解析 FLV 文件头(9 字节 + 4 字节 PreviousTagSize0)
 *   2. 逐个解析 FLV Tag(11 字节头 + body + 4 字节 PreviousTagSize)
 *   3. 视频:
 *      - AVCSequenceHeader(CodecID=7, AVCPacketType=0) → 提取 SPS/PPS,生成 fMP4 init segment
 *      - AVC NALU(AVCPacketType=1) → 提取 NALU,生成 fMP4 media segment
 *   4. 音频:
 *      - AACSequenceHeader(SoundFormat=10, AACPacketType=0) → 提取 AudioSpecificConfig,生成 fMP4 init segment
 *      - AAC Raw(AACPacketType=1) → 生成 fMP4 media segment
 *
 * License: MIT
 */
(function (global) {
  'use strict';

  // ===== FLV 常量 =====
  const FLV_TAG_AUDIO = 8;
  const FLV_TAG_VIDEO = 9;
  const FLV_TAG_SCRIPT = 18;

  const CODEC_AVC = 7;   // H264
  const CODEC_HEVC = 12; // H265(社区版透传,但浏览器 MSE 不支持,需转码)

  const AVC_SEQ_HEADER = 0;
  const AVC_NALU = 1;

  const AAC_SEQ_HEADER = 0;
  const AAC_RAW = 1;

  const FRAME_KEY = 1;

  // AAC 采样率索引表
  const AAC_SAMPLE_RATES = [96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350];

  // ===== 工具函数 =====

  function readUint32(buf, offset) {
    // 使用乘法避免有符号位运算 (buf[offset] << 24 在高位时变负数)
    return (buf[offset] * 0x1000000) + (buf[offset + 1] << 16) + (buf[offset + 2] << 8) + buf[offset + 3];
  }

  function readUint24(buf, offset) {
    return (buf[offset] << 16) | (buf[offset + 1] << 8) | buf[offset + 2];
  }

  function readUint16(buf, offset) {
    return (buf[offset] << 8) | buf[offset + 1];
  }

  function writeUint32(buf, offset, val) {
    buf[offset] = (val >>> 24) & 0xFF;
    buf[offset + 1] = (val >>> 16) & 0xFF;
    buf[offset + 2] = (val >>> 8) & 0xFF;
    buf[offset + 3] = val & 0xFF;
  }

  function writeUint16(buf, offset, val) {
    buf[offset] = (val >>> 8) & 0xFF;
    buf[offset + 1] = val & 0xFF;
  }

  // 缓存 TextEncoder 实例(避免每次 parseScriptTag 都创建)
  const _textEncoder = typeof TextEncoder !== 'undefined' ? new TextEncoder() : null;

  // ===== 解析 AudioSpecificConfig =====
  // 返回 { sampleRate, channels, objectType }
  function parseAudioSpecificConfig(config) {
    if (!config || config.length < 2) {
      return { sampleRate: 44100, channels: 2, objectType: 2 };
    }
    const byte0 = config[0];
    const byte1 = config[1];
    const objectType = (byte0 >> 3) & 0x1F;
    const samplingIndex = ((byte0 & 0x07) << 1) | ((byte1 >> 7) & 0x01);
    const channelConfig = (byte1 >> 3) & 0x0F;
    const sampleRate = samplingIndex < AAC_SAMPLE_RATES.length ? AAC_SAMPLE_RATES[samplingIndex] : 44100;
    return {
      sampleRate: sampleRate,
      channels: channelConfig || 2,
      objectType: objectType || 2
    };
  }

  // ===== MP4 Box 构造器 =====

  function box(type, payload) {
    const size = 8 + (payload ? payload.length : 0);
    const buf = new Uint8Array(size);
    writeUint32(buf, 0, size);
    for (let i = 0; i < 4; i++) {
      buf[4 + i] = type.charCodeAt(i);
    }
    if (payload) {
      buf.set(payload, 8);
    }
    return buf;
  }

  function fullbox(type, version, flags, payload) {
    const size = 12 + (payload ? payload.length : 0);
    const buf = new Uint8Array(size);
    writeUint32(buf, 0, size);
    for (let i = 0; i < 4; i++) {
      buf[4 + i] = type.charCodeAt(i);
    }
    buf[8] = version;
    buf[9] = (flags >>> 16) & 0xFF;
    buf[10] = (flags >>> 8) & 0xFF;
    buf[11] = flags & 0xFF;
    if (payload) {
      buf.set(payload, 12);
    }
    return buf;
  }

  // ===== AVC (H264) Profile → Codec String =====

  function avcCodecString(sps) {
    if (!sps || sps.length < 4) return 'avc1.42E01E'; // 默认 Baseline
    // H.264 SPS: [0]=NAL头 [1]=profile_idc [2]=constraint_set_flags [3]=level_idc
    const profile_idc = sps[1];
    const constraint = sps[2];
    const level_idc = sps[3];
    const pstr = profile_idc.toString(16).padStart(2, '0').toUpperCase();
    const cstr = constraint.toString(16).padStart(2, '0').toUpperCase();
    const lstr = level_idc.toString(16).padStart(2, '0').toUpperCase();
    return `avc1.${pstr}${cstr}${lstr}`;
  }

  // ===== fMP4 初始化段(Video) =====

  function buildVideoInitSegment(sps, pps, width, height) {
    // 调试日志
    const avcC = new Uint8Array(11 + sps.length + pps.length);
    let off = 0;
    avcC[off++] = 1;            // configurationVersion
    avcC[off++] = sps[1];       // AVCProfileIndication
    avcC[off++] = sps[2];       // profile_compatibility
    avcC[off++] = sps[3];       // AVCLevelIndication
    avcC[off++] = 0xFF;         // 111111 + lengthSizeMinusOne(3) → 4 字节长度前缀
    avcC[off++] = 0xE1;         // 111 + numSPS(1)
    writeUint16(avcC, off, sps.length); off += 2;
    avcC.set(sps, off); off += sps.length;
    avcC[off++] = 1;            // numPPS
    writeUint16(avcC, off, pps.length); off += 2;
    avcC.set(pps, off);

    return buildMoov(avcC, width || 0, height || 0, 'video', avcCodecString(sps));
  }

  // ===== fMP4 初始化段(Audio AAC) =====

  function buildAudioInitSegment(audioSpecificConfig) {
    const cfg = parseAudioSpecificConfig(audioSpecificConfig);
    const esds = buildESDS(audioSpecificConfig);
    const moov = buildMoov(esds, 0, 0, 'audio', 'mp4a.40.' + cfg.objectType, cfg.sampleRate, cfg.channels);
    return moov;
  }

  function buildESDS(config) {
    const esDesc = new Uint8Array(5 + 5 + 4 + config.length + 5);
    let off = 0;
    esDesc[off++] = 0x03; // ES_DescrTag
    esDesc[off++] = 0x19; // length
    esDesc[off++] = 0x00; // ES_ID
    esDesc[off++] = 0x01; // flags
    esDesc[off++] = 0x04; // DecoderConfigDescriptor tag
    esDesc[off++] = 5 + config.length + 5; // length
    esDesc[off++] = 0x40; // objectTypeIndication = Audio ISO/IEC 14496-3
    esDesc[off++] = 0x15; // streamType = AudioStream, upStream=0, reserved=1
    esDesc[off++] = 0x00; esDesc[off++] = 0x00; esDesc[off++] = 0x00;
    esDesc[off++] = 0x00; esDesc[off++] = 0x00; esDesc[off++] = 0x00; esDesc[off++] = 0x00;
    esDesc[off++] = 0x00; esDesc[off++] = 0x00; esDesc[off++] = 0x00; esDesc[off++] = 0x00;
    esDesc[off++] = 0x05; // DecoderSpecificInfo tag
    esDesc[off++] = config.length;
    esDesc.set(config, off); off += config.length;
    esDesc[off++] = 0x06;
    esDesc[off++] = 0x01;
    esDesc[off++] = 0x02;
    return fullbox('esds', 0, 0, esDesc);
  }

  function buildMoov(configData, width, height, type, codec, audioSampleRate, audioChannels) {
    const isVideo = type === 'video';
    const trackId = isVideo ? 1 : 2;
    // 视频用 1000 timescale (毫秒),音频用采样率作为 timescale
    const timescale = isVideo ? 1000 : (audioSampleRate || 44100);

    // mvhd (version 0, payload = 96 bytes)
    const mvhdPayload = new Uint8Array(96);
    let off = 0;
    off += 8; // creation_time + modification_time (zeros)
    writeUint32(mvhdPayload, off, timescale); off += 4;
    off += 4; // duration (zero)
    mvhdPayload[off++] = 0x00; mvhdPayload[off++] = 0x01; mvhdPayload[off++] = 0x00; mvhdPayload[off++] = 0x00; // rate = 1.0
    mvhdPayload[off++] = 0x01; mvhdPayload[off++] = 0x00; // volume = 1.0
    off += 10; // reserved
    const matrix = [0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000];
    for (const m of matrix) {
      writeUint32(mvhdPayload, off, m); off += 4;
    }
    off += 24; // pre_defined (zeros)
    writeUint32(mvhdPayload, off, 2); // next_track_ID
    const mvhd = fullbox('mvhd', 0, 0, mvhdPayload);

    // tkhd (version 0, flags=7, payload = 80 bytes)
    const tkhdPayload = new Uint8Array(80);
    off = 0;
    off += 8; // creation_time + modification_time (zeros)
    writeUint32(tkhdPayload, off, trackId); off += 4;
    off += 4; // reserved
    off += 4; // duration (zero)
    off += 8; // reserved
    tkhdPayload[off++] = 0; tkhdPayload[off++] = 0; // layer
    tkhdPayload[off++] = 0; tkhdPayload[off++] = 0; // alternate_group
    tkhdPayload[off++] = isVideo ? 0 : 0x01; tkhdPayload[off++] = isVideo ? 0 : 0x00; // volume
    off += 2; // reserved
    for (const m of matrix) {
      writeUint32(tkhdPayload, off, m); off += 4;
    }
    if (isVideo) {
      writeUint32(tkhdPayload, off, width << 16); off += 4;
      writeUint32(tkhdPayload, off, height << 16); off += 4;
    } else {
      off += 8;
    }
    const tkhd = fullbox('tkhd', 0, 0x07, tkhdPayload);

    // mdhd (version 0, payload = 20 bytes)
    const mdhdPayload = new Uint8Array(20);
    off = 0;
    off += 8; // creation_time + modification_time (zeros)
    writeUint32(mdhdPayload, off, timescale); off += 4;
    off += 4; // duration (zero)
    mdhdPayload[off++] = 0x55; mdhdPayload[off++] = 0xC4; // language = 'und'
    mdhdPayload[off++] = 0; mdhdPayload[off++] = 0; // pre_defined
    const mdhd = fullbox('mdhd', 0, 0, mdhdPayload);

    // hdlr
    const hdlrPayload = new Uint8Array(25);
    hdlrPayload[0] = 0; hdlrPayload[1] = 0; hdlrPayload[2] = 0; hdlrPayload[3] = 0;
    hdlrPayload[4] = isVideo ? 'v'.charCodeAt(0) : 's'.charCodeAt(0);
    hdlrPayload[5] = isVideo ? 'i'.charCodeAt(0) : 'o'.charCodeAt(0);
    hdlrPayload[6] = isVideo ? 'd'.charCodeAt(0) : 'u'.charCodeAt(0);
    hdlrPayload[7] = isVideo ? 'e'.charCodeAt(0) : 'n'.charCodeAt(0);
    hdlrPayload[24] = 0;
    const hdlr = fullbox('hdlr', 0, 0, hdlrPayload);

    let minf;
    if (isVideo) {
      const vmhd = fullbox('vmhd', 0, 1, new Uint8Array(8));
      // dref: entry_count=1 + url  box (self-contained, flags=1)
      const drefEntryCount = new Uint8Array(4);
      drefEntryCount[3] = 1;
      const urlBox = fullbox('url ', 0, 1, new Uint8Array(0));
      const dinf = box('dinf', fullbox('dref', 0, 0, concatBuffers([drefEntryCount, urlBox])));
      const stbl = buildStbl(configData, isVideo, codec, audioSampleRate, audioChannels, width, height);
      minf = box('minf', concatBuffers([vmhd, dinf, stbl]));
    } else {
      const smhd = fullbox('smhd', 0, 0, new Uint8Array(4));
      const drefEntryCount2 = new Uint8Array(4);
      drefEntryCount2[3] = 1;
      const urlBox2 = fullbox('url ', 0, 1, new Uint8Array(0));
      const dinf = box('dinf', fullbox('dref', 0, 0, concatBuffers([drefEntryCount2, urlBox2])));
      const stbl = buildStbl(configData, isVideo, codec, audioSampleRate, audioChannels, width, height);
      minf = box('minf', concatBuffers([smhd, dinf, stbl]));
    }

    const mdia = box('mdia', concatBuffers([mdhd, hdlr, minf]));
    const trak = box('trak', concatBuffers([tkhd, mdia]));

    // ftyp: major_brand='isom', minor_version=0, compatible_brands=['isom','iso5','mp41','iso2']
    const ftypPayload = new Uint8Array(24);
    ftypPayload[0] = 'i'.charCodeAt(0); // major_brand = 'isom'
    ftypPayload[1] = 's'.charCodeAt(0);
    ftypPayload[2] = 'o'.charCodeAt(0);
    ftypPayload[3] = 'm'.charCodeAt(0);
    ftypPayload[4] = 0; ftypPayload[5] = 0; ftypPayload[6] = 0; ftypPayload[7] = 0; // minor_version = 0
    ftypPayload[8] = 'i'.charCodeAt(0); // compatible_brand = 'isom'
    ftypPayload[9] = 's'.charCodeAt(0);
    ftypPayload[10] = 'o'.charCodeAt(0);
    ftypPayload[11] = 'm'.charCodeAt(0);
    ftypPayload[12] = 'i'.charCodeAt(0); // compatible_brand = 'iso5'
    ftypPayload[13] = 's'.charCodeAt(0);
    ftypPayload[14] = 'o'.charCodeAt(0);
    ftypPayload[15] = '5'.charCodeAt(0);
    ftypPayload[16] = 'm'.charCodeAt(0); // compatible_brand = 'mp41'
    ftypPayload[17] = 'p'.charCodeAt(0);
    ftypPayload[18] = '4'.charCodeAt(0);
    ftypPayload[19] = '1'.charCodeAt(0);
    ftypPayload[20] = 'i'.charCodeAt(0); // compatible_brand = 'iso2'
    ftypPayload[21] = 's'.charCodeAt(0);
    ftypPayload[22] = 'o'.charCodeAt(0);
    ftypPayload[23] = '2'.charCodeAt(0);

    const ftyp = box('ftyp', ftypPayload);

    // mvex (MovieExtendsBox) — fMP4 必需,告诉 MSE 这是 fragmented MP4
    const trexPayload = new Uint8Array(20);
    let trexOff = 0;
    writeUint32(trexPayload, trexOff, trackId); trexOff += 4; // track_ID
    writeUint32(trexPayload, trexOff, 1); trexOff += 4; // default_sample_description_index
    writeUint32(trexPayload, trexOff, 0); trexOff += 4; // default_sample_duration
    writeUint32(trexPayload, trexOff, 0); trexOff += 4; // default_sample_size
    writeUint32(trexPayload, trexOff, 0); trexOff += 4; // default_sample_flags
    const trex = fullbox('trex', 0, 0, trexPayload);
    const mvex = box('mvex', trex);

    const moov = box('moov', concatBuffers([mvhd, trak, mvex]));

    return concatBuffers([ftyp, moov]);
  }

  function buildStbl(configData, isVideo, codec, audioSampleRate, audioChannels, width, height) {
    // stsd payload: entry_count (4 bytes) = 1
    const stsdPayload = new Uint8Array(4);
    stsdPayload[3] = 1;

    if (isVideo) {
      const entryType = 'avc1';
      // VisualSampleEntry: 标准 78 字节 (ISO BMFF 规范)
      const visEntry = new Uint8Array(78);
      visEntry[6] = 0; visEntry[7] = 1; // data_reference_index = 1
      // Width and Height (16-bit big-endian at offset 24-27)
      if (width && width > 0) {
        visEntry[24] = (width >> 8) & 0xFF;
        visEntry[25] = width & 0xFF;
      }
      if (height && height > 0) {
        visEntry[26] = (height >> 8) & 0xFF;
        visEntry[27] = height & 0xFF;
      }
      // horizresolution = 0x00480000 (72 dpi)
      visEntry[28] = 0x00; visEntry[29] = 0x48; visEntry[30] = 0x00; visEntry[31] = 0x00;
      // vertresolution = 0x00480000 (72 dpi)
      visEntry[32] = 0x00; visEntry[33] = 0x48; visEntry[34] = 0x00; visEntry[35] = 0x00;
      visEntry[40] = 0; visEntry[41] = 1; // frame_count = 1
      visEntry[74] = 0x00; visEntry[75] = 0x18; // depth = 24
      visEntry[76] = 0xFF; visEntry[77] = 0xFF; // pre_defined = -1
      // avcC box: contains SPS/PPS for decoder initialization
      const avcCBox = box('avcC', configData);
      // colr box: nclx color information (Chrome 134+ required)
      // All 16-bit fields are big-endian per ISO 14496-12 spec
      const colrPayload = new Uint8Array(11);
      colrPayload[0] = 'n'.charCodeAt(0);
      colrPayload[1] = 'c'.charCodeAt(0);
      colrPayload[2] = 'l'.charCodeAt(0);
      colrPayload[3] = 'x'.charCodeAt(0);
      colrPayload[4] = 0x00; colrPayload[5] = 0x01; // colour_primaries = 1 (BT.709)
      colrPayload[6] = 0x00; colrPayload[7] = 0x01; // transfer_characteristics = 1 (BT.709)
      colrPayload[8] = 0x00; colrPayload[9] = 0x01; // matrix_coefficients = 1 (BT.709)
      colrPayload[10] = 0x00; // full_range_flag=0 + reserved=0
      const colrBox = box('colr', colrPayload);
      const entryBox = box(entryType, concatBuffers([visEntry, avcCBox, colrBox]));
      const stsd = fullbox('stsd', 0, 0, concatBuffers([stsdPayload, entryBox]));
      const stts = fullbox('stts', 0, 0, new Uint8Array(4));
      const stsc = fullbox('stsc', 0, 0, new Uint8Array(4));
      const stsz = fullbox('stsz', 0, 0, new Uint8Array(8));
      const stco = fullbox('stco', 0, 0, new Uint8Array(4));
      return box('stbl', concatBuffers([stsd, stts, stsc, stsz, stco]));
    } else {
      const entryType = 'mp4a';
      const audEntry = new Uint8Array(28);
      audEntry[6] = 0; audEntry[7] = 1;
      const channels = audioChannels || 2;
      audEntry[16] = 0; audEntry[17] = channels;
      audEntry[18] = 0; audEntry[19] = 16;
      const sr = audioSampleRate || 44100;
      audEntry[24] = (sr >> 8) & 0xFF;
      audEntry[25] = sr & 0xFF;
      audEntry[26] = 0; audEntry[27] = 0;
      const entryBox = box(entryType, concatBuffers([audEntry, configData]));
      const stsd = fullbox('stsd', 0, 0, concatBuffers([stsdPayload, entryBox]));
      const stts = fullbox('stts', 0, 0, new Uint8Array(4));
      const stsc = fullbox('stsc', 0, 0, new Uint8Array(4));
      const stsz = fullbox('stsz', 0, 0, new Uint8Array(8));
      const stco = fullbox('stco', 0, 0, new Uint8Array(4));
      return box('stbl', concatBuffers([stsd, stts, stsc, stsz, stco]));
    }
  }

  // ===== fMP4 媒体段(moof + mdat) =====
  // 注意: sequenceNumber 改为参数传入,实现实例级隔离

  function buildMediaSegment(payloads, baseDts, isVideo, trackId, sequenceNumber, timescale) {
    if (!payloads || payloads.length === 0) return null;

    const sampleCount = payloads.length;

    // 预计算 mdat payload 大小
    let mdatPayloadSize = 0;
    for (const p of payloads) {
      mdatPayloadSize += p.data.length;
    }

    // 预计算各 box 大小(避免中间分配)
    // mdat: 8(header) + payload
    const mdatSize = 8 + mdatPayloadSize;
    // mfhd: 12(fullbox header) + 4(sequence)
    const mfhdSize = 16;
    // tfhd: 12(fullbox header) + 4(trackId)
    const tfhdSize = 16;
    // tfdt: 12(fullbox header) + 8(baseDts)
    const tfdtSize = 20;
    // trun: 12(fullbox header) + 4(sampleCount) + 4(dataOffset) + sampleCount*12
    const trunSize = 12 + 4 + 4 + sampleCount * 12;
    // traf: 8(header) + tfhd + tfdt + trun
    const trafSize = 8 + tfhdSize + tfdtSize + trunSize;
    // moof: 8(header) + mfhd + traf
    const moofSize = 8 + mfhdSize + trafSize;

    // 总大小 = moof + mdat
    const totalSize = moofSize + mdatSize;

    // 单次分配,直接写入
    const buf = new Uint8Array(totalSize);
    let off = 0;

    // === moof ===
    writeUint32(buf, off, moofSize); off += 4;
    buf[off++] = 0x6D; buf[off++] = 0x6F; buf[off++] = 0x6F; buf[off++] = 0x66; // 'moof'

    // --- mfhd ---
    writeUint32(buf, off, mfhdSize); off += 4;
    buf[off++] = 0x6D; buf[off++] = 0x66; buf[off++] = 0x68; buf[off++] = 0x64; // 'mfhd'
    buf[off++] = 0; // version
    buf[off++] = 0; buf[off++] = 0; buf[off++] = 0; // flags
    writeUint32(buf, off, sequenceNumber); off += 4;

    // --- traf ---
    writeUint32(buf, off, trafSize); off += 4;
    buf[off++] = 0x74; buf[off++] = 0x72; buf[off++] = 0x61; buf[off++] = 0x66; // 'traf'

    // --- tfhd ---
    writeUint32(buf, off, tfhdSize); off += 4;
    buf[off++] = 0x74; buf[off++] = 0x66; buf[off++] = 0x68; buf[off++] = 0x64; // 'tfhd'
    buf[off++] = 0; // version
    buf[off++] = 0x02; buf[off++] = 0x00; buf[off++] = 0x00; // flags = 0x020000
    writeUint32(buf, off, trackId); off += 4;

    // --- tfdt ---
    writeUint32(buf, off, tfdtSize); off += 4;
    buf[off++] = 0x74; buf[off++] = 0x66; buf[off++] = 0x64; buf[off++] = 0x74; // 'tfdt'
    buf[off++] = 1; // version 1
    buf[off++] = 0; buf[off++] = 0; buf[off++] = 0; // flags
    const scaledDts = Math.floor(baseDts * timescale / 1000);
    writeUint32(buf, off, Math.floor(scaledDts / 0x100000000)); off += 4;
    writeUint32(buf, off, scaledDts & 0xFFFFFFFF); off += 4;

    // --- trun ---
    writeUint32(buf, off, trunSize); off += 4;
    buf[off++] = 0x74; buf[off++] = 0x72; buf[off++] = 0x75; buf[off++] = 0x6E; // 'trun'
    buf[off++] = 1; // version
    buf[off++] = 0x00; buf[off++] = 0x07; buf[off++] = 0x01; // flags = 0x000701
    writeUint32(buf, off, sampleCount); off += 4;
    // data_offset = moofSize + 8 (mdat header)
    writeUint32(buf, off, moofSize + 8); off += 4;
    // 记录 data_offset 在 buf 中的位置(用于后续修正,虽然这里已计算正确)
    for (const p of payloads) {
      const scaledDur = Math.max(1, Math.floor(p.duration * timescale / 1000));
      writeUint32(buf, off, scaledDur); off += 4;  // sample_duration
      writeUint32(buf, off, p.data.length); off += 4;  // sample_size
      const sampleFlags = p.isKey ? 0x02000000 : 0x00010000;
      writeUint32(buf, off, sampleFlags); off += 4;  // sample_flags
    }

    // === mdat ===
    writeUint32(buf, off, mdatSize); off += 4;
    buf[off++] = 0x6D; buf[off++] = 0x64; buf[off++] = 0x61; buf[off++] = 0x74; // 'mdat'
    for (const p of payloads) {
      buf.set(p.data, off);
      off += p.data.length;
    }

    return buf;
  }

  function concatBuffers(arr) {
    let total = 0;
    for (const b of arr) total += b.length;
    const r = new Uint8Array(total);
    let off = 0;
    for (const b of arr) {
      r.set(b, off);
      off += b.length;
    }
    return r;
  }

  // ===== FLV Demuxer =====

  // 视频编解码类型映射
  const VIDEO_CODEC_NAMES = {
    7: 'H264',
    12: 'H265'
  };

  // 音频编解码类型映射
  const AUDIO_CODEC_NAMES = {
    2: 'MP3',
    7: 'G711A',
    8: 'G711U',
    10: 'AAC'
  };

  class FLVDemuxer {
    constructor() {
      // 偏移量式缓冲区管理:避免每次 parse 都 slice 整个剩余缓冲区
      this._buf = new Uint8Array(0);
      this._bufOff = 0;          // 已消费的偏移量
      this.parsedHeader = false;
      this.videoInitSent = false;
      this.audioInitSent = false;
      this.videoCodec = null;
      this.audioCodec = null;
      this.sps = null;
      this.pps = null;
      this.audioConfig = null;
      this.audioInfo = null; // { sampleRate, channels, objectType }
      this.lastVideoDts = -1;
      this.lastAudioDts = -1;
      this.videoSeqNumber = 1;  // 实例级序号
      this.audioSeqNumber = 1;
      this.onInitSegment = null;     // (type, data) => void
      this.onMediaSegment = null;    // (type, data) => void
      this.onMetaData = null;        // (meta) => void
      this.onError = null;           // (message) => void
    }

    reset() {
      // 重置状态(用于重连)
      this._buf = new Uint8Array(0);
      this._bufOff = 0;
      this.parsedHeader = false;
      this.videoInitSent = false;
      this.audioInitSent = false;
      this.lastVideoDts = -1;
      this.lastAudioDts = -1;
      this.videoSeqNumber = 1;
      this.audioSeqNumber = 1;
    }

    feed(data) {
      const remaining = this._buf.length - this._bufOff;
      if (remaining === 0) {
        // 缓冲区已全部消费,直接替换
        this._buf = data;
        this._bufOff = 0;
      } else {
        // 追加新数据(仅在有剩余时才分配新数组)
        const newBuf = new Uint8Array(remaining + data.length);
        newBuf.set(this._buf.subarray(this._bufOff), 0);
        newBuf.set(data, remaining);
        this._buf = newBuf;
        this._bufOff = 0;
      }
      this.parse();
    }

    parse() {
      if (!this.parsedHeader) {
        if (this._buf.length - this._bufOff < 13) return;
        // 搜索 FLV 签名 'F','L','V'
        while (this._bufOff <= this._buf.length - 3) {
          if (this._buf[this._bufOff] === 0x46 && this._buf[this._bufOff+1] === 0x4C && this._buf[this._bufOff+2] === 0x56) break;
          this._bufOff++;
        }
        if (this._bufOff > this._buf.length - 13) return;
        this.parsedHeader = true;
        this._bufOff += 13;
      }

      while (this._buf.length - this._bufOff >= 15) {
        const tagType = this._buf[this._bufOff];
        const dataSize = readUint24(this._buf, this._bufOff + 1);
        const timestamp = readUint24(this._buf, this._bufOff + 4);
        const timestampExt = this._buf[this._bufOff + 7];
        // 修复:使用乘法避免有符号位运算 (timestampExt << 24 在 Ext>=0x80 时变负数)
        const dts = timestampExt * 0x1000000 + timestamp;

        const tagTotalSize = 11 + dataSize + 4;
        if (this._buf.length - this._bufOff < tagTotalSize) break;

        // 用 subarray 替代 slice:零拷贝视图,避免每帧分配新 Uint8Array
        const body = this._buf.subarray(this._bufOff + 11, this._bufOff + 11 + dataSize);

        switch (tagType) {
          case FLV_TAG_SCRIPT:
            this.parseScriptTag(body);
            break;
          case FLV_TAG_VIDEO:
            this.parseVideoTag(body, dts);
            break;
          case FLV_TAG_AUDIO:
            this.parseAudioTag(body, dts);
            break;
        }

        this._bufOff += tagTotalSize;
      }

      // 定期压缩缓冲区:当已消费部分超过 64KB 时回收
      if (this._bufOff > 65536 && this._bufOff > (this._buf.length - this._bufOff) * 2) {
        const remaining = this._buf.length - this._bufOff;
        const compacted = new Uint8Array(remaining);
        compacted.set(this._buf.subarray(this._bufOff), 0);
        this._buf = compacted;
        this._bufOff = 0;
      }
    }

    parseScriptTag(body) {
      if (this.onMetaData) {
        try {
          const meta = {};
          // 正确的 AMF0 解析:按结构遍历,而非盲目搜索
          // AMF0 onMetaData 格式:
          //   0x02 (string) + 2字节长度 + "onMetaData"
          //   0x08 (ecma array) + 4字节count
          //   每个 key-value: 2字节key长度 + key + 1字节value类型 + value
          //   0x00 0x00 0x09 (end marker)
          let off = 0;
          // 跳过 onMetaData 字符串 (0x02 + 2字节长度 + 字符串)
          if (off < body.length && body[off] === 0x02) {
            off++; // 0x02
            const strLen = readUint16(body, off); off += 2;
            off += strLen; // 跳过字符串内容
          }
          // 跳过 ecma array 标记 (0x08 + 4字节count)
          if (off < body.length && body[off] === 0x08) {
            off++; // 0x08
            off += 4; // array count
          }
          // 遍历 key-value 对
          while (off < body.length - 3) {
            // 检查 end marker (0x00 0x00 0x09)
            if (body[off] === 0x00 && body[off + 1] === 0x00 && body[off + 2] === 0x09) {
              break;
            }
            // 读取 key (2字节长度 + 字符串)
            if (off + 2 > body.length) break;
            const keyLen = readUint16(body, off); off += 2;
            if (keyLen === 0 || off + keyLen > body.length) break;
            let key = '';
            for (let i = 0; i < keyLen; i++) {
              key += String.fromCharCode(body[off + i]);
            }
            off += keyLen;
            // 读取 value
            if (off >= body.length) break;
            const valueType = body[off++];
            if (valueType === 0x00) {
              // number: 8字节 float64 big-endian
              if (off + 8 > body.length) break;
              // 必须使用 body.byteOffset + off 作为 DataView 偏移
              // 因为 body 可能是 subarray 视图,byteOffset 不为 0
              const view = new DataView(body.buffer, body.byteOffset + off, 8);
              const val = view.getFloat64(0, false);
              meta[key] = val;
              off += 8;
            } else if (valueType === 0x02) {
              // string: 2字节长度 + 字符串
              if (off + 2 > body.length) break;
              const sLen = readUint16(body, off); off += 2;
              let str = '';
              for (let i = 0; i < sLen && off + i < body.length; i++) {
                str += String.fromCharCode(body[off + i]);
              }
              meta[key] = str;
              off += sLen;
            } else if (valueType === 0x01) {
              // boolean: 1字节
              if (off >= body.length) break;
              meta[key] = body[off] !== 0;
              off += 1;
            } else {
              // 未知类型,停止解析
              break;
            }
          }
          // Store metadata for init segment generation
          if (meta.width && meta.width > 0 && meta.width < 65536) this._metaWidth = meta.width;
          if (meta.height && meta.height > 0 && meta.height < 65536) this._metaHeight = meta.height;
          this.onMetaData(meta);
        } catch (e) {
          // 忽略解析错误
        }
      }
    }

    parseVideoTag(body, dts) {
      if (body.length < 5) return;
      const frameType = (body[0] >> 4) & 0x0F;
      const codecId = body[0] & 0x0F;
      const avcPacketType = body[1];
      // composition time offset (signed 24-bit)
      let cts = readUint24(body, 2);
      if (cts & 0x800000) cts = cts - 0x1000000; // 转为有符号
      const isKeyFrame = frameType === FRAME_KEY;

      if (codecId !== CODEC_AVC) {
        // H265/HEVC 浏览器 MSE 不支持,报错而非静默丢弃
        const codecName = VIDEO_CODEC_NAMES[codecId] || ('Unknown(' + codecId + ')');
        if (codecId === CODEC_HEVC) {
          if (this.onError) this.onError('UNSUPPORTED_VIDEO_CODEC:H265');
        } else {
          if (this.onError) this.onError('UNSUPPORTED_VIDEO_CODEC:' + codecName);
        }
        return;
      }

      // subarray:零拷贝视图,仅在需要持久化时才 copy
      const naluData = body.subarray(5);

      if (avcPacketType === AVC_SEQ_HEADER) {
        this.parseAVCSeqHeader(naluData);
      } else if (avcPacketType === AVC_NALU) {
        if (!this.videoInitSent) {
          this.extractSPSPPSFromNALUs(naluData);
          if (!this.sps || !this.pps) return;
          this.sendVideoInit();
        }
        this.sendVideoNALUs(naluData, dts, isKeyFrame, cts);
      }
    }

    parseAVCSeqHeader(data) {
      if (data.length < 7) return;
      const numSPS = data[5] & 0x1F;
      let off = 6;
      if (numSPS > 0) {
        const spsLen = readUint16(data, off);
        off += 2;
        if (off + spsLen <= data.length) {
          // slice:必须 copy,SPS 需要持久化存储
          this.sps = data.slice(off, off + spsLen);
        }
        off += spsLen;
      }
      if (off < data.length) {
        const numPPS = data[off];
        off++;
  
        if (numPPS > 0) {
          const ppsLen = readUint16(data, off);
          off += 2;
          if (off + ppsLen <= data.length) {
            // slice:必须 copy,PPS 需要持久化存储
            this.pps = data.slice(off, off + ppsLen);
          }
        }
      }
      this.sendVideoInit();
    }

    extractSPSPPSFromNALUs(naluData) {
      let off = 0;
      while (off + 4 <= naluData.length) {
        const naluLen = readUint32(naluData, off);
        off += 4;
        if (off + naluLen > naluData.length) break;
        // slice:必须 copy,因为 SPS/PPS 需要持久化存储
        const nalu = naluData.slice(off, off + naluLen);
        off += naluLen;
        if (nalu.length === 0) continue;
        const naluType = nalu[0] & 0x1F;
        if (naluType === 7 && !this.sps) this.sps = nalu;       // SPS
        else if (naluType === 8 && !this.pps) this.pps = nalu;  // PPS
      }
    }

    sendVideoInit() {
      if (!this.sps || !this.pps) return;
      if (this.videoInitSent) return;
      // 从 onMetaData 获取分辨率,如果没有则从 SPS 解析(简化:留 0 让浏览器自适应)
      // 验证 width/height 必须是有效的正整数(防止 FLV metadata 解析出垃圾值)
      var width = 0, height = 0;
      if (this._metaWidth && isFinite(this._metaWidth) && this._metaWidth > 0 && this._metaWidth < 65536) {
        width = Math.round(this._metaWidth);
      }
      if (this._metaHeight && isFinite(this._metaHeight) && this._metaHeight > 0 && this._metaHeight < 65536) {
        height = Math.round(this._metaHeight);
      }
      const init = buildVideoInitSegment(this.sps, this.pps, width, height);
      this.videoInitSent = true;
      if (this.onInitSegment) this.onInitSegment('video', init);
    }

    sendVideoNALUs(naluData, dts, isKeyFrame, cts) {
      const nalus = [];
      let off = 0;
      while (off + 4 <= naluData.length) {
        const naluLen = readUint32(naluData, off);
        off += 4;
        if (off + naluLen > naluData.length) break;
        // subarray:零拷贝视图,sampleData 会 copy 到新数组
        const nalu = naluData.subarray(off, off + naluLen);
        off += naluLen;
        const naluType = nalu.length > 0 ? nalu[0] & 0x1F : 0;
        if (naluType === 7 || naluType === 8 || naluType === 9) continue;
        nalus.push(nalu);
      }
      if (nalus.length === 0) return;

      let totalLen = 0;
      for (const n of nalus) totalLen += 4 + n.length;
      const sampleData = new Uint8Array(totalLen);
      let sOff = 0;
      for (const n of nalus) {
        writeUint32(sampleData, sOff, n.length);
        sOff += 4;
        sampleData.set(n, sOff);
        sOff += n.length;
      }

      // 处理 DTS 回退(重连或时间戳重置)
      if (this.lastVideoDts >= 0 && dts < this.lastVideoDts) {
        // DTS 回退,重置基准
        this.lastVideoDts = dts;
      }
      const duration = this.lastVideoDts >= 0 && dts > this.lastVideoDts ? dts - this.lastVideoDts : 33;
      this.lastVideoDts = dts;

      const seq = this.videoSeqNumber++;
      const seg = buildMediaSegment(
        [{ data: sampleData, dts: dts, duration: duration, isKey: isKeyFrame }],
        dts, true, 1, seq, 1000
      );
      if (seg && this.onMediaSegment) this.onMediaSegment('video', seg);
    }

    parseAudioTag(body, dts) {
      if (body.length < 2) return;
      const soundFormat = (body[0] >> 4) & 0x0F;
      const aacPacketType = body[1];

      if (soundFormat !== 10) {
        // 非 AAC 音频(G711A/G711U/MP3 等),MSE 不支持,报错
        const codecName = AUDIO_CODEC_NAMES[soundFormat] || ('Unknown(' + soundFormat + ')');
        if (this.onError) this.onError('UNSUPPORTED_AUDIO_CODEC:' + codecName);
        return;
      }

      if (aacPacketType === AAC_SEQ_HEADER) {
        // AAC SeqHeader 需要持久化存储,必须 copy
        this.audioConfig = body.slice(2);
        this.audioInfo = parseAudioSpecificConfig(this.audioConfig);
        this.sendAudioInit();
      } else if (aacPacketType === AAC_RAW) {
        if (!this.audioInitSent) return;
        // subarray:零拷贝视图,buildMediaSegment 会 copy 到 mdat
        const aacData = body.subarray(2);
        // 处理 DTS 回退
        if (this.lastAudioDts >= 0 && dts < this.lastAudioDts) {
          this.lastAudioDts = dts;
        }
        const duration = this.lastAudioDts >= 0 && dts > this.lastAudioDts ? dts - this.lastAudioDts : 23;
        this.lastAudioDts = dts;
        const seq = this.audioSeqNumber++;
        const seg = buildMediaSegment(
          [{ data: aacData, dts: dts, duration: duration, isKey: true }],
          dts, false, 2, seq, this.audioInfo ? this.audioInfo.sampleRate : 44100
        );
        if (seg && this.onMediaSegment) this.onMediaSegment('audio', seg);
      }
    }

    sendAudioInit() {
      if (!this.audioConfig) return;
      if (this.audioInitSent) return;
      const init = buildAudioInitSegment(this.audioConfig);
      this.audioInitSent = true;
      if (this.onInitSegment) this.onInitSegment('audio', init);
    }
  }

  // 导出
  global.FLVDemuxer = FLVDemuxer;
  global.buildVideoInitSegment = buildVideoInitSegment;
  global.buildAudioInitSegment = buildAudioInitSegment;
  global.buildMediaSegment = buildMediaSegment;
  global.avcCodecString = avcCodecString;
  global.parseAudioSpecificConfig = parseAudioSpecificConfig;

})(typeof window !== 'undefined' ? window : this);
