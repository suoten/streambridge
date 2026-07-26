# 演示截图 / Demo Screenshots

> 此目录存放 StreamBridge 的演示截图与 GIF,用于 README 展示。
> This directory holds demo screenshots/GIFs for the README.

## 截图清单 / Screenshot List

| 文件 / File | 内容 / Content | 推荐尺寸 / Recommended Size |
|------------|---------------|---------------------------|
| `player.png` | 播放器主界面 / Player main UI | 1280×720 |
| `streams.png` | 活跃流列表 / Active streams list | 1280×720 |
| `demo.gif` | 60 秒上手演示 / 60s quick start demo | 1280×720, < 5MB |
| `architecture.png` | 架构图 / Architecture diagram | 1920×1080 |

## 拍摄指南 / Shooting Guide

1. 启动 StreamBridge:`./streambridge`
2. 浏览器打开 `http://localhost:8080`
3. 用浏览器开发者工具的「截图整页」功能(Chrome DevTools → Ctrl+Shift+P → "Capture full size screenshot")
4. 用 [ScreenToGif](https://www.screentogif.com/) 录制 60 秒演示 GIF
5. 命名后放入本目录

## 在 README 中引用 / Reference in README

截图放入本目录后,在 README.md / README.en.md 顶部把这一行:

```markdown
> 截图待补充,首次启动后访问 `http://localhost:8080` 即可看到实际界面
```

替换为:

```markdown
![播放器界面](docs/screenshots/player.png)
![流列表](docs/screenshots/streams.png)
```
