// Package server 内嵌 HTML 页面模板
package server

// playerPageTemplate 嵌入式播放器页面(/play?url=... 或 /play?demo=1)
// 两个 %s 分别对应: source URL, demo 标记
const playerPageTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>StreamBridge Player</title>
<link rel="stylesheet" href="/player.css">
<style>body{margin:0;background:#000} .player-wrap{aspect-ratio:16/9;width:100%%}</style>
</head>
<body>
<div class="player-wrap">
  <video id="video" autoplay muted playsinline style="width:100%%;height:100%%;object-fit:contain"></video>
  <div id="overlay" class="overlay"><div class="overlay-content"><div class="spinner"></div><p id="overlay-text">等待播放...</p></div></div>
</div>
<script src="/flv-demuxer.js?v=2"></script>
<script src="/streambridge.js?v=2"></script>
<script>
var source = "%s";
var demo = "%s";
var url = demo === "demo" ? "demo" : source;
StreamBridge.play({
  container: '#video',
  source: url,
  mode: 'auto',
  autoplay: true,
  muted: true,
  onConnected: function() { document.getElementById('overlay').style.display='none'; },
  onError: function(e) { document.getElementById('overlay-text').textContent = '错误: ' + e; }
});
</script>
</body>
</html>`

// doctorPageTemplate 诊断页面
const doctorPageTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>StreamBridge 诊断</title>
<link rel="stylesheet" href="/player.css">
</head>
<body>
<div class="app">
  <header class="topbar">
    <div class="brand">
      <span class="logo">🔧</span>
      <div><h1>StreamBridge 诊断</h1><p class="tagline">系统健康检查</p></div>
    </div>
    <nav class="nav"><a href="/">播放器</a><a href="/streams.html">流列表</a><a href="/stats.html">统计</a><a href="/doctor" class="active">诊断</a></nav>
  </header>
  <main class="main" style="grid-template-columns:1fr">
    <div class="info-card">
      <h3>📡 系统状态</h3>
      <pre id="health">加载中...</pre>
    </div>
    <div class="info-card">
      <h3>📊 活跃流</h3>
      <pre id="streams">加载中...</pre>
    </div>
    <div class="info-card">
      <h3>🌐 浏览器能力检测</h3>
      <pre id="browser">检测中...</pre>
    </div>
  </main>
</div>
<script>
fetch('/api/health').then(r=>r.json()).then(d=>{
  document.getElementById('health').textContent=JSON.stringify(d,null,2);
}).catch(e=>{document.getElementById('health').textContent='错误: '+e;});
fetch('/api/streams').then(r=>r.json()).then(d=>{
  document.getElementById('streams').textContent=JSON.stringify(d,null,2);
}).catch(e=>{document.getElementById('streams').textContent='错误: '+e;});
var caps=[];
caps.push('MSE: '+(typeof MediaSource!=='undefined'?'✓':'✗'));
caps.push('WebSocket: '+(typeof WebSocket!=='undefined'?'✓':'✗'));
caps.push('WebRTC: '+(typeof RTCPeerConnection!=='undefined'?'✓':'✗'));
caps.push('HLS native: '+(document.createElement('video').canPlayType('application/vnd.apple.mpegurl')?'✓':'✗'));
if(typeof MediaSource!=='undefined'&&MediaSource.isTypeSupported){
  caps.push('H264: '+(MediaSource.isTypeSupported('video/mp4;codecs="avc1.42E01E"')?'✓':'✗'));
  caps.push('AAC: '+(MediaSource.isTypeSupported('audio/mp4;codecs="mp4a.40.2"')?'✓':'✗'));
}
document.getElementById('browser').textContent=caps.join('\\n');
</script>
</body>
</html>`
