/*!
 * StreamBridge 微信小程序 SDK v1.0.0
 *
 * 微信小程序的 <video> 组件原生支持 HLS 播放,因此本 SDK 默认使用 HLS 输出
 *
 * 用法:
 *   // 在页面 JS 中
 *   const StreamBridge = require('../../streambridge.js');
 *
 *   Page({
 *     data: { hlsUrl: '' },
 *     onPlay() {
 *       StreamBridge.play({
 *         gateway: 'http://192.168.1.50:8080',
 *         source: 'rtsp://admin:abc12345@192.168.1.64:554/Streaming/Channels/101',
 *         onSuccess: (res) => this.setData({ hlsUrl: res.hlsUrl }),
 *         onError: (err) => wx.showToast({ title: err, icon: 'none' }),
 *       });
 *     },
 *     onStop() {
 *       StreamBridge.stop(this.data.gateway, this.data.streamId);
 *     }
 *   });
 *
 *   <!-- WXML -->
 *   <video src="{{hlsUrl}}" controls autoplay></video>
 *   <button bindtap="onPlay">播放</button>
 *
 * 注意:
 *   - 小程序需在 app.json 的 networkTimeout 中放宽 request 超时
 *   - 需在微信小程序后台配置 request/socket/downloadFile 合法域名
 *   - iOS 微信不支持 ws:// 的 WebSocket-FLV,只支持 HLS
 *
 * License: MIT
 */
(function (global) {
  'use strict';

  const VERSION = '1.0.0';

  /**
   * 启动流并返回 HLS 播放地址
   * @param {Object} options
   * @param {string} options.gateway 网关地址,如 http://localhost:8080
   * @param {string} options.source 流地址(rtsp/rtmp/file)
   * @param {Function} [options.onSuccess] 成功回调 ({streamId, hlsUrl})
   * @param {Function} [options.onError] 错误回调
   * @param {string} [options.format] 输出格式,默认 hls(也可传 flv 用 WebSocket-FLV)
   */
  function play(options) {
    const { gateway, source, format = 'hls', onSuccess, onError } = options;

    if (format === 'flv') {
      // WebSocket-FLV 模式(仅 Android 微信支持)
      wx.request({
        url: `${gateway}/api/play?url=${encodeURIComponent(source)}&format=flv`,
        method: 'GET',
        success: (res) => {
          if (res.statusCode === 200 && res.data && res.data.wsUrl) {
            // 注意:微信 <video> 组件不直接支持 ws-flv,需通过 WebSocket + 自定义解码
            // 推荐使用 HLS 模式,这里仅返回 wsUrl 供高级用户使用
            onSuccess && onSuccess({ streamId: res.data.streamId, wsUrl: res.data.wsUrl, mode: 'flv' });
          } else {
            onError && onError('启动流失败: ' + res.statusCode);
          }
        },
        fail: (err) => onError && onError(err.errMsg || '网络请求失败'),
      });
      return;
    }

    // 默认 HLS 模式
    wx.request({
      url: `${gateway}/hls/play?url=${encodeURIComponent(source)}`,
      method: 'GET',
      success: (res) => {
        if (res.statusCode === 200 && res.data && res.data.hlsUrl) {
          onSuccess && onSuccess({
            streamId: res.data.streamId,
            hlsUrl: res.data.hlsUrl,
            mode: 'hls',
          });
        } else {
          onError && onError('HLS 启动失败: ' + res.statusCode);
        }
      },
      fail: (err) => onError && onError(err.errMsg || '网络请求失败'),
    });
  }

  /**
   * 停止流
   * @param {string} gateway
   * @param {string} streamId
   * @param {Function} [cb]
   */
  function stop(gateway, streamId, cb) {
    wx.request({
      url: `${gateway}/api/stop?id=${streamId}`,
      method: 'POST',
      success: () => cb && cb(),
      fail: () => cb && cb(false),
    });
  }

  /**
   * 查询活跃流列表
   * @param {string} gateway
   * @param {Function} cb
   */
  function listStreams(gateway, cb) {
    wx.request({
      url: `${gateway}/api/streams`,
      success: (res) => cb && cb(res.data),
      fail: (err) => cb && cb(null, err.errMsg),
    });
  }

  /**
   * 健康检查
   * @param {string} gateway
   * @param {Function} cb
   */
  function health(gateway, cb) {
    wx.request({
      url: `${gateway}/api/health`,
      success: (res) => cb && cb(res.data),
      fail: (err) => cb && cb(null, err.errMsg),
    });
  }

  global.StreamBridge = {
    version: VERSION,
    play,
    stop,
    listStreams,
    health,
  };
})(typeof module !== 'undefined' ? module.exports : this);
