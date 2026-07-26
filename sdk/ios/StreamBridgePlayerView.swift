//
//  StreamBridgePlayerView.swift
//  StreamBridge iOS SDK v1.0.0
//
//  基于 AVPlayer 的 HLS 播放器封装
//
//  依赖:
//   - iOS 13+
//   - 无需第三方库
//
//  用法:
//   import SwiftUI
//
//   struct ContentView: View {
//     @StateObject var player = StreamBridgePlayerView()
//
//     var body: some View {
//       VStack {
//         player.view
//         Button("播放") {
//           player.play(gateway: "http://192.168.1.50:8080",
//                       source: "rtsp://admin:abc12345@192.168.1.64:554/Streaming/Channels/101")
//         }
//       }
//     }
//   }
//
//  License: MIT
//

import SwiftUI
import AVKit
import Combine

/// StreamBridge iOS 播放器视图,基于 AVPlayer + HLS
public final class StreamBridgePlayerView: ObservableObject {

    @Published public var isConnecting = false
    @Published public var errorMessage: String?

    private var player: AVPlayer?
    private var gateway: String?
    private var streamId: String?
    private var observer: AnyCancellable?

    public init() {}

    /// SwiftUI 视图
    public var view: some View {
        VideoPlayer(player: player)
            .onDisappear { stop() }
    }

    /// 用于 UIKit 的 AVPlayerLayer
    public func makePlayerLayer() -> AVPlayerLayer {
        let layer = AVPlayerLayer()
        layer.player = player
        layer.videoGravity = .resizeAspect
        return layer
    }

    /// 播放 RTSP/RTMP/FILE 流(通过网关转换为 HLS)
    /// - Parameters:
    ///   - gateway: 网关地址,如 http://192.168.1.50:8080
    ///   - source: 流地址
    public func play(gateway: String, source: String) {
        self.gateway = gateway
        self.isConnecting = true
        self.errorMessage = nil

        guard let url = URL(string: "\(gateway)/hls/play?url=\(source.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? source)") else {
            self.errorMessage = "无效的 URL"
            self.isConnecting = false
            return
        }

        var req = URLRequest(url: url)
        req.httpMethod = "GET"
        req.timeoutInterval = 10

        URLSession.shared.dataTask(with: req) { [weak self] data, response, error in
            DispatchQueue.main.async {
                guard let self = self else { return }
                self.isConnecting = false
                if let error = error {
                    self.errorMessage = "启动流失败: \(error.localizedDescription)"
                    return
                }
                guard let data = data,
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let hlsUrl = json["hlsUrl"] as? String else {
                    self.errorMessage = "解析响应失败"
                    return
                }
                self.streamId = json["streamId"] as? String
                self.playHLS(hlsUrl)
            }
        }.resume()
    }

    /// 直接播放 HLS URL
    public func playHLS(_ hlsUrl: String) {
        guard let url = URL(string: hlsUrl) else {
            self.errorMessage = "无效的 HLS URL"
            return
        }
        let item = AVPlayerItem(url: url)
        if player == nil {
            player = AVPlayer(playerItem: item)
        } else {
            player?.replaceCurrentItem(with: item)
        }
        player?.play()

        // 监听错误
        observer = item.publisher(for: \.status)
            .sink { [weak self] status in
                if status == .failed {
                    self?.errorMessage = "播放失败: \(item.error?.localizedDescription ?? "未知错误")"
                }
            }
    }

    /// 停止播放
    public func stop() {
        player?.pause()
        player?.replaceCurrentItem(with: nil)

        if let gateway = gateway, let streamId = streamId {
            var req = URLRequest(url: URL(string: "\(gateway)/api/stop?id=\(streamId)")!)
            req.httpMethod = "POST"
            URLSession.shared.dataTask(with: req).resume()
        }
    }

    deinit {
        observer?.cancel()
        player?.pause()
    }
}

/// SwiftUI 视图扩展
public extension View {
    /// 便捷方法:绑定 StreamBridge 播放器
    func streamBridgePlayer(_ player: StreamBridgePlayerView) -> some View {
        self.background(player.view)
    }
}
