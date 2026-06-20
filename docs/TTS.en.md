[中文](TTS.md) | [English](TTS.en.md)

# TTS Speech Synthesis Deployment Guide

ClawBench supports TTS speech synthesis, automatically summarizing and reading aloud AI responses. You need to configure a **TTS engine** and a **summarization backend**.

## TTS Engines

| Engine | Description | Network Requirement |
|--------|-------------|-------------------|
| `edge` | Microsoft Edge TTS, free and unlimited (default) | Requires network |
| `minimax` | Cloud synthesis, best audio quality | Requires mmx CLI + API quota |
| `piper` | Local offline, extremely fast (poor Chinese recognition, recommended for English) | No network required |
| `kokoro` | Local offline, high-quality Chinese | No network required |
| `moss-nano` | Local offline, multilingual, 48kHz voice cloning | First-time model download, then no network |

## Summarization Backend

Long texts are automatically summarized before reading aloud, controlled by `summarize.backend` (shared by TTS voice and scheduled task summarization):

| Backend | Description | Network Requirement |
|---------|-------------|-------------------|
| `simple` | Plain text cleaning (default), zero latency | None |
| `mmx-cli` | mmx text chat (lightweight and fast) | Requires mmx CLI |
| `claude` | Claude CLI (high summarization quality) | Requires claude CLI |
| `codebuddy` | CodeBuddy CLI | Requires codebuddy CLI |
| `opencode` | OpenCode CLI | Requires opencode CLI |
| `codex` | Codex CLI | Requires codex CLI |
| `qoder` | Qoder CLI (Alibaba coding agent) | Requires qoder CLI |
| `vecli` | VeCLI (Volcengine Doubao) | Requires vecli CLI |
| `deepseek` | CodeWhale (requires v0.8.33+) | Requires deepseek CLI |
| `pi` | Pi (minimalist coding agent) | Requires pi CLI |
| `api` | Remote AI API (OpenAI/Anthropic format) | Requires URL and API Key configuration |
| `ollama` | ~~Deprecated, use `api` + `format: "openai"` instead~~ | — |

## Text Processing Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `inline_code_max_len` | 100 | Maximum character count (rune) for preserving inline code; exceeded means entire segment is removed |
| `max_summarize_runes` | 10000 | Maximum character count for summarization input; exceeded means trailing portion is kept (simple mode: 1000) |

---

## MiniMax (Cloud, Best Audio Quality)

Cloud speech synthesis with the best audio quality, supporting multiple voices and languages.

```yaml
tts:
  engine: "minimax"
  voice: "female-chengshu"        # Voice: female-chengshu, male-qn-qingse, etc. (default: female-chengshu)
  tts_model: "speech-2.8-hd"     # Synthesis model (default: speech-2.8-hd)
  language: "zh"                  # Language boost: zh, en, ja, etc. (default: zh)
  speed: 1.5                      # Speech rate multiplier, recommended 1.0-2.0 (default: 1.5)
  format: "mp3"                   # Output format: mp3, wav, pcm, etc. (default: mp3)

summarize:
  backend: "mmx-cli"
  model: "MiniMax-M2.7"
```

