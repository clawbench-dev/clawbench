# 语音合成（TTS）

语音合成让用户不用盯着屏幕也能"听"AI 的回复——在移动场景下尤其有用，比如走路时听代码审查结果。系统支持云引擎（Edge TTS、OpenAI 兼容 TTS）和本地引擎（Piper、Kokoro、MOSS-Nano），通过统一的 `SpeechProvider` 接口切换，TTS 前由 summarizer 将长文本压缩为适合朗读的短文本。

## 流程图

### TTS 请求到播放

```mermaid
sequenceDiagram
    participant 前端
    participant handler
    participant service
    participant TTS引擎

    前端->>handler: POST /api/tts/generate
    handler->>service: 注册 TTS Job
    service->>service: 摘要+文本清理
    service->>service: 检查缓存（hash 匹配）
    alt 缓存命中
        service-->>handler: 返回缓存文件路径
    else 缓存未命中
        service->>TTS引擎: Synthesize(text, outputPath)
        TTS引擎-->>service: 音频文件
        service->>service: 写入缓存
        service-->>handler: 返回文件路径
    end
    handler-->>前端: SSE 通知就绪
    前端->>handler: GET /api/tts/stream/{path}
    handler-->>前端: 音频流
```

## 功能与设计要点

### 功能清单

- **多引擎 TTS**：支持 Edge TTS（云，微软语音）、OpenAI 兼容 TTS（云，`/v1/audio/speech`）、Piper（本地 ONNX）、Kokoro（本地 ONNX）、MOSS-Nano（本地），用户按需选择引擎。云引擎声音自然但需要网络，本地引擎离线可用但声音质量有限
- **总结后端三选一**：长文本先经 summarizer 压缩再合成语音。后端可选 `simple`（纯文本清洗，零延迟零成本）、`api`（LLM 二次压缩，复用 `ai_summary` 配置）或 `auto`（**按内容自动分流**：剥离出的"杂质"占比 ≥ `summarize.auto_junk_ratio`（默认 0.5），或清洗后文本超过 simple 的截断上限时走 LLM，否则直接朗读清洗文本）。短文本（< 300 字符）跳过摘要直接合成。`auto` 在未配置 `ai_summary` 时降级为 `simple`，不像 `api` 那样硬失败
- **文本清理**：合成前自动剥离 Markdown 格式、工具调用输出、thinking block 等不适合朗读的内容，保留可读的纯文本。清理**刻意保守**——保留 `snake_case` 标识符、乘号、时间、端口、Windows 路径、`C#` 等会因过度清洗而被静默篡改的技术文本；公式、代码围栏、链接等才真正删除。`StripMarkdownStats` 回报删除/保留 rune 数，正是 `auto` 分流的判据来源
- **群聊轮流朗读**：群聊里每位成员**各自发言结束**即触发朗读，而不是等整轮结束只念最后一条。前端按发言者逐一触发并带上发言人名，播放走 FIFO 队列，多位成员并发发言时**按顺序轮流播放**、互不抢占；某位失败即推进下一位，不阻塞整轮。后端 `POST /api/tts/generate` 接收 `speakerName`，在摘要之后、合成之前拼上「`xxx`说：」前缀——**只作用于音频**，不进摘要、不写摘要缓存；缓存键并入发言人名，避免不同成员说同样的话共用音频
- **文件缓存**：合成结果按「文本 + 发言人 + 合成配置」hash 缓存为音频文件，相同内容不重复合成。缓存上限 100 个文件，自动淘汰最旧的——避免存储空间无限增长
- **TTS Job 管理**：每个 TTS 请求注册为 Job，支持取消和完成通知。防止重复合成相同内容

### 设计要点

