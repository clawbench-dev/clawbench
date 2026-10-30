# 屏幕共享 AI 听记：设计

日期：2026-10-30
前置：[2026-10-28 桌面屏幕与声音共享](2026-10-28-desktop-screen-audio-share-design.md)

## 目标

给已经能共享屏幕 / 声音的直播加「AI 听记」：

1. 共享过程中实时出字幕；
2. 停止后生成结构化会议纪要（概要 / 议题 / 结论 / 待办）；
3. 观众可以看（默认收起）；
4. **不同成员的音频信道分离，再按时间重排**——每行都带说话人。

## 关键约束（决定了整体形态）

**服务端拿不到音频。** 共享是纯 P2P：服务端只转发 SDP/ICE，媒体在浏览器之间直连。所以转写不可能在服务端「监听」房间，只能在发布端采集、再上传。

**发布端是唯一还有信道分离的地方。** 发布端持有系统声音、自己的麦克风、每个观众的麦克风（`pc.ontrack`）。把这几路混成一路再送 STT 就永久丢失了说话人；分开送则说话人标签是免费的，不需要 diarization 模型。

> 结论：**发布端分信道录音 → 逐段上传 → 服务端转写落库 → 读取时按时间重排**。

## 为什么不是别的方案

| 方案 | 否决理由 |
|---|---|
| 服务端从 WebRTC 取流转写 | 媒体是 P2P，服务端一个包都收不到；要做得先上 SFU，等于重写传输层 |
| 观众端转写 | 观众只有一路混合流（且每个人的混音内容还不一样），同样丢失信道；还要每个观众各跑一份 STT |
| 本地 whisper（Electron 主进程） | 新增模型分发与本地推理依赖；现有 STT provider 已经可用且是用户配置的 |
| 整段录完再转写 | 没有「实时字幕」；长会议还要一次性上传几十 MB |

## 分信道录音

```
system      ctx.createMediaStreamSource(displayStream)   → dest → MediaRecorder
mic         ctx.createMediaStreamSource(micStream)       → dest → MediaRecorder
viewer:<id> ctx.createMediaStreamSource(remoteStream)    → dest → MediaRecorder
```

每路一个 `MediaStreamAudioDestinationNode` + 一个 `MediaRecorder`，**互不混音**。

**在停顿处切，而不是按固定时钟切。** 固定 10s 切会让每句话都有概率在词中间断开；Whisper 在切口处会吞掉半个词或猜一个，一小时会议有几百个切口，质量下降是累积的。

做法是每 100ms 用 `AnalyserNode` 取一次 RMS：本段已满 **4 秒**且静音持续 **450ms** 就收尾；连续说话（一直不停顿）由 **20 秒**硬上限兜底，20s 仍在 Whisper 原生窗口内。分析器经一个 `gain=0` 的节点接到 destination，保证它被驱动但不会出声。

每次收尾都重启一个 recorder 而不是用 `timeslice`：WebM 只有第一片带容器头，后续切片单独不可解码，无法单独送 STT。重启的代价是边界处几毫秒的空隙，对会议记录无影响。

整段静音的段直接丢弃（不含内容，送上去只换回空字符串）；但**没测到过电平的短尾段要保留**——「不知道有没有声音」不能当成「没有声音」。

**串行上传，但绝不丢段**：每信道同一时刻只有一个上传在飞。**字幕可以滞后，转写不能缺口**——丢一段就是会议里永久少一句话，而「实时」只影响观感。所以队列允许随积压增长，失败按 1s/3s/9s 退避重试；只有 STT 后端连续不可达约 30 分钟（180 段/信道的硬内存闸门）才会丢，并在界面上标「本次转写缺少 N 段」。

**结束顺序**：先 `stop()` 让 `MediaRecorder` 在 `onstop` 里产出最后一段，**等它产出后**再关 `AudioContext`。反过来会静默丢掉最后几秒。之后等上传队列排空（最长 3 分钟）再生成纪要；排不空就标「可能缺少结尾」，而不是假装完整。

## 重排与去重

各信道独立转写，完成顺序 ≠ 说话顺序。所有 `startedAt` 由发布端**同一个时钟**打，因此可直接比较：

1. 按 `startedAt` 排序，同刻用 `id` 决胜（确定性）；
2. 丢弃空文本（静音段不落库）；
3. **回声去重**：观众的声音经发布端扬声器播放后会被系统回环再次采集，同一句话在 `system` 与 `viewer:*` 各出现一次。若两条不同信道在 ±5s 内**归一化后文本完全相同**（去大小写/空白/标点），只保留较早的一条。真正的同时说话用词不同，不受影响；
4. 相邻同信道、间隔 ≤4s 的段合并成一行（仅显示层；纪要用的是逐行带时间戳的全文）。

## 服务端

### 表