**Prerequisite**: Install [mmx CLI](https://github.com/MiniMax-AI/MiniMax-M1) and configure the API Key.

---

## Edge TTS (Free, Requires Network)

Microsoft Edge TTS engine, free and unlimited, supporting hundreds of voices.

```yaml
tts:
  engine: "edge"
  voice: "zh-CN-XiaoxiaoNeural"   # Voice ID
  speed: 1                         # Speech rate multiplier (automatically converted to percentage rate, e.g., 1.2 → +20%)
```

**Common Chinese voices**: `zh-CN-XiaoxiaoNeural`, `zh-CN-YunxiNeural`, `zh-CN-YunjianNeural`
**Common English voices**: `en-US-JennyNeural`, `en-US-GuyNeural`
**Full list**: https://speech.microsoft.com/portal/voicegallery

No additional installation needed, works out of the box.

---

## Piper (Local Offline, Extremely Fast)

Local offline speech synthesis, extremely fast, suitable for low-latency scenarios. Poor Chinese recognition; recommended for English only.

```yaml
tts:
  engine: "piper"
  voice: "zh_CN-huayan-medium"    # Model name (without .onnx extension)
  speed: 1                         # Speech rate multiplier (automatically converted to length_scale = 1/speed)
  piper:
    model_path: ""                 # .onnx model path; leave empty to use .clawbench/piper-models/<voice>.onnx
    noise_scale: 0.667             # Sampling noise ratio; higher means more variation (default: 0.667, range: 0.0-1.0)
    length_scale: 1.0              # Explicit speech rate adjustment; overrides speed when set (lower=faster, 1.0=normal)
    sentence_silence: 0.2          # Inter-sentence pause duration in seconds (default: 0.2)
```

**Available Chinese models**: `zh_CN-huayan-medium`, `zh_CN-huayan-x_low`, `zh_CN-chaowen-medium`
**Available English models**: `en_US-lessac-medium`, `en_US-libritts-high`, etc.
**Full list**: https://github.com/rhasspy/piper/blob/master/VOICES.md

### Installation Steps

1. **Download binary**: https://github.com/rhasspy/piper/releases (extract to `.venv/piper/`)
2. **Download models**: https://huggingface.co/rhasspy/piper-voices (place in `.clawbench/piper-models/`)
   - Each model requires two files: `<model>.onnx` and `<model>.onnx.json`

---

## Kokoro (Local Offline, High-Quality Chinese)

Uses kokoro-onnx for local offline speech synthesis. No GPU required; excellent Chinese results.

```yaml
tts:
  engine: "kokoro"
  voice: "zf_001"                 # Voice name
  speed: 1                         # Speech rate multiplier (1.0=normal, 1.5=1.5x speed)
  kokoro:
    model_path: ""                 # Model file path; leave empty to use .clawbench/kokoro-models/kokoro-v1.1-zh.onnx
    voices_path: ""                # Voices file path; leave empty to use .clawbench/kokoro-models/voices-v1.1-zh.bin
    lang: "cmn"                    # espeak language code (cmn=Mandarin, en-us=American English, default: cmn)
```

**v1.1 Chinese female voices**: `zf_001`, `zf_002`, `zf_003`, ...
**v1.1 Chinese male voices**: `zm_009`, `zm_010`, `zm_011`, ...
**v1.0 Chinese female voices**: `zf_xiaobei`, `zf_shanshan`, `zf_xiaoyi`
**v1.0 Chinese male voices**: `zm_yunxi`, `zm_yunjian`
**Full list**: https://huggingface.co/onnx-community/Kokoro-82M-v1.1-zh-ONNX

### Installation Steps

```bash
# 1. Install Python dependencies (in .venv)
pip install kokoro-onnx "misaki[zh]" soundfile

# 2. Create model directory
mkdir -p .clawbench/kokoro-models

# 3. Download v1.1-zh Chinese-optimized model (~328MB)
curl -L -o .clawbench/kokoro-models/kokoro-v1.1-zh.onnx \
  https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.1/kokoro-v1.1-zh.onnx

# 4. Download voice embeddings file (~51MB)
curl -L -o .clawbench/kokoro-models/voices-v1.1-zh.bin \
  https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.1/voices-v1.1-zh.bin

# 5. Download vocabulary config (~3KB, required for misaki G2P)
curl -L -o .clawbench/kokoro-models/config.json \
  https://huggingface.co/hexgrad/Kokoro-82M-v1.1-zh/resolve/main/config.json
```

### Chinese Phonemization Notes

The Kokoro v1.1-zh model supports two phonemization paths:

- **misaki[zh] G2P** (recommended): Based on Jieba tokenization + PaddleSpeech, correctly handles polyphonic characters (e.g., "银行行长"), producing natural Chinese pronunciation. Automatically enabled when `.clawbench/kokoro-models/config.json` exists.
- **espeak-ng** (fallback): Rule-based phonemization, prone to mispronouncing polyphonic characters with a heavier accent. Automatically falls back when config.json is missing or misaki is not installed.

For the best Chinese results, ensure:
1. `config.json` has been downloaded to the model directory
2. `misaki[zh]` is installed in the Python virtual environment (`pip install misaki[zh]`)

---

## MOSS-TTS-Nano (Local Offline, Multilingual, Voice Cloning)

[MOSS-TTS-Nano](https://github.com/OpenMOSS/MOSS-TTS-Nano) is an open-source 0.1B-parameter lightweight speech generation model from MOSI.AI and the OpenMOSS team. It supports CPU inference (ONNX Runtime), 48kHz stereo output, approximately 20 languages, and zero-shot voice cloning.

**Features**:
- Only 0.1B parameters; ONNX CPU single-core can achieve real-time inference
- 48kHz stereo WAV output with natural sound
- Supports Chinese, English, Japanese, Korean, German, French, Spanish, and ~20 other languages
- Zero-shot voice cloning (provide reference audio to clone a voice)
- ONNX inference is approximately 2x faster than PyTorch

```yaml
tts:
  engine: "moss-nano"
  moss_nano:
    model_dir: ""          # ONNX model directory; leave empty for auto-detection or CLI auto-download
    prompt_speech: ""      # Reference audio path (for voice cloning); leave empty to use built-in voice
    voice: "Junhao"        # Built-in voice preset (ONNX backend)
    backend: "onnx"        # Inference backend: "onnx" (CPU) or "pytorch" (requires GPU)
```

> 💡 `prompt_speech` is used for zero-shot voice cloning: provide a reference audio clip (e.g., `.clawbench/moss-nano-models/ref_zh.wav`), and the model will clone its voice. Leave empty to use the built-in voice preset specified by `voice`.

### Installation Steps

```bash
# 1. Create Python environment
conda create -n moss-tts-nano python=3.12 -y
conda activate moss-tts-nano

# 2. Clone and install
git clone https://github.com/OpenMOSS/MOSS-TTS-Nano.git
cd MOSS-TTS-Nano
pip install -r requirements.txt
pip install -e .

# 3. First run will automatically download ONNX models from Hugging Face (~700MB)
# 4. Ensure moss-tts-nano command is in $PATH, or located in .venv/bin/
```

---

## API Summarization Backend (Remote AI API, OpenAI/Anthropic Format)

Use HTTP API to call remote AI services for text summarization. Supports both OpenAI Chat Completions and Anthropic Messages formats. Compatible with OpenAI, DeepSeek, Groq, OpenRouter, Ollama (OpenAI-compatible endpoint), and any compatible API.

### OpenAI Format (compatible with DeepSeek, Groq, Ollama, etc.)

```yaml
tts:
  engine: "edge"                    # Any TTS engine
  voice: "zh-CN-XiaoxiaoNeural"
  speed: 1

summarize:
  backend: "api"
  model: "gpt-4o-mini"
  api:
    base_url: "https://api.openai.com/v1/chat/completions"  # Full endpoint URL
    key: "sk-xxx"                                             # API Key
    format: "openai"                                          # API format (default: openai)
```

### Anthropic Format

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

### Ollama Local Inference (via API Backend)

> 💡 The old `ollama` summarization backend is deprecated. Please migrate to `api` backend + `format: "openai"`.

Ollama provides an OpenAI-compatible endpoint `/v1/chat/completions`, no cloud API needed, suitable for offline or privacy-sensitive scenarios.

```yaml
tts:
  engine: "edge"
  voice: "zh-CN-XiaoxiaoNeural"
  speed: 1

summarize:
  backend: "api"
  model: "gemma3:270m"
  api:
    base_url: "http://localhost:11434/v1/chat/completions"  # Ollama OpenAI-compatible endpoint
    format: "openai"
```

> 💡 The `gemma3:270m` model is only 291MB, suitable for fast summarization. For better quality, switch to `qwen3:0.6b` or a larger model — just change the `model` field.

#### Installation Steps

```bash
# 1. Install Ollama: https://ollama.com/download
# 2. Pull model
ollama pull gemma3:270m
# 3. Start Ollama service
ollama serve
```
