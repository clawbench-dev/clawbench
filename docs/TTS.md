[中文](TTS.md) | [English](TTS.en.md)

# TTS 语音合成部署指南

ClawBench 支持 TTS 语音合成，自动将 AI 回复总结后朗读。需要配置 **TTS 引擎** 和 **总结后端**。

## TTS 引擎

| 引擎 | 说明 | 网络要求 |
|------|------|---------|
| `edge` | 微软 Edge TTS，免费无限制（默认） | 需要网络 |
| `minimax` | 云端合成，音质最佳 | 需要 mmx CLI + API 配额 |
| `piper` | 本地离线，速度极快（中文识别较差，推荐英文环境） | 无需网络 |
| `kokoro` | 本地离线，高质量中文 | 无需网络 |
| `moss-nano` | 本地离线，多语言，48kHz 音色克隆 | 首次需下载模型，之后无需网络 |

## 总结后端

长文本朗读前会自动总结，由 `summarize.backend` 控制（TTS 语音和定时任务共用同一配置）：

| 后端 | 说明 | 网络要求 |
|------|------|---------|
| `simple` | 纯文本清洗（默认），零延迟 | 无 |
| `mmx-cli` | mmx text chat（轻量快速） | 需要 mmx CLI |
| `claude` | Claude CLI（总结质量高） | 需要 claude CLI |
| `codebuddy` | CodeBuddy CLI | 需要 codebuddy CLI |
| `opencode` | OpenCode CLI | 需要 opencode CLI |
| `codex` | Codex CLI | 需要 codex CLI |
| `qoder` | Qoder CLI（阿里编码智能体） | 需要 qoder CLI |
| `vecli` | VeCLI（火山引擎 Doubao） | 需要 vecli CLI |
| `deepseek` | CodeWhale（需 v0.8.33+） | 需要 deepseek CLI |
| `pi` | Pi（极简编程智能体） | 需要 pi CLI |
| `api` | 远程 AI API（OpenAI/Anthropic 格式） | 需配置 URL 和 API Key |
| `ollama` | ~~已废弃，请用 `api` + `format: "openai"`~~ | — |

## 文本处理参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `inline_code_max_len` | 100 | 行内代码保留的最大字符数（rune）；超出则整段删除 |
| `max_summarize_runes` | 10000 | 总结输入的最大字符数；超出则截取尾部（simple 模式: 1000） |

---

## MiniMax（云端，音质最佳）

云端语音合成，音质最好，支持多种音色和语言。

```yaml
tts:
  engine: "minimax"
  voice: "female-chengshu"        # 音色: female-chengshu, male-qn-qingse 等（默认: female-chengshu）
  tts_model: "speech-2.8-hd"     # 合成模型（默认: speech-2.8-hd）
  language: "zh"                  # 语言增强: zh, en, ja 等（默认: zh）
  speed: 1.5                      # 语速倍率，推荐 1.0-2.0（默认: 1.5）
  format: "mp3"                   # 输出格式: mp3, wav, pcm 等（默认: mp3）

summarize:
  backend: "mmx-cli"
  model: "MiniMax-M2.7"
```