- `live_notes_segments`：`(token, project_id, channel, speaker, started_at, duration_ms, text)`，索引 `(token, started_at)`；
- `live_notes`：`(token PRIMARY KEY, project_id, title, status, minutes, error, created_at, updated_at)`。

`token` 就是分享 token（房间唯一标识），**没有外键**：房间在内存里，发布端一断就没了，而转写要活得更久。

### 接口

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/live-share/notes/segment` | 登录 + 项目 | multipart 音频 → STT → 落库，返回文本 |
| GET | `/api/live-share/notes?token=&since=` | 登录 + 项目 | 增量读时间线 + 纪要状态 |
| POST | `/api/live-share/notes/minutes` | 登录 + 项目 | 异步生成纪要（幂等，`regenerate` 才重跑） |
| GET | `/api/share/{token}/notes?since=` | **公开** | 观众读时间线；房间不存在即 404 |

### token 归属的两种来源

发布端端点接受两种归属证明：

1. 房间还在且 `room.ProjectKey() == 请求项目`（正常情况）；
2. **已存在本项目的听记行**（分享刚结束的瞬间）。

第二条不是补丁而是必需：`stop()` 会立刻删房，而最后一段音频与纪要请求正在路上。没有它，每次结束共享都会丢掉尾巴。

### 并发与限额

- 全局 STT 并发闸门 4（跨所有房间）——STT 是共享的 HTTP 服务，一场 8 人会议不该把它打满；
- 上传体 ≤4MB、单段时长 ≤120s、单行文本 ≤2000 字、说话人标签 ≤40 字；
- 转写失败返回 502 且**不落库**：该段留空，共享本身不受影响，上传队列继续。

### 纪要

- `summarize.NewAISummarizer(model.ConfigInstance.AISummary)`，用 `AskAboutContext` 传自定义 system prompt（会议记录员），`PreserveMarkdown` 由 prompt 侧保证（不经过 TTS 管线，不会被剥格式）；
- 转写 >6000 字时 **map-reduce**：分段提取要点 → 合并成纪要。整段塞进去要么超窗，要么被 provider 截断；
- 异步执行（后台 goroutine + 5 分钟超时），`live_notes.status` 走 `idle → generating → ready|failed`，前端轮询；
- 生成成功后写一份 Markdown 到 `<项目>/.clawbench/notes/<YYYY-MM-DD-HHmm>[-标题].md`（正文 + 完整转写附录）。数据库行有 7 天保留期，文件没有——它才是用户真正拥有的副本。文件名由会议开始时间决定，重新生成覆盖同一文件；
- 幂等：`generating`/`ready` 时重复 POST 直接返回当前状态，双击不会跑两遍。

### 生命周期

- 转写与纪要保留 **7 天**，由现有房间回收器每小时顺带清理；
- 观众侧 `{token}/notes` 只在房间存活期间可读——共享结束后 token 立即失效，不留下可长期读取的会议记录。

## 前端

### 发布端

`useLiveNotes`（模块级单例，与 `useLiveShare` 同构）+ `liveNotes.ts`（纯录音/上传逻辑，无 Vue，可单测）。

- `useLiveShare.start({notes:true})` 在建好 `AudioContext` 后启动采集，`pc.ontrack` 里 `addViewer`，`viewer-leave` 里 `removeViewer`；
- 字幕直接来自上传响应，**不轮询**；
- 停止后对话框**不关闭**，切到纪要视图（生成中 → Markdown + 复制/下载）。这是唯一展示纪要的地方，关掉就等于丢掉这场会议。

### 观众端

- `joined` / `meta` 带 `notesEnabled`，没开听记就不显示入口；
- 面板默认收起，**展开才轮询**（3s），404 即停止；
- 共享结束（`publisher-gone`）时关闭面板并停轮询。

## 测试

- `internal/service/live_notes_test.go`：跨信道重排、回声去重（丢弃/保留同时说话/保留远处的重复）、增量游标按 id、纪要 upsert、保留期清理、分块边界；
- `internal/handler/live_notes_test.go`：项目鉴权（缺项目 403、他人项目 404）、信道校验、静音不落库、公开读需房间存活、纪要幂等与空转写；
- `web/src/composables/__tests__/liveNotes.test.ts`：定时出段、停止时冲刷最后一段、积压丢弃、观众离开后仍上传、合并与分组；
- `web/src/share/__tests__/liveNotesClient.test.ts`：增量轮询、404 停止；
- `web/src/share/__tests__/LiveShareView.dock.test.ts`：听记入口只在开启时出现。

## 已知限制

- 系统声音只有 Windows 能采（Electron 限制），macOS/Linux 上只有麦克风信道；
- 观众面板不显示纪要（共享结束后房间销毁，观众侧 token 立即失效）——纪要只给发布端；
- 转写质量取决于配置的 STT 模型与语言；`stt.language` 为空时交给模型自判；
- 听记不区分同一信道内的多个说话人（例如系统声音里对方会议的多个人）——只有物理上分离的音源才能区分。