- **CLISpeechProvider 是通用包装器**：所有 CLI 类 TTS 引擎共享统一的 CLI 调用模式（构建参数 → 管道文本 → 读取输出文件），差异仅在 CLI 名称和参数格式——新增引擎只需配置命令行参数。OpenAI 兼容引擎是纯 HTTP 客户端，不走这条路径
- **OpenAI 兼容引擎双形态流式**：`/v1/audio/speech` 的 `stream_format` 有两种取值。`sse` 每块音频是一个 `speech.audio.delta` 事件（base64），由 `speech.audio.done` 收尾，**只有新模型（`gpt-4o-mini-tts` 系）支持**；`audio` 是原始音频字节流（HTTP chunked），所有模型都支持。因此 provider **先请求 `sse`，收到 400 才回退 `audio`**——这样新模型拿到逐块事件、老模型（`tts-1` / `tts-1-hd`）仍然能流式播放而不会硬失败。判断实际返回的是哪种形态**看 `Content-Type` 而不是请求参数**：兼容服务器可能直接忽略 `stream_format` 并回原始音频，按请求参数判断会解析错。两条路径共用同一个 chunk 通道，因此前端 MSE 播放路径无需改动
- **模型名与音色由用户填写，不设默认**：兼容服务器接受哪些模型/音色不受 OpenAI 文档列表约束（自定义音色以 `{"id":"voice_xxx"}` 引用），猜一个默认值只会在请求时得到一条难懂的上游错误。空值时 provider 在 `validate()` 阶段就报错并指名缺失的配置路径
- **摘要与 TTS 共享 summarizer 接口**：TTS 摘要和任务摘要使用同一个 `Summarizer` 接口，但配置不同——TTS 摘要剥离 Markdown（朗读不需要格式），任务摘要保留 Markdown（记录需要格式）
- **`auto` 是"内容感知"而非"配置感知"**：`simple` 与 `api` 的取舍是固定的（要么总是清洗、要么总是调 LLM），用户得为整份会话选一种。`auto` 把决策下移到单条消息——大多数回复是干净散文，清洗即可朗读，不该为它付 LLM 延迟与费用；只有代码/表格/公式占比高、清洗后成了断句碎片时，才值得让 LLM 重新组织。它用 `StripMarkdownStats` 的删除比例作为判据，复用清理阶段已有的产物，不额外解析文本
- **清理的"保守"是被真实缺陷逼出来的**：早期版本无差别删除 `\ # * ~ \`` 等残留字符，把 `user_id`、`C:\Users\...`、`12:30:45`、`host:8080` 等技术文本改得面目全非——朗读出来是错的，却没有任何报错。现在按"是否真有 Markdown 语义"逐类判定，宁可少剥也不篡改
- **缓存键必须并入合成配置**：缓存键早期只哈希文本，于是改音色/模型后仍会命中旧音频——用户听到的是上一套配置的声音，且**与"设置没生效"无法区分**。现在实现 `CacheIdentityProvider` 的 provider 会把自己的配置（引擎 / 音色 / 模型 / 速率）折进键里，改配置即产生新缓存项。群聊的发言人名并入键是同一道理（"不同人说同样的话"不能共用音频）。**API key 绝不入键**——它不影响音频，而这个字符串会被哈希成磁盘文件名
- **Edge TTS 的 `Sec-MS-GEC` 是"会过期的鉴权算法"，不是固定常量**：微软用它拦截非 Edge 客户端，并在 2025-10 把 v1（纯 SHA-256 摘要）换成了 v2（`<CS64 8字节>-<自定义 SHA-256>`，配合 `Sec-MS-GEC-Version: 2-<ver>`）。旧 token 现在**在 WebSocket 握手阶段就被 403 拒绝**——不是超时、不是解析错误，而是一次快速失败。v2 的哈希是被刻意混淆的（非标准初始状态 + 改动过的消息扩展表 + CS64 混合），所以 `edge_tts_drm.go` 的常量必须与参考实现（Python `edge-tts` 的 `drm.py`）逐字节一致；`edge_tts_drm_test.go` 用固定时间戳向量钉住它。**当 Edge 合成开始 403 时，正确反应是照最新 `edge-tts` 重新移植算法，而不是排查网络或重试**
- **TTS 失败必须"可见"**：这套链路有三处会让失败退化成"永久转圈"，都已针对性修掉——① 任务 goroutine 提前返回（如摘要失败）若不关 `AudioCh`，WS 主协程会永久阻塞在 `range` 上；② 终态帧（done/error）的转发是异步的，而 `job.Done` 在任务返回时立即关闭，若据此关连接会**丢掉终态帧**，客户端收不到错误（WS 处理器改为等待转发协程写完再关闭）；③ 前端 `ws.onclose` 若只清理引用而不重置状态，连接在无终态帧时关闭会让 UI 永远停在"合成中"（现在会兜底报错并回到 idle，但**已开始播放音频时不干预**，那种收尾交给播放结束定时器）