**前置条件**：安装 [mmx CLI](https://github.com/MiniMax-AI/MiniMax-M1) 并配置 API Key。

---

## Edge TTS（免费，需网络）

微软 Edge TTS 引擎，免费无限制，支持数百种音色。

```yaml
tts:
  engine: "edge"
  voice: "zh-CN-XiaoxiaoNeural"   # 音色ID
  speed: 1                         # 语速倍率（自动转为百分比速率，如 1.2 → +20%）
```

**常用中文音色**：`zh-CN-XiaoxiaoNeural`、`zh-CN-YunxiNeural`、`zh-CN-YunjianNeural`
**常用英文音色**：`en-US-JennyNeural`、`en-US-GuyNeural`
**完整列表**：https://speech.microsoft.com/portal/voicegallery

无需额外安装，开箱即用。

---

## Piper（本地离线，极速）

本地离线语音合成，速度极快，适合低延迟场景。中文识别较差，仅推荐英文环境使用。

```yaml
tts:
  engine: "piper"
  voice: "zh_CN-huayan-medium"    # 模型名（不含.onnx扩展名）
  speed: 1                         # 语速倍率（自动转为 length_scale = 1/speed）
  piper:
    model_path: ""                 # .onnx 模型路径，留空则使用 .clawbench/piper-models/<voice>.onnx
    noise_scale: 0.667             # 采样噪声比例，越大变化越多（默认: 0.667，范围: 0.0-1.0）
    length_scale: 1.0              # 显式语速调节，设置后优先于 speed（越小越快，1.0=正常）
    sentence_silence: 0.2          # 句间停顿时长，单位秒（默认: 0.2）
```

**可用中文模型**：`zh_CN-huayan-medium`、`zh_CN-huayan-x_low`、`zh_CN-chaowen-medium`
**可用英文模型**：`en_US-lessac-medium`、`en_US-libritts-high` 等
**完整列表**：https://github.com/rhasspy/piper/blob/master/VOICES.md

### 安装步骤

1. **下载二进制**：https://github.com/rhasspy/piper/releases（解压到 `.venv/piper/`）
2. **下载模型**：https://hf-mirror.com/rhasspy/piper-voices（放入 `.clawbench/piper-models/`）
   - 每个模型需要两个文件：`<model>.onnx` 和 `<model>.onnx.json`

---

## Kokoro（本地离线，高质量中文）

使用 kokoro-onnx 进行本地离线语音合成，无需 GPU，中文效果优秀。

```yaml
tts:
  engine: "kokoro"
  voice: "zf_001"                 # 音色名
  speed: 1                         # 语速倍率（1.0=正常，1.5=1.5倍速）
  kokoro:
    model_path: ""                 # 模型文件路径，留空则使用 .clawbench/kokoro-models/kokoro-v1.1-zh.onnx
    voices_path: ""                # 音色文件路径，留空则使用 .clawbench/kokoro-models/voices-v1.1-zh.bin
    lang: "cmn"                    # espeak 语言代码（cmn=普通话，en-us=美式英语，默认: cmn）
```

**v1.1 中文女声**：`zf_001`, `zf_002`, `zf_003`, ...
**v1.1 中文男声**：`zm_009`, `zm_010`, `zm_011`, ...
**v1.0 中文女声**：`zf_xiaobei`, `zf_shanshan`, `zf_xiaoyi`
**v1.0 中文男声**：`zm_yunxi`, `zm_yunjian`
**完整列表**：https://huggingface.co/onnx-community/Kokoro-82M-v1.1-zh-ONNX

### 安装步骤

```bash
# 1. 安装 Python 依赖（在 .venv 中）
pip install kokoro-onnx "misaki[zh]" soundfile

# 2. 创建模型目录
mkdir -p .clawbench/kokoro-models

# 3. 下载 v1.1-zh 中文优化模型（~328MB）
curl -L -o .clawbench/kokoro-models/kokoro-v1.1-zh.onnx \
  https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.1/kokoro-v1.1-zh.onnx

# 4. 下载声纹文件（~51MB）
curl -L -o .clawbench/kokoro-models/voices-v1.1-zh.bin \
  https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.1/voices-v1.1-zh.bin

# 5. 下载词表配置（~3KB，misaki G2P 必需）
curl -L -o .clawbench/kokoro-models/config.json \
  https://huggingface.co/hexgrad/Kokoro-82M-v1.1-zh/resolve/main/config.json
```

### 中文音素化说明

Kokoro v1.1-zh 模型支持两种音素化路径：

- **misaki[zh] G2P**（推荐）：基于 Jieba 分词 + PaddleSpeech，能正确处理多音字（如"银行行长"），中文发音自然。当 `.clawbench/kokoro-models/config.json` 存在时自动启用。
- **espeak-ng**（回退）：基于规则的音素化，多音字容易读错，口音较重。当 config.json 不存在或 misaki 未安装时自动回退。

如需最佳中文效果，请确保：
1. 已下载 `config.json` 到模型目录
2. Python 虚拟环境中已安装 `misaki[zh]`（`pip install misaki[zh]`）

---

## MOSS-TTS-Nano（本地离线，多语言，音色克隆）

[MOSS-TTS-Nano](https://github.com/OpenMOSS/MOSS-TTS-Nano) 是 MOSI.AI 与 OpenMOSS 团队开源的 0.1B 参数轻量级语音生成模型。支持 CPU 推理（ONNX Runtime）、48kHz 立体声输出、约 20 种语言，以及零样本音色克隆。

**特性**：
- 仅 0.1B 参数，ONNX CPU 单核即可实时推理
- 48kHz 立体声 WAV 输出，听感自然
- 支持中文、英文、日语、韩语、德语、法语、西班牙语等 ~20 种语言
- 零样本音色克隆（提供参考音频即可复刻音色）
- ONNX 推理比 PyTorch 快约 2 倍

```yaml
tts:
  engine: "moss-nano"
  moss_nano:
    model_dir: ""          # ONNX 模型目录，留空则自动检测或 CLI 自动下载
    voice: "Junhao"        # 内置音色预设（ONNX 后端）
    backend: "onnx"        # 推理后端: "onnx" (CPU) 或 "pytorch" (需GPU)
```

### 安装步骤

```bash
# 1. 创建 Python 环境
conda create -n moss-tts-nano python=3.12 -y
conda activate moss-tts-nano

# 2. 克隆并安装
git clone https://github.com/OpenMOSS/MOSS-TTS-Nano.git
cd MOSS-TTS-Nano
pip install -r requirements.txt
pip install -e .

# 3. 首次运行会自动从 Hugging Face 下载 ONNX 模型（约 700MB）
#    国内用户会自动使用 hf-mirror.com 镜像
# 4. 确保 moss-tts-nano 命令在 $PATH 中，或位于 .venv/bin/
```

> **国内网络提示**：如果 `pip install -r requirements.txt` 中 `WeTextProcessing` 安装失败，可先 `conda install -c conda-forge pynini=2.1.6.post1`，再安装其余依赖。

---

## API 总结后端（远程 AI API，支持 OpenAI/Anthropic 格式）

通过 HTTP API 调用远程 AI 服务进行文本总结，支持 OpenAI Chat Completions 和 Anthropic Messages 两种格式。适用于 OpenAI、DeepSeek、Groq、OpenRouter、Ollama（OpenAI 兼容端点）等任何兼容 API。

### OpenAI 格式（兼容 DeepSeek、Groq、Ollama 等）

```yaml
tts:
  engine: "edge"                    # 任意 TTS 引擎
  voice: "zh-CN-XiaoxiaoNeural"
  speed: 1

summarize:
  backend: "api"
  model: "gpt-4o-mini"
  api:
    base_url: "https://api.openai.com/v1/chat/completions"  # 完整端点 URL
    key: "sk-xxx"                                             # API Key
    format: "openai"                                          # API 格式（默认: openai）
```

### Anthropic 格式

```yaml
tts:
  engine: "edge"
  voice: "zh-CN-XiaoxiaoNeural"
  speed: 1

summarize:
  backend: "api"
  model: "claude-3-5-haiku-latest"
  api:
    base_url: "https://api.anthropic.com/v1/messages"
    key: "sk-ant-xxx"
    format: "anthropic"
```

### Ollama 本地推理（通过 API 后端）

> 💡 原 `ollama` 总结后端已废弃，请迁移到 `api` 后端 + `format: "openai"`。

Ollama 提供了 OpenAI 兼容端点 `/v1/chat/completions`，无需云 API，适合离线或隐私敏感场景。

```yaml
tts:
  engine: "edge"
  voice: "zh-CN-XiaoxiaoNeural"
  speed: 1

summarize:
  backend: "api"
  model: "gemma3:270m"
  api:
    base_url: "http://localhost:11434/v1/chat/completions"  # Ollama 的 OpenAI 兼容端点
    format: "openai"
```

> 💡 `gemma3:270m` 模型仅 291MB，适合快速总结。如需更好质量，可换 `qwen3:0.6b` 或更大模型，只需修改 `model` 即可。

#### 安装步骤

```bash
# 1. 安装 Ollama：https://ollama.com/download
# 2. 拉取模型（推荐国内镜像）
OLLAMA_REGISTRY=https://ollama.ai-mirror.cn ollama pull gemma3:270m
# 3. 启动 Ollama 服务
ollama serve
```
