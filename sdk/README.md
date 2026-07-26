# StreamBridge 客户端 SDK

> 一个文件,让摄像头连上浏览器 / 小程序 / App

社区版永久免费,支持以下平台:

| 平台 | 文件 | 播放方式 | 推荐场景 |
|------|------|---------|---------|
| 浏览器 (JavaScript) | [javascript/streambridge.js](javascript/streambridge.js) | WebSocket-FLV (MSE) / HLS / WebRTC | Web 网站、后台管理、H5 |
| 微信小程序 | [wechat-miniprogram/streambridge.js](wechat-miniprogram/streambridge.js) | HLS (原生 `<video>` 组件) | 微信小程序直播/监控 |
| Android | [android/StreamBridgeView.java](android/StreamBridgeView.java) | HLS (ExoPlayer) | 安卓 App |
| iOS | [ios/StreamBridgePlayerView.swift](ios/StreamBridgePlayerView.swift) | HLS (AVPlayer) | iOS App |

## 通用集成流程

1. 部署 StreamBridge 网关(参考项目根 README)
2. 引入对应平台的 SDK 文件
3. 调用 `play({ gateway, source })` 即可

## 浏览器示例

```html
<!DOCTYPE html>
<html>
<body>
  <div id="player" style="width:640px;height:360px"></div>
  <script src="http://localhost:8080/streambridge.js"></script>
  <script>
    StreamBridge.play({
      container: '#player',
      source: 'rtsp://admin:abc12345@192.168.1.64:554/Streaming/Channels/101',
      mode: 'auto',
      onConnected: (info) => console.log('已连接', info),
      onError: (err) => console.error('错误', err),
    });
  </script>
</body>
</html>
```

## 微信小程序示例

```js
// page.js
const StreamBridge = require('../../streambridge.js');

Page({
  data: { hlsUrl: '' },
  onPlay() {
    StreamBridge.play({
      gateway: 'http://192.168.1.50:8080',
      source: 'rtsp://admin:abc12345@192.168.1.64:554/Streaming/Channels/101',
      onSuccess: (res) => this.setData({ hlsUrl: res.hlsUrl }),
      onError: (err) => wx.showToast({ title: err, icon: 'none' }),
    });
  }
});
```

```xml
<!-- page.wxml -->
<video src="{{hlsUrl}}" controls autoplay style="width:100%;height:300px;"></video>
<button bindtap="onPlay">播放</button>
```

> 小程序需在微信公众平台配置 request 合法域名。

## Android 示例

```kotlin
val player = findViewById<StreamBridgeView>(R.id.player)
player.setPlayListener(object : StreamBridgeView.PlayListener {
    override fun onConnected(hlsUrl: String) { /* 已连接 */ }
    override fun onError(message: String) { Toast.makeText(this@MainActivity, message, Toast.LENGTH_SHORT).show() }
})
player.play("http://192.168.1.50:8080",
            "rtsp://admin:abc12345@192.168.1.64:554/Streaming/Channels/101")
```

## iOS 示例

```swift
import SwiftUI

struct ContentView: View {
    @StateObject var player = StreamBridgePlayerView()

    var body: some View {
        VStack {
            player.view
                .frame(height: 360)
            Button("播放") {
                player.play(gateway: "http://192.168.1.50:8080",
                            source: "rtsp://admin:abc12345@192.168.1.64:554/Streaming/Channels/101")
            }
        }
        .padding()
    }
}
```

## License

MIT License - 社区版永久免费,可商用
