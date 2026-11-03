# ai-upscaler — 本地 AI 图片高清修复

使用 [Real-ESRGAN](https://github.com/xinntao/Real-ESRGAN) 在本地把低分辨率/模糊图片放大并修复成高清图。计算在本机 GPU 上完成，**首次安装后完全离线，图片不上传任何服务器**。

- 语言：Go（仅用标准库，零第三方依赖）
- 引擎：Real-ESRGAN ncnn-vulkan，自动调用 Apple GPU（Metal）
- 支持格式：JPG / PNG / WebP
- 支持单张处理与整个目录批量处理

---

## 一分钟上手

```bash
# 1. 编译（需要 Go 1.23+）
cd ai-upscaler
go build -o ai-upscaler .

# 2. 直接使用（首次运行会自动下载约 50MB 引擎与模型）
./ai-upscaler -i 输入.jpg -o 输出.png
```

完成。默认使用通用照片模型放大 4 倍。引擎和模型安装在 `~/.ai-upscaler/`，之后无需联网。

---

## 常用命令

```bash
# 放大 2 倍 / 3 倍 / 4 倍
./ai-upscaler -i low.png -o high.png -s 2
./ai-upscaler -i low.png -o high.png -s 3
./ai-upscaler -i low.png -o high.png -s 4

# 动漫插画（专用模型，线条更干净）
./ai-upscaler -i anime.png -o out.png -n anime

# 动画视频帧 / 追求速度（快约 10 倍）
./ai-upscaler -i frame.png -o out.png -n animevideo

# 批量处理整个目录
./ai-upscaler -i ./photos -o ./photos_out -s 2
```

> 注意：批量处理时 `-i` 和 `-o` 都应是目录，输出文件名与输入相同（统一转为 PNG）。

---

## 参数说明

| 参数 | 说明 | 默认值 |
|---|---|---|
| `-i` | 输入图片或目录（**必填**） | — |
| `-o` | 输出图片或目录（**必填**） | — |
| `-s` | 放大倍数：`2` / `3` / `4` | `4` |
| `-n` | 模型：`general` / `anime` / `animevideo` | `general` |
| `-t` | 分块大小，内存/显存不足时调小（如 `128`、`64`）；`0`=自动 | `0` |
| `-f` | 输出格式：`jpg` / `png` / `webp` | 按输出扩展名 |
| `-g` | 指定 GPU 设备 id | 自动 |
| `-j` | 加载/处理/保存线程数，如 `1:2:2` | `1:2:2` |
| `-x` | TTA 模式，画质略高但慢数倍 | 关闭 |
| `-v` | 打印详细推理日志 | 关闭 |
| `-setup` | 强制重新下载安装引擎与模型 | — |

### 模型怎么选

| 模型 | 适用场景 | 特点 |
|---|---|---|
| `general` | 真实照片、通用图像 | 修复细节丰富，默认推荐 |
| `anime` | 动漫插画、二次元图 | 线条锐利，色彩干净 |
| `animevideo` | 动画截图、视频帧、批量任务 | 速度最快，原生支持 2/3/4 倍 |

---

## 实测性能（Apple M4）

| 输入尺寸 | 倍数 | 模型 | 耗时 |
|---|---|---|---|
| 220 × 220 | 4× | general | 4.2 s |
| 220 × 220 | 2× | general | 1.7 s |
| 220 × 220 | 4× | animevideo | **0.4 s** |
| 80 × 80 | 4× | general | 0.6 s |

> 大图（如 1080p→4K）耗时会显著增加，可用 `-n animevideo` 或 `-t` 调小分块提速。

---

## 常见问题

**首次下载很慢或失败？**
下载源是 GitHub，国内网络可能不稳定。可重试，或配置代理后再运行；也可手动下载对应平台的包（见下）后解压到 `~/.ai-upscaler/`。

**处理大图时报错或闪退？**
加 `-t 128`（仍失败再试 `-t 64`）降低单次处理的内存占用。

**想恢复初始状态 / 重新安装？**
```bash
rm -rf ~/.ai-upscaler        # 清除已安装的引擎和模型
./ai-upscaler -setup ...     # 或直接用 -setup 强制重装
```

**支持哪些系统？**
内置 macOS / Linux(Ubuntu) / Windows 的官方引擎下载地址。macOS 为通用二进制，同时支持 Apple Silicon 与 Intel。

---

## 工作原理

程序本身只负责参数解析、首次安装、调度和结果校验；真正的 AI 推理由官方 Real-ESRGAN ncnn-vulkan 可执行程序完成：

```
输入图片 → Go 程序组装参数 → Real-ESRGAN 引擎（本地 GPU 推理）→ 输出图片
```

模型权重来自 Real-ESRGAN 官方 v0.2.5.0 发行版，许可证见引擎包内 LICENSE。
