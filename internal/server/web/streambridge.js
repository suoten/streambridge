/*!
 * StreamBridge JavaScript SDK v1.3.0
 * 一个文件,让摄像头连上浏览器
 *
 * v1.3: 偏移量缓冲区优化,实时统计,自适应延迟控制,指数退避重连
 * v1.2: 修复重连后 demuxer 状态重置,改进缓冲与延迟控制
 * v1.1: 修复 FLV→fMP4 转封装,实现流畅播放
 *
 * 用法:
 *   <div id="player"></div>
 *   <script src="flv-demuxer.js"></script>
 *   <script src="streambridge.js"></script>
 *   <script>
 *     const player = StreamBridge.play({
 *       container: '#player',
 *       source: 'rtsp://192.168.1.100:554/live',
 *       mode: 'auto',
 *       autoplay: true,
 *       muted: true
 *     });
 *   </script>
 *
 * License: MIT
 */
(function (global) {
  'use strict';

  const STREAMBRIDGE_VERSION = '1.3.0';

  function play(options) {
    return new Player(options);
  }

  async function startStream(gatewayUrl, sourceUrl) {
    const resp = await fetch(`${gatewayUrl}/api/play?url=${encodeURIComponent(sourceUrl)}&format=flv`);
    if (!resp.ok) throw new Error(`启动流失败: ${resp.status} ${resp.statusText}`);
    return await resp.json();
  }

  async function startHLS(gatewayUrl, sourceUrl) {
    const resp = await fetch(`${gatewayUrl}/hls/play?url=${encodeURIComponent(sourceUrl)}`);
    if (!resp.ok) throw new Error(`HLS 启动失败: ${resp.status}`);
    return await resp.json();
  }

  async function stopStream(gatewayUrl, streamId) {
    if (streamId) {
      await fetch(`${gatewayUrl}/api/stop?id=${streamId}`, { method: 'POST' }).catch(() => {});
    }
  }

  async function listStreams(gatewayUrl) {
    const resp = await fetch(`${gatewayUrl}/api/streams`);
    return await resp.json();
  }

  class Player {
    constructor(options) {
      this.opts = Object.assign({
        mode: 'auto',
        autoplay: true,
        muted: true,
        gateway: '',
        liveBufferLatency: 1.5,  // 直播延迟(秒),控制缓冲
        liveBufferMaxLatency: 4, // 最大允许延迟(秒),超过则硬追赶
        statsInterval: 1000,     // 统计上报间隔(ms)
      }, options);
      this.video = null;
      this.ws = null;
      this.pc = null;
      this.mse = null;
      this.videoSB = null;
      this.audioSB = null;
      this.demuxer = null;
      this.streamId = null;
      this.wsUrl = null;
      this._videoQueue = [];
      this._audioQueue = [];
      this._mseReady = false;
      this._destroyed = false;
      this._resetting = false;  // 标记正在重置(避免 sourceended 误报)
      this._everReceivedVideo = false;  // 是否曾收到过视频数据
      this._fallbackTried = false;  // 是否已尝试过回退
      this._hls = null;
      this._inSBUpdate = false;  // 防止 _onSBUpdateEnd 递归
      // 实时统计
      this._statsTimer = null;
      this._videoSegCount = 0;
      this._audioSegCount = 0;
      this._bytesReceived = 0;
      this._lastStatsTime = 0;
      this._lastBufferEnd = 0;
      this._lastDataTime = 0;  // 最后一次收到数据的时间戳
      this._staleCheckTimer = null;  // 数据活性检测定时器
      // 重连退避
      this._reconnectDelay = 2000;
      this._maxReconnectDelay = 30000;
      this._init();
    }

    async _init() {
      this._resolveContainer();
      this._createVideo();
      try {
        await this._startPlayback();
      } catch (e) {
        this._emit('onError', e.message || String(e));
      }
    }

    _resolveContainer() {
      const c = this.opts.container;
      if (typeof c === 'string') {
        this.container = document.querySelector(c);
      } else if (c instanceof HTMLElement) {
        this.container = c;
      } else {
        this.container = document.body;
      }
      if (!this.container) throw new Error('容器不存在: ' + c);
    }

    _createVideo() {
      // If container is already a video element, use it directly
      if (this.container instanceof HTMLVideoElement) {
        this.video = this.container;
      } else {
        let v = this.container.querySelector('video');
        if (!v) {
          v = document.createElement('video');
          v.setAttribute('playsinline', '');
          v.setAttribute('webkit-playsinline', '');
          this.container.appendChild(v);
        }
        this.video = v;
      }
      this.video.autoplay = this.opts.autoplay;
      this.video.muted = this.opts.muted;
      this.video.controls = true;
      this.video.playsInline = true;
    }

    _gateway() {
      return this.opts.gateway || (location.protocol + '//' + location.host);
    }

    async _startPlayback() {
      const source = this.opts.source;
      const mode = this.opts.mode;
      const gateway = this._gateway();

      // demo 流: 使用 Canvas 生成测试画面(无需 H264 编码,所有浏览器兼容)
      if (source === 'demo' || !source) {
        return this._playDemo();
      }

      if (source.startsWith('ws://') || source.startsWith('wss://')) {
        return this._playFLV(source);
      }
      if (source.startsWith('http') && source.endsWith('.m3u8')) {
        return this._playHLS(source);
      }

      const isHLSInput = source.endsWith('.m3u8');

      if (mode === 'hls' || isHLSInput) {
        const resp = await startHLS(gateway, source);
        this.streamId = resp.streamId;
        this.wsUrl = resp.hlsUrl;
        return this._playHLS(resp.hlsUrl);
      }

      const info = await startStream(gateway, source);
      this.streamId = info.streamId;
      this.wsUrl = info.wsUrl;

      const finalMode = mode === 'auto' ? this._pickAutoMode() : mode;
      if (finalMode === 'webrtc') {
        return this._playWebRTC(info.streamId);
      }
      return this._playFLV(info.wsUrl);
    }

    _pickAutoMode() {
      // 默认 FLV(低延迟,1-3s)
      // iOS Safari 不支持 MSE 的 video/mp4; codecs="avc1" - 实际 iOS 17+ 支持
      const ua = navigator.userAgent;
      const isIOS = /iPad|iPhone|iPod/.test(ua);
      const isSafari = /Safari\/[\d.]+$/.test(ua) && !/Chrome|CriOS|FxiOS/.test(ua);
      if (isIOS && isSafari) {
        // iOS Safari 17+ 支持 MSE,但仍建议 HLS
        // 检测 iOS 版本
        const match = ua.match(/OS (\d+)_/);
        if (match && parseInt(match[1]) < 17) return 'hls'; // 旧 iOS 走 HLS
      }
      return 'flv';
    }

    // ===== WebSocket-FLV via MSE (核心播放路径) =====
    // 检查 MSE 是否可用
    _isMSEAvailable() {
      return typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported;
    }

    _playFLV(wsUrl) {
      this.wsUrl = wsUrl;

      // 检查 MSE 可用性
      if (!this._isMSEAvailable()) {
        this._emit('onError', '当前浏览器不支持 MSE (MediaSource Extensions),无法播放 WebSocket-FLV。尝试切换到 HLS 模式...');
        this._fallbackTried = true;
        this._fallbackToHLS();
        return;
      }

      this._initMSE();
      this._initDemuxer();
      this._connectWS(wsUrl);

      // 启动数据接收超时检测(30 秒无视频数据则报错)
      this._startDataTimeout();
    }

    _initMSE() {
      this.mse = new MediaSource();
      this.video.src = URL.createObjectURL(this.mse);

      this.mse.addEventListener('sourceopen', () => {
        this._mseReady = true;
        // 重置 currentTime,避免上一个流的播放位置残留导致新流缓冲被误删
        try { this.video.currentTime = 0; } catch (e) {}
        this._flushQueues();
      });

      this.mse.addEventListener('sourceended', () => {
        // 仅在非重置情况下报错(重置时 _destroyed 或 _resetMSE 会设为预期)
        if (!this._destroyed && !this._resetting) {
          this._emit('onError', 'MediaSource 已结束');
        }
      });

      this.mse.addEventListener('error', (e) => {
        if (!this._destroyed) {
          this._emit('onError', 'MediaSource 错误: ' + (e.target.error?.message || '未知'));
        }
      });

      this.mse.addEventListener('sourceclose', () => {
        if (!this._destroyed) {
          this._emit('onError', 'MediaSource 已关闭');
        }
      });

      // 视频元素错误处理
      this.video.addEventListener('error', () => {
        if (!this._destroyed && this.video.error) {
          const errMap = {
            1: 'MEDIA_ERR_ABORTED - 取消',
            2: 'MEDIA_ERR_NETWORK - 网络错误',
            3: 'MEDIA_ERR_DECODE - 解码错误',
            4: 'MEDIA_ERR_SRC_NOT_SUPPORTED - 源不支持(可能是 H265 编码或 MSE 不可用)'
          };
          const errMsg = errMap[this.video.error.code] || this.video.error.code;
          // 如果是源不支持且从未收到过视频数据,很可能是 H265
          if (this.video.error.code === 4 && !this._everReceivedVideo) {
            this._emit('onError', '视频错误: ' + errMsg + '。建议:1) 在服务端配置 enable_h265_transcode: true 2) 使用 HLS 模式播放');
          } else {
            this._emit('onError', '视频错误: ' + errMsg);
          }
        }
      });
    }

    _initDemuxer() {
      if (typeof FLVDemuxer === 'undefined') {
        this._emit('onError', 'FLVDemuxer 未加载,请引入 flv-demuxer.js');
        return;
      }

      this.demuxer = new FLVDemuxer();
      this.demuxer.onMetaData = (meta) => {
        // 可用于显示分辨率等信息
        if (meta.width && meta.height) {
          this._emit('onStats', { resolution: `${meta.width}x${meta.height}` });
        }
      };
      this.demuxer.onInitSegment = (type, data) => {
        this._onInitSegment(type, data);
      };
      this.demuxer.onMediaSegment = (type, data) => {
        this._onMediaSegment(type, data);
      };
      this.demuxer.onError = (msg) => {
        this._clearDataTimeout();
        this._onDemuxerError(msg);
      };
    }

    // 启动数据接收超时
    _startDataTimeout() {
      this._clearDataTimeout();
      this._lastDataTime = Date.now();
      // 数据活性检测:每 5 秒检查一次,如果 30 秒无数据则触发超时
      this._staleCheckTimer = setInterval(() => {
        if (this._destroyed || this._everReceivedVideo) {
          // 已收到视频数据,停止活性检测
          if (this._staleCheckTimer) {
            clearInterval(this._staleCheckTimer);
            this._staleCheckTimer = null;
          }
          return;
        }
        const elapsed = Date.now() - this._lastDataTime;
        if (elapsed > 30000) {
          if (this._staleCheckTimer) {
            clearInterval(this._staleCheckTimer);
            this._staleCheckTimer = null;
          }
          if (!this._fallbackTried) {
            this._fallbackTried = true;
            this._emit('onError', '30 秒内未收到视频数据,可能是源不可用或编码不支持。正在切换到 HLS 模式...');
            this._fallbackToHLS();
          } else {
            this._emit('onError', '30 秒内未收到视频数据。请检查:1) 源地址是否正确 2) 网络是否可达 3) 摄像头是否在线');
          }
        }
      }, 5000);
      // 兼容:保留旧的 _dataTimeoutTimer 引用
      this._dataTimeoutTimer = this._staleCheckTimer;
    }

    // 清除数据接收超时
    _clearDataTimeout() {
      if (this._staleCheckTimer) {
        clearInterval(this._staleCheckTimer);
        this._staleCheckTimer = null;
      }
      if (this._dataTimeoutTimer) {
        clearTimeout(this._dataTimeoutTimer);
        this._dataTimeoutTimer = null;
      }
    }

    _connectWS(wsUrl) {
      try {
        this.ws = new WebSocket(wsUrl);
      } catch (e) {
        this._emit('onError', 'WebSocket 创建失败: ' + e.message);
        return;
      }
      this.ws.binaryType = 'arraybuffer';

      this.ws.onopen = () => {
        // 连接成功,等待数据(onConnected 在收到首个 init segment 时触发)
      };

      this.ws.onmessage = (ev) => {
        if (this._destroyed) return;
        if (!this.demuxer) return;
        const data = new Uint8Array(ev.data);
        this._bytesReceived += data.length;
        this._lastDataTime = Date.now();
        this.demuxer.feed(data);
      };

      this.ws.onerror = (e) => {
        this._emit('onError', 'WebSocket 错误');
      };

      this.ws.onclose = (ev) => {
        if (this._destroyed) return;
        if (ev.code !== 1000) {
          this._emit('onError', `WebSocket 关闭 (code=${ev.code}),尝试重连...`);
        }
        // 尝试自动重连(仅非正常关闭),使用指数退避
        if (ev.code !== 1000 && !this._destroyed) {
          const delay = this._reconnectDelay;
          this._reconnectDelay = Math.min(this._reconnectDelay * 2, this._maxReconnectDelay);
          setTimeout(() => {
            if (!this._destroyed && this.wsUrl) {
              // 重连前重置 demuxer 状态(避免旧时间戳/序号污染)
              if (this.demuxer) {
                this.demuxer.reset();
              }
              // 重置 MSE SourceBuffer(需要重新接收 init segment)
              this._resetMSE();
              // 重置数据接收状态,以便重新检测数据超时
              this._everReceivedVideo = false;
              this._connectWS(this.wsUrl);
              // 重启数据超时检测(给重连后的流更多时间)
              this._startDataTimeout();
            }
          }, delay);
        }
      };
    }

    // 重置 MSE SourceBuffer(重连后需要重新接收 init segment)
    _resetMSE() {
      this._resetting = true;
      // 移除旧的 SourceBuffer
      if (this.mse && this.mse.readyState === 'open') {
        try {
          if (this.videoSB) {
            this.mse.removeSourceBuffer(this.videoSB);
            this.videoSB = null;
          }
          if (this.audioSB) {
            this.mse.removeSourceBuffer(this.audioSB);
            this.audioSB = null;
          }
        } catch (e) {}
      }
      // 清空队列
      this._videoQueue = [];
      this._audioQueue = [];
      this._resetting = false;
    }

    // 处理 demuxer 报告的编解码器不支持错误
    _onDemuxerError(msg) {
      if (this._destroyed) return;

      // H265 视频编解码不支持
      if (msg === 'UNSUPPORTED_VIDEO_CODEC:H265') {
        // 如果有源地址且尚未尝试过回退,自动回退到 HLS
        if (this.opts.source && this.opts.source !== 'demo' &&
            !this.opts.source.startsWith('ws://') &&
            !this.opts.source.startsWith('wss://') &&
            !this._fallbackTried) {
          this._fallbackTried = true;
          this._emit('onError', '检测到 H265 编码,浏览器 MSE 不支持 H265。正在自动切换到 HLS 模式...');
          this._fallbackToHLS();
          return;
        }
        this._emit('onError', 'H265 编码不被浏览器 MSE 支持。建议:1) 在服务端配置 enable_h265_transcode: true 开启转码 2) 使用 HLS 模式播放 3) 使用 WebRTC 模式');
        return;
      }

      // 非 AAC 音频编解码不支持(不致命,仅警告)
      if (msg.startsWith('UNSUPPORTED_AUDIO_CODEC:')) {
        const codec = msg.split(':')[1];
        if (!this._audioCodecWarned) {
          this._audioCodecWarned = true;
          this._emit('onError', '音频编解码 ' + codec + ' 不被 MSE 支持,将仅播放视频');
        }
        return;
      }

      // 其他错误
      this._emit('onError', msg);
    }

    // 自动回退到 HLS 模式
    async _fallbackToHLS() {
      // 清理当前 FLV 播放资源
      if (this.ws) { try { this.ws.close(); } catch (e) {} this.ws = null; }
      if (this.mse) {
        try {
          if (this.videoSB) this.mse.removeSourceBuffer(this.videoSB);
          if (this.audioSB) this.mse.removeSourceBuffer(this.audioSB);
          if (this.mse.readyState === 'open') this.mse.endOfStream();
        } catch (e) {}
      }
      this.videoSB = null;
      this.audioSB = null;
      this.mse = null;
      this._mseReady = false;

      // 停止当前流(必须等待完成,避免新流复用旧 session)
      if (this.streamId) {
        await stopStream(this._gateway(), this.streamId);
        this.streamId = null;
      }

      // 重新用 HLS 模式启动
      try {
        const gateway = this._gateway();
        const source = this.opts.source;
        const resp = await startHLS(gateway, source);
        this.streamId = resp.streamId;
        this.wsUrl = resp.hlsUrl;
        this._playHLS(resp.hlsUrl);
      } catch (e) {
        this._emit('onError', 'HLS 回退失败: ' + e.message + '。请在服务端配置 enable_h265_transcode: true');
      }
    }

    _onInitSegment(type, data) {
      if (!this._mseReady) {
        // MSE 还没准备好,排队
        if (type === 'video') this._videoQueue.push({ kind: 'init', data });
        else this._audioQueue.push({ kind: 'init', data });
        return;
      }

      let codec;
      if (type === 'video') {
        // 标记已收到视频数据,清除超时
        this._everReceivedVideo = true;
        this._clearDataTimeout();
        // 重连成功,重置退避
        this._reconnectDelay = 2000;
        // 从 demuxer 获取 codec string
        codec = this.demuxer.sps ? avcCodecString(this.demuxer.sps) : 'avc1.42E01E';
        // 预检编解码器是否被浏览器支持
        if (this.mse.isTypeSupported && !this.mse.isTypeSupported(`video/mp4; codecs="${codec}"`)) {
          this._emit('onError', `浏览器不支持视频编解码: ${codec}。建议:1) 在服务端配置 enable_h265_transcode: true 2) 使用 HLS 模式播放`);
          if (!this._fallbackTried) {
            this._fallbackTried = true;
            this._fallbackToHLS();
          }
          return;
        }
        if (!this.videoSB) {
          try {
            this.videoSB = this.mse.addSourceBuffer(`video/mp4; codecs="${codec}"`);
            this.videoSB.mode = 'segments';
            this.videoSB.addEventListener('updateend', () => this._onSBUpdateEnd('video'));
            this.videoSB.addEventListener('error', (e) => {
              this._emit('onError', 'videoSB 错误: ' + e.target.error?.message);
            });
          } catch (e) {
            this._emit('onError', '创建 videoSB 失败: ' + e.message + ' (codec=' + codec + ')');
            return;
          }
        }
        this._appendBuffer(this.videoSB, data, 'video');
        this._emit('onConnected', { mode: 'flv', wsUrl: this.wsUrl });
        // 启动实时统计
        this._startStats();
      } else {
        if (!this.audioSB) {
          try {
            this.audioSB = this.mse.addSourceBuffer('audio/mp4; codecs="mp4a.40.2"');
            this.audioSB.mode = 'segments';
            this.audioSB.addEventListener('updateend', () => this._onSBUpdateEnd('audio'));
          } catch (e) {
            // 音频 SourceBuffer 创建失败不致命,继续只有视频
            return;
          }
        }
        this._appendBuffer(this.audioSB, data, 'audio');
      }
    }

    _onMediaSegment(type, data) {
      if (!this._mseReady) {
        if (type === 'video') this._videoQueue.push({ kind: 'media', data });
        else this._audioQueue.push({ kind: 'media', data });
        return;
      }

      const sb = type === 'video' ? this.videoSB : this.audioSB;
      if (!sb) {
        // SourceBuffer 还没创建,排队
        if (type === 'video') this._videoQueue.push({ kind: 'media', data });
        else this._audioQueue.push({ kind: 'media', data });
        return;
      }

      // 统计计数
      if (type === 'video') this._videoSegCount++;
      else this._audioSegCount++;

      this._appendBuffer(sb, data, type);
    }

    _appendBuffer(sb, data, type) {
      if (this._destroyed) return;
      // 检查 SourceBuffer 是否仍可用
      if (!sb || !this.mse || this.mse.readyState !== 'open') {
        // MSE 已关闭,排队等待重连后处理
        if (type === 'video') this._videoQueue.push({ kind: 'media', data });
        else this._audioQueue.push({ kind: 'media', data });
        return;
      }
      if (sb.updating) {
        // SourceBuffer 正在更新,排队
        if (type === 'video') this._videoQueue.push({ kind: 'media', data });
        else this._audioQueue.push({ kind: 'media', data });
        return;
      }
      try {
        // appendBuffer 接受 ArrayBuffer 或 ArrayBufferView (Uint8Array)
        // 直接传入 Uint8Array 更安全,避免 data.buffer 返回整个底层 buffer
        sb.appendBuffer(data);
      } catch (e) {
        if (e.name === 'QuotaExceededError') {
          this._cleanBuffer(sb);
          try {
            sb.appendBuffer(data);
          } catch (e2) {}
        } else if (e.name === 'InvalidStateError') {
          // SourceBuffer 已被移除,排队等待重连
          if (type === 'video') this._videoQueue.push({ kind: 'media', data });
          else this._audioQueue.push({ kind: 'media', data });
        } else {
          // 其他错误:排队等待重连,不丢弃数据
          if (type === 'video') this._videoQueue.push({ kind: 'media', data });
          else this._audioQueue.push({ kind: 'media', data });
        }
      }
    }

    _onSBUpdateEnd(type) {
      if (this._destroyed) return;
      if (this._inSBUpdate) return;  // 防止递归
      this._inSBUpdate = true;
      try {
        const queue = type === 'video' ? this._videoQueue : this._audioQueue;
        const sb = type === 'video' ? this.videoSB : this.audioSB;
        if (!sb || sb.updating) return;

        // 检查 SourceBuffer 是否仍 attached
        try { void sb.buffered; } catch (e) { return; }

        // 处理排队的 init segment 优先
        let initIdx = queue.findIndex(q => q.kind === 'init');
        if (initIdx >= 0) {
          const item = queue.splice(initIdx, 1)[0];
          this._appendBuffer(sb, item.data, type);
          return;
        }

        // 处理排队的 media segment
        // 优化:当队列深度 >1 时,合并多个段为一次 appendBuffer 调用
        // MSE 支持一次追加多个 fMP4 segment,减少 appendBuffer 调用次数
        if (queue.length === 1) {
          const item = queue.shift();
          this._appendBuffer(sb, item.data, type);
        } else if (queue.length > 1) {
          // 合并最多 8 个段
          const maxBatch = Math.min(queue.length, 8);
          let totalLen = 0;
          for (let i = 0; i < maxBatch; i++) {
            totalLen += queue[i].data.length;
          }
          const merged = new Uint8Array(totalLen);
          let mOff = 0;
          for (let i = 0; i < maxBatch; i++) {
            merged.set(queue[i].data, mOff);
            mOff += queue[i].data.length;
          }
          queue.splice(0, maxBatch);
          this._appendBuffer(sb, merged, type);
        }

        // 控制直播延迟:自适应播放速率 + 丢弃过期的缓冲
        this._controlLatency();

        // 自动播放(首次有数据时)
        if (this.video.paused && this.opts.autoplay) {
          try {
            if (this.videoSB && this.videoSB.buffered.length > 0) {
              this.video.play().catch(() => {});
            }
          } catch (e) {}
        }
      } finally {
        this._inSBUpdate = false;
      }
    }

    _flushQueues() {
      // MSE ready 后,flush 排队的数据
      ['video', 'audio'].forEach(type => {
        const queue = type === 'video' ? this._videoQueue : this._audioQueue;
        if (queue.length === 0) return;
        // 先处理 init
        const initIdx = queue.findIndex(q => q.kind === 'init');
        if (initIdx >= 0) {
          const item = queue.splice(initIdx, 1)[0];
          this._onInitSegment(type, item.data);
        }
      });
    }

    _controlLatency() {
      if (!this.videoSB || !this.mse || this.mse.readyState !== 'open') return;
      try {
        if (this.videoSB.buffered.length === 0) return;
        const buffered = this.videoSB.buffered;
        const end = buffered.end(buffered.length - 1);
        const start = buffered.start(0);
        const targetLatency = this.opts.liveBufferLatency || 1.5;
        const maxLatency = this.opts.liveBufferMaxLatency || 4;
        const behind = end - this.video.currentTime;

        // 如果 currentTime 在缓冲范围之外(如切换流后未重置),seek 到直播边缘
        if (this.video.currentTime < start - 1 || this.video.currentTime > end + 1) {
          this.video.currentTime = Math.max(start, end - targetLatency);
          this.video.playbackRate = 1.0;
          return;
        }

        // 超过最大延迟,硬追赶(直接 seek)
        if (behind > maxLatency) {
          this.video.currentTime = end - targetLatency;
          this.video.playbackRate = 1.0;
        }
        // 超过目标延迟但未超最大,加速播放追赶(平滑,无视觉跳变)
        else if (behind > targetLatency * 1.5) {
          this.video.playbackRate = 1.1;
        }
        // 在目标延迟附近,恢复正常速率
        else if (behind < targetLatency * 0.5) {
          this.video.playbackRate = 1.0;
        }

        // 清理已播放过的旧缓冲(保留 10 秒)
        // 仅当 currentTime 在缓冲范围内时才清理,避免误删
        if (this.video.currentTime > 10 && this.video.currentTime >= start && this.video.currentTime <= end) {
          for (const sb of [this.videoSB, this.audioSB]) {
            if (sb && !sb.updating && sb.buffered.length > 0) {
              sb.remove(0, this.video.currentTime - 5);
            }
          }
        }
      } catch (e) {
        // SourceBuffer 可能已被移除
      }
    }

    _cleanBuffer(sb) {
      if (!sb || !this.mse || this.mse.readyState !== 'open') return;
      try {
        if (sb.buffered.length === 0) return;
        const end = sb.buffered.end(sb.buffered.length - 1);
        if (end > 10) {
          // 保留最近 8 秒的数据
          sb.remove(0, end - 8);
        }
      } catch (e) {}
    }

    // ===== 实时统计 =====
    _startStats() {
      if (this._statsTimer) return;
      this._lastStatsTime = performance.now();
      this._videoSegCount = 0;
      this._audioSegCount = 0;
      this._bytesReceived = 0;
      this._statsTimer = setInterval(() => {
        if (this._destroyed) return;
        const now = performance.now();
        const elapsed = (now - this._lastStatsTime) / 1000;
        if (elapsed <= 0) return;
        const stats = {
          videoFps: Math.round(this._videoSegCount / elapsed),
          audioFps: Math.round(this._audioSegCount / elapsed),
          bitrateKbps: Math.round(this._bytesReceived * 8 / elapsed / 1000),
          queueDepth: this._videoQueue.length + this._audioQueue.length,
        };
        // 缓冲健康度
        if (this.videoSB && this.videoSB.buffered.length > 0) {
          const end = this.videoSB.buffered.end(this.videoSB.buffered.length - 1);
          stats.bufferEnd = end;
          stats.currentTime = this.video.currentTime;
          stats.latency = end - this.video.currentTime;
          stats.playbackRate = this.video.playbackRate;
        }
        this._emit('onStats', stats);
        // 重置计数器
        this._videoSegCount = 0;
        this._audioSegCount = 0;
        this._bytesReceived = 0;
        this._lastStatsTime = now;
      }, this.opts.statsInterval || 1000);
    }

    _stopStats() {
      if (this._statsTimer) {
        clearInterval(this._statsTimer);
        this._statsTimer = null;
      }
    }

    // ===== HLS 播放 =====

    // 动态加载 hls.js(先本地,再 CDN)
    _loadHlsJs() {
      return new Promise((resolve, reject) => {
        if (typeof Hls !== 'undefined') {
          resolve();
          return;
        }
        const tryLoad = (src, onErrorFallback) => {
          const script = document.createElement('script');
          script.src = src;
          script.onload = () => {
            if (typeof Hls !== 'undefined') {
              resolve();
            } else {
              onErrorFallback();
            }
          };
          script.onerror = onErrorFallback;
          document.head.appendChild(script);
        };
        // 优先本地路径,失败后从 CDN 加载
        tryLoad('/hls.min.js', () => {
          tryLoad('https://cdn.jsdelivr.net/npm/hls.js@1.5.15/dist/hls.min.js', () => {
            reject(new Error('hls.js 加载失败(本地和 CDN 均不可达)'));
          });
        });
      });
    }

    async _playHLS(hlsUrl) {
      this.wsUrl = hlsUrl;

      // 路径 1: 原生 HLS 支持(Safari/iOS)
      if (this.video.canPlayType('application/vnd.apple.mpegurl')) {
        this._playNativeHLS(hlsUrl);
        return;
      }

      // 路径 2: 使用 hls.js(Chrome/Firefox/Edge)
      try {
        if (typeof Hls === 'undefined') {
          this._emit('onError', '正在加载 hls.js 库...');
          await this._loadHlsJs();
        }
        this._playWithHlsJs(hlsUrl);
      } catch (e) {
        this._emit('onError', 'HLS 播放失败: ' + e.message + '。建议使用 Safari 或引入 hls.js');
      }
    }

    // 原生 HLS 播放(Safari/iOS)
    _playNativeHLS(hlsUrl) {
      // 清理旧的 HLS 事件监听器
      this._clearHlsListeners();

      this.video.src = hlsUrl;

      // 超时检测:10 秒内未加载到 metadata 则报错
      this._hlsNativeTimeout = setTimeout(() => {
        if (!this._destroyed && this.video.readyState === 0) {
          this._emit('onError', 'HLS 原生播放超时(10 秒无数据)。请检查:1) 流地址是否正确 2) 摄像头是否在线 3) 服务端 HLS 切片是否正常');
        }
      }, 10000);

      const onLoadedMetadata = () => {
        clearTimeout(this._hlsNativeTimeout);
        this._emit('onConnected', { mode: 'hls', hlsUrl, native: true });
        this.video.play().catch(() => {});
      };

      const onError = () => {
        clearTimeout(this._hlsNativeTimeout);
        if (!this._destroyed && this.video.error) {
          const errMap = {
            1: 'MEDIA_ERR_ABORTED',
            2: 'MEDIA_ERR_NETWORK',
            3: 'MEDIA_ERR_DECODE',
            4: 'MEDIA_ERR_SRC_NOT_SUPPORTED'
          };
          this._emit('onError', 'HLS 原生播放错误: ' + (errMap[this.video.error.code] || this.video.error.code));
        }
      };

      this.video.addEventListener('loadedmetadata', onLoadedMetadata, { once: true });
      this.video.addEventListener('error', onError, { once: true });
      this._hlsListeners = [
        { event: 'loadedmetadata', fn: onLoadedMetadata },
        { event: 'error', fn: onError }
      ];
    }

    // 使用 hls.js 播放(Chrome/Firefox/Edge)
    _playWithHlsJs(hlsUrl) {
      if (!Hls.isSupported()) {
        this._emit('onError', 'hls.js 不支持当前浏览器(MSE 不可用)');
        return;
      }

      const hls = new Hls({
        liveDurationInfinity: true,
        lowLatencyMode: false,
        enableWorker: true,
        // 错误恢复配置(直播流刚启动时可能没有切片,需要更多重试)
        fragLoadingMaxRetry: 10,
        fragLoadingRetryDelay: 1000,
        manifestLoadingMaxRetry: 10,
        manifestLoadingRetryDelay: 1000,
        levelLoadingMaxRetry: 10,
        levelLoadingRetryDelay: 1000,
      });

      hls.loadSource(hlsUrl);
      hls.attachMedia(this.video);

      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        this._emit('onConnected', { mode: 'hls', hlsUrl, engine: 'hls.js' });
        this.video.play().catch(() => {});
      });

      let mediaErrorCount = 0;
      let networkErrorCount = 0;
      hls.on(Hls.Events.ERROR, (_, data) => {
        console.log('[HLS-ERROR]', JSON.stringify({type: data.type, details: data.details, fatal: data.fatal, reason: data.reason, error: data.error?.message}));
        if (!data.fatal) return;

        // 尝试自动恢复
        switch (data.type) {
          case Hls.ErrorTypes.NETWORK_ERROR:
            networkErrorCount++;
            if (networkErrorCount > 5) {
              this._emit('onError', 'HLS 网络错误: ' + data.details + ' (恢复失败,已达重试上限)');
              hls.destroy();
              this._hls = null;
            } else {
              this._emit('onError', 'HLS 网络错误: ' + data.details + ' (尝试恢复...' + networkErrorCount + '/5)');
              // manifestLoadError 时重新加载整个源,startLoad 可能不够
              if (data.details === 'manifestLoadError') {
                setTimeout(() => { hls.loadSource(hlsUrl); }, 1000 * networkErrorCount);
              } else {
                hls.startLoad();
              }
            }
            break;
          case Hls.ErrorTypes.MEDIA_ERROR:
            mediaErrorCount++;
            if (mediaErrorCount > 3) {
              this._emit('onError', 'HLS 媒体错误: ' + data.details + ' (恢复失败)');
              hls.destroy();
              this._hls = null;
            } else {
              this._emit('onError', 'HLS 媒体错误: ' + data.details + ' (尝试恢复...)');
              hls.recoverMediaError();
            }
            break;
          default:
            this._emit('onError', 'HLS 致命错误: ' + data.details);
            hls.destroy();
            this._hls = null;
            break;
        }
      });

      this._hls = hls;
    }

    // 清理 HLS 事件监听器
    _clearHlsListeners() {
      if (this._hlsNativeTimeout) {
        clearTimeout(this._hlsNativeTimeout);
        this._hlsNativeTimeout = null;
      }
      if (this._hlsListeners) {
        for (const { event, fn } of this._hlsListeners) {
          this.video.removeEventListener(event, fn);
        }
        this._hlsListeners = null;
      }
    }

    // ===== WebRTC WHEP =====
    async _playWebRTC(streamId) {
      const gateway = this._gateway();
      const pc = new RTCPeerConnection({
        iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
      });
      this.pc = pc;
      pc.addTransceiver('video', { direction: 'recvonly' });
      pc.addTransceiver('audio', { direction: 'recvonly' });

      // 收到的轨道可能分多次到达,统一管理 MediaStream
      let remoteStream = new MediaStream();
      this.video.srcObject = remoteStream;

      pc.ontrack = (event) => {
        // pion/webrtc 不创建 MediaStream,event.streams 可能为空
        // 需要手动创建 MediaStream 并添加 track
        if (event.streams && event.streams.length > 0) {
          // 使用服务端提供的 stream
          remoteStream = event.streams[0];
        } else {
          // pion 场景:将 track 添加到我们创建的 stream
          remoteStream.addTrack(event.track);
        }
        this.video.srcObject = remoteStream;
        this._emit('onConnected', { mode: 'webrtc' });
        this.video.play().catch(() => {});
      };

      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);

      // 等待 ICE 收集完成(非 trickle ICE,WHEP 需要完整 SDP)
      await new Promise((resolve) => {
        if (pc.iceGatheringState === 'complete') return resolve();
        const checkState = () => {
          if (pc.iceGatheringState === 'complete') {
            pc.removeEventListener('icegatheringstatechange', checkState);
            resolve();
          }
        };
        pc.addEventListener('icegatheringstatechange', checkState);
        // 超时保护(3 秒)
        setTimeout(resolve, 3000);
      });

      try {
        const resp = await fetch(`${gateway}/whep/${streamId}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/sdp' },
          body: pc.localDescription.sdp,
        });
        if (!resp.ok) {
          // 读取错误响应体,显示具体错误信息
          const errText = await resp.text().catch(() => '');
          this._emit('onError', `WHEP 协商失败: ${resp.status} - ${errText || resp.statusText}`);
          return;
        }
        const answerSdp = await resp.text();
        await pc.setRemoteDescription({ type: 'answer', sdp: answerSdp });
      } catch (e) {
        this._emit('onError', 'WHEP 错误: ' + e.message);
      }
    }

    // ===== Canvas Demo 播放(测试画面,无需服务端) =====
    _playDemo() {
      const canvas = document.createElement('canvas');
      canvas.width = 640;
      canvas.height = 360;
      const ctx = canvas.getContext('2d');
      this._demoCanvas = canvas;
      this._demoRunning = true;

      let frame = 0;
      const colors = ['#e74c3c', '#e67e22', '#f1c40f', '#2ecc71', '#1abc9c', '#3498db', '#9b59b6'];
      const drawFrame = () => {
        if (!this._demoRunning) return;
        const colorIdx = Math.floor(frame / 25) % colors.length;
        const color = colors[colorIdx];
        // Background
        ctx.fillStyle = color;
        ctx.fillRect(0, 0, 640, 360);
        // Color bars
        const barW = 640 / colors.length;
        for (let i = 0; i < colors.length; i++) {
          ctx.fillStyle = colors[i];
          ctx.fillRect(i * barW, 0, barW, 40);
        }
        // Text
        ctx.fillStyle = 'white';
        ctx.font = 'bold 24px sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('StreamBridge Demo', 320, 200);
        ctx.font = '14px sans-serif';
        ctx.fillText('Test Pattern - Frame ' + frame, 320, 230);
        ctx.fillText(new Date().toLocaleTimeString(), 320, 260);
        // Moving circle
        const x = 320 + Math.cos(frame * 0.05) * 100;
        const y = 300 + Math.sin(frame * 0.05) * 30;
        ctx.beginPath();
        ctx.arc(x, y, 15, 0, Math.PI * 2);
        ctx.fillStyle = 'white';
        ctx.fill();
        frame++;
        this._demoTimer = requestAnimationFrame(drawFrame);
      };
      drawFrame();

      // Use canvas captureStream for video playback
      const stream = canvas.captureStream(10); // 10 fps
      this.video.srcObject = stream;
      this.video.play().catch(() => {});
      this._emit('onConnected', { mode: 'demo' });

      // Emit stats periodically
      this._demoStatsTimer = setInterval(() => {
        this._emit('onStats', {
          fps: 10,
          resolution: '640x360',
          viewers: 1
        });
      }, 1000);
    }

    _emit(name, ...args) {
      if (typeof this.opts[name] === 'function') {
        try { this.opts[name](...args); } catch (e) {}
      }
    }

    // ===== 公共方法 =====
    destroy() {
      this._destroyed = true;
      this._clearDataTimeout();
      this._clearHlsListeners();
      this._stopStats();
      this._clearDataTimeout();
      if (this.ws) { try { this.ws.close(); } catch (e) {} this.ws = null; }
      if (this.pc) { try { this.pc.close(); } catch (e) {} this.pc = null; }
      if (this._hls) { try { this._hls.destroy(); } catch (e) {} this._hls = null; }
      // Clean up demo canvas resources
      if (this._demoTimer) { cancelAnimationFrame(this._demoTimer); this._demoTimer = null; }
      if (this._demoStatsTimer) { clearInterval(this._demoStatsTimer); this._demoStatsTimer = null; }
      this._demoRunning = false;
      this._demoCanvas = null;
      if (this.video) {
        this.video.pause();
        this.video.src = '';
        this.video.srcObject = null;
      }
      if (this.mse) {
        try {
          if (this.videoSB) this.mse.removeSourceBuffer(this.videoSB);
          if (this.audioSB) this.mse.removeSourceBuffer(this.audioSB);
          if (this.mse.readyState === 'open') this.mse.endOfStream();
        } catch (e) {}
      }
      this.videoSB = null;
      this.audioSB = null;
      this.mse = null;
      this.demuxer = null;
      this._videoQueue = [];
      this._audioQueue = [];
      if (this.streamId) {
        stopStream(this._gateway(), this.streamId);
      }
    }

    // 便捷方法:截图
    snapshot() {
      if (!this.video || !this.video.videoWidth) return null;
      const canvas = document.createElement('canvas');
      canvas.width = this.video.videoWidth;
      canvas.height = this.video.videoHeight;
      canvas.getContext('2d').drawImage(this.video, 0, 0);
      return canvas.toDataURL('image/png');
    }
  }

  global.StreamBridge = {
    version: STREAMBRIDGE_VERSION,
    play,
    startStream,
    startHLS,
    stopStream,
    listStreams,
    Player,
  };
})(typeof window !== 'undefined' ? window : this);
