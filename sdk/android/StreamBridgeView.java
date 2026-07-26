/*
 * StreamBridge Android SDK v1.0.0
 *
 * 基于 ExoPlayer 的 HLS / HTTP-FLV 播放器封装
 *
 * 依赖:
 *   implementation 'com.google.android.exoplayer:exoplayer-core:2.19.1'
 *   implementation 'com.google.android.exoplayer:exoplayer-hls:2.19.1'
 *
 * 用法:
 *   <io.streambridge.sdk.StreamBridgeView
 *     android:id="@+id/player"
 *     android:layout_width="match_parent"
 *     android:layout_height="wrap_content" />
 *
 *   StreamBridgeView player = findViewById(R.id.player);
 *   player.play("http://192.168.1.50:8080",
 *               "rtsp://admin:abc12345@192.168.1.64:554/Streaming/Channels/101");
 *
 * License: MIT
 */
package io.streambridge.sdk;

import android.content.Context;
import android.net.Uri;
import android.util.AttributeSet;
import android.view.ViewGroup;
import android.widget.FrameLayout;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;

import com.google.android.exoplayer2.ExoPlayer;
import com.google.android.exoplayer2.MediaItem;
import com.google.android.exoplayer2.source.hls.HlsMediaSource;
import com.google.android.exoplayer2.source.MediaSource;
import com.google.android.exoplayer2.upstream.DefaultHttpDataSource;
import com.google.android.exoplayer2.upstream.DefaultDataSourceFactory;
import com.google.android.exoplayer2.util.Util;

import java.io.IOException;

import okhttp3.Call;
import okhttp3.Callback;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;
import org.json.JSONObject;

/**
 * StreamBridge 播放视图,封装 ExoPlayer + HLS 输出
 */
public class StreamBridgeView extends FrameLayout {

    private ExoPlayer player;
    private OkHttpClient http;
    private String gateway;
    private String streamId;
    private PlayListener listener;

    public interface PlayListener {
        void onConnected(String hlsUrl);
        void onError(String message);
    }

    public StreamBridgeView(@NonNull Context context) {
        super(context);
        init();
    }

    public StreamBridgeView(@NonNull Context context, @Nullable AttributeSet attrs) {
        super(context, attrs);
        init();
    }

    public StreamBridgeView(@NonNull Context context, @Nullable AttributeSet attrs, int defStyleAttr) {
        super(context, attrs, defStyleAttr);
        init();
    }

    private void init() {
        http = new OkHttpClient();
        player = new ExoPlayer.Builder(getContext()).build();
        // 用 AspectRatioFrameLayout 包裹 SurfaceView 更佳,这里简化为直接 attach
        setLayoutParams(new LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.WRAP_CONTENT));
    }

    public void setPlayListener(PlayListener l) { this.listener = l; }

    /**
     * 播放 RTSP/RTMP/FILE 流
     * @param gateway 网关地址,如 http://192.168.1.50:8080
     * @param sourceUrl 流地址
     */
    public void play(@NonNull String gateway, @NonNull String sourceUrl) {
        this.gateway = gateway;
        Request req = new Request.Builder()
            .url(gateway + "/hls/play?url=" + Uri.encode(sourceUrl))
            .get()
            .build();
        http.newCall(req).enqueue(new Callback() {
            @Override public void onFailure(Call call, IOException e) {
                notifyError("启动流失败: " + e.getMessage());
            }
            @Override public void onResponse(Call call, Response response) throws IOException {
                if (!response.isSuccessful()) {
                    notifyError("启动流失败: HTTP " + response.code());
                    return;
                }
                try {
                    JSONObject json = new JSONObject(response.body().string());
                    streamId = json.optString("streamId");
                    String hlsUrl = json.optString("hlsUrl");
                    post(() -> playHls(hlsUrl));
                } catch (Exception e) {
                    notifyError("解析响应失败: " + e.getMessage());
                }
            }
        });
    }

    /**
     * 直接播放 HLS URL
     */
    public void playHls(@NonNull String hlsUrl) {
        DefaultHttpDataSource.Factory httpFactory = new DefaultHttpDataSource.Factory()
            .setUserAgent("StreamBridge/1.0")
            .setConnectTimeoutMs(8000)
            .setReadTimeoutMs(15000);
        DefaultDataSourceFactory dataSourceFactory = new DefaultDataSourceFactory(
            getContext(), httpFactory);
        MediaSource mediaSource = new HlsMediaSource.Factory(dataSourceFactory)
            .createMediaSource(MediaItem.fromUri(hlsUrl));
        player.setMediaSource(mediaSource);
        player.prepare();
        player.setPlayWhenReady(true);
        // 注意:实际渲染需绑定 PlayerView(com.google.android.exoplayer2.ui.PlayerView)
        // 建议在 XML 中使用 PlayerView 替代本视图,或在此处动态添加 PlayerView
        if (listener != null) listener.onConnected(hlsUrl);
    }

    /**
     * 停止播放并释放资源
     */
    public void stop() {
        if (player != null) {
            player.stop();
            player.clearMediaItems();
        }
        if (gateway != null && streamId != null) {
            Request req = new Request.Builder()
                .url(gateway + "/api/stop?id=" + streamId)
                .post(okhttp3.RequestBody.create(null, new byte[0]))
                .build();
            http.newCall(req).enqueue(new Callback() {
                @Override public void onFailure(Call call, IOException e) {}
                @Override public void onResponse(Call call, Response response) {}
            });
        }
    }

    public void release() {
        if (player != null) {
            player.release();
            player = null;
        }
    }

    private void notifyError(String msg) {
        post(() -> { if (listener != null) listener.onError(msg); });
    }

    public ExoPlayer getPlayer() { return player; }
}
