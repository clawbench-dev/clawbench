# 会议关键画面（自动截图）设计

日期：2026-11-03
状态：已实现（2026-11-03）
前置：[会议页签](2026-11-01-meeting-tab-design.md)、[屏幕共享 AI 听记](2026-10-30-live-share-ai-notes-design.md)、[桌面屏幕与声音共享](2026-10-28-desktop-screen-audio-share-design.md)

## 1. 背景与问题

会议记录现在有文字（纪要 / 转写 / 校对）、有声音（`audio/`）、有聊天，**唯独没有画面**。而屏幕会议里大量信息只存在于画面上：打开的那张架构图、切过去的那个页面、讲到一半停住的那一屏。会后看纪要只能读到「讨论了架构方案」，看不到当时到底长什么样。

听记流水线已经在记录**时间线事件**：

| 事件 | 表 | 触发者 |
|---|---|---|
| 谁在说、说了什么 | `live_notes_segments` | 发布端逐段上传 |
| 打开了哪个文件（起止） | `live_notes_files` | `meetingFileTracker` |
| 谁在共享屏幕（起止） | `live_notes_screen` | 发布端 |

这三条已经能把「几点几分看了什么」还原出来，但还原不了「看到的是什么」。本设计补上第四条：**画面**。

## 2. 目标与非目标

### 2.1 目标

1. 会议过程中**自动**留存关键画面，无需用户操作；
2. 画面与会话时间线对齐（谁在说、当时开着什么文件、谁在共享）；
3. 会后在会议记录的转写里逐条可见，可放大、可删除；
4. 数量可控：一场一小时的会留几十张，不是几百张近似帧；
5. 默认开启，但用户可以随时暂停、可以整场关掉。

### 2.2 非目标

- **不做录像**。截帧解决「回看关键一屏」，录像解决「逐秒回放」，两者存储量差两个数量级，需求也不同。要做录像另立设计。
- **不做视觉模型自动描述**（OCR / 图片理解）。v1 只保证「截到、存下、对得上时间」，描述留到有明确需求再做。
- **观众端不可见**。观众看的是直播，不需要回看；公开分享端点也不该暴露发布端项目里的持久化产物。
- **不新增详情页签**。详情已有 5 个页签（纪要/转写/校对/聊天/资源），第 6 个在手机上会挤成一排小字。画面按语义放进**转写的时间线**（它本来就是一条时间线），文件列表交给**资源**页签。
- **不写进 `minutes.md`**。见 §6.3。

## 3. 决策汇总

| 决策 | 选择 | 理由 |
|---|---|---|
| 在哪抓 | **发布端本地** | 服务端拿不到媒体（P2P，只转发 SDP/ICE），见 §4 |
| 抓哪一路 | **共享者优先**：`screenSharer ? remoteScreenStream : previewStream` | 与 `currentScreenSharer()`（`useLiveShare.ts:900`）同一判据：成员共享时 `displayStream` 拍的是主持人自己的屏 |
| 怎么触发 | 文件打开 / 切源 / 关键词 / 画面变化 / 手动 | 定时全量截图不可用，见 §5.1 |
| 去重 | dHash（汉明距离 ≤8 视为同屏）+ 最小间隔 + 配额 | 静态幻灯片一小时只留 1–2 张 |
| 存哪 | `<会议目录>/shots/*.webp` + `live_notes_shots` 表 | 与 audio / minutes 同一目录，随记录一起删 |
| 展示 | 转写时间线内联缩略图 + 资源页签 `shots/` 分组 | 不新增页签，语义正确 |
| 默认值 | 跟随独立开关「关键画面」，默认开 | 截图是持久化，必须能关；与听记解耦（有人只要文字） |
| 标注 | 按平台合成（win/mac 必须，Linux 已在画面里） | 标注画在独立窗口，是否进视频轨取决于平台，见 §5.4 |

## 4. 为什么不是别的方案

| 方案 | 否决理由 |
|---|---|
| 服务端抓帧 | 共享是纯 P2P，服务端一个媒体包都收不到。要做得先上 SFU，等于为截图重写传输层 |
| 观众端抓帧 | 观众没有项目登录态，写不进发布端的会议记录；而且观众看到的是转码后的下行画面，比源画面差 |
| 抓帧时再调一次 `getDisplayMedia` | 多一次系统授权弹窗；共享单个窗口时会**把整块屏幕拍进去**（拍到没共享的窗口）；切源后与在播画面不同步 |
| 用 `ImageCapture.grabFrame()` | display track 上的支持度各引擎不一致（Firefox 尤其），且拿不到「最近一帧已解码」的时机 |
| 固定每 N 秒截图 | 一小时的幻灯片会议产生几百张几乎相同的图，既没人看也占磁盘。触发必须带语义 |
| 把图塞进 `minutes.md` | 相对路径的基准在两个消费者里不一致，见 §6.3 |
| 新增「画面」页签 | 详情页已有 5 个页签，移动端放不下第 6 个 |

## 5. 抓帧

### 5.1 触发策略

按性价比排序。前两条是主力：**「主持人打开了什么、切到了哪」本身就是这场会的信息结构**，而这两条链路已经在记时间线了，截图只是给它们补上画面。

| 触发 | 依据（已有） | 拍摄时机 | 优先级 |
|---|---|---|---|
| 打开文件 / 文档 | `noteMeetingFileOpened`（`web/src/utils/meetingFileTracker.ts:135`） | 延迟 1200ms，等渲染完成 | 2 |
| 切源 / 成员开始共享 | `watch(currentScreenSharer)`（`useLiveShare.ts:942`）+ `switchSource`（`:1709`） | 等新轨首帧后再 +500ms | 3 |
| 语音关键句 | 转写分段的 `startedAt` + `durationMs` | 段落结束（通常已过去，立即拍） | 5 |
| 画面显著变化 | dHash | 立即 | 6 |
| 手动 | 按钮 | 0 | 1（最高） |

**语音触发天然滞后 2–5 秒**：转写要等 STT 往返才回到前端，等它命中关键词时，那句话往往已经说完几秒了。所以它抓的是「说完之后仍在看的那一屏」，不是那句话的精确帧。这是可接受的（人讲一件事时画面通常不变），但不要把它宣传成「说到哪拍到哪」。要精确对齐得维护 1fps 的环形帧缓冲，属于 v2。

关键词表刻意小、可解释（中英各一组）：`看这里 / 看这个 / 这张图 / 这个页面 / 问题是 / 结论 / 决定 / 下一步 / 重点 / 大家看 / 注意一下`，英文 `look here / as you can see / the problem is / the point is / next step / decision`。它是启发式，漏了不影响主链路（文件与切源仍在拍）。

**文件触发挂在记录点，不是上传点**。`meetingFileTracker` 的本地事件是即时的，但上传是 5 秒批量（`FLUSH_INTERVAL_MS`）；订阅必须挂在 `noteMeetingFileOpened`（`:135`），挂到 flush 上会让截图晚 0–5 秒，画面早就切走了。

**延迟 1200ms 而不是 700ms**：`closeCurrent`（`:163`）会把短于 1 秒的访问当成误点从时间线里删掉。截图在 700ms 拍下、文件在 900ms 被关掉，就会留下一张时间线上不存在的孤立画面。1.2 秒之后再拍，并检查该文件是否仍是当前打开项，两个问题一起解决。

**共享者的变化只需要两个接线点**，不是四个：

- `useLiveShare.ts:942` 已经有一个 `watch(currentScreenSharer, …)`，成员开始/停止共享、主持人开始/停止共享都会经过它 —— 截图触发挂在这里；
- **`switchSource` 必须单独调一次**：主持人从「屏幕 A」切到「屏幕 B」时 `currentScreenSharer()` 前后都是 `'host'`，watcher 不会触发，而画面确实换了。

### 5.2 去重与配额

- **dHash**：9×8 灰度差分，64 位；与**上一张保留的**截图汉明距离 ≤ 8 视为同屏，直接丢弃（不是与上一张拍到的比——连续丢弃才不会漂移）；
- **最小间隔**：非手动触发之间至少 8 秒；手动不受限；
- **配额**：每小时 40 张、每场 80 张；超额后只保留优先级 ≤3（文件/切源/手动）的；
- **服务端硬上限**：每场 100 张（防客户端 bug，不是产品策略）。

配额与去重都在**编码之前**判定：先抓一帧缩到 9×8 算哈希，只有决定保留才做全尺寸编码。这样一次「什么都没变」的检查只有 1–2ms 开销。

### 5.3 抓帧实现

```
getSource() = screenSharer ? remoteScreenStream : previewStream   // 与 currentScreenSharer 同判据
      │
      ▼
隐藏 <video muted playsinline>  ──readyState / 首帧──▶ 「有新帧」信号
      │
      ├── 9×8 灰度 ──▶ dHash ──▶ 去重/配额判定 ──▶ 丢弃
      │
      └── 源尺寸 canvas（不再缩放）──▶ 合成墨迹 ──▶ WebP q0.82 ──▶ 上传队列（单飞）
```

要点：

- **截图不可能比推流更清晰**：`screenCaptureConstraints()`（`liveMediaPolicy.ts:106`）已经把采集轨压到 1920×1200，安卓更狠（`ANDROID_CAPTURE_MAX_WIDTH=1280`、JPEG q55）。所以不要再加一层「长边 1920」的缩放——照源尺寸画即可，多一次缩放只是多一次模糊。
- **隐藏 video 用 1px + `opacity:0` 挂在 body 上，不用 `display:none`**。有的引擎不为 `display:none` 的 video 解码帧；`videoWidth/videoHeight` 反映的是轨道的固有尺寸，与 CSS 尺寸无关。
- **抓帧器挂在模块单例上，不挂在组件里**：面板切到别的页签、组件卸载都不该让截图停下（`useLiveShare` / `useLiveNotes` 都是这个形态）。video 元素在第一次真正要抓时才创建。
- **`requestVideoFrameCallback` 只是锦上添花，不能当依赖**：Firefox 到 132 才有。判「有没有帧」用 `readyState >= HAVE_CURRENT_DATA && videoWidth > 0`；切源时等 `videoWidth/Height` 变成新轨的尺寸（或 rVFC 可用时等一次回调），2 秒超时放弃本次。
- **换源要重设 `srcObject` 并重新 `play()`**：`srcObject` 变化后元素会回到 `HAVE_NOTHING`，不重设会一直画上一路的最后一帧。
- **源在抓的瞬间读取**（`getSource()`），不做缓存。切源、成员共享、成员退出都由下一次触发自然跟上。
- **成员共享优先于主持人自己的屏**。两边可能同时在播（主持人的画面仍发给观众，成员那一路走第三条 m-line），但「正在被讲解的是哪一路」只有一个答案：`currentScreenSharer()`（`useLiveShare.ts:900`）已经实现了这条优先级，`live_notes_screen` 的时间线一次也只允许一个共享者。截图跟它走，不跟「主画面」走。
- **编码串行、单飞**：同一时刻只允许一张在编码或上传。1080p WebP 编码约 30–80ms，与 WebRTC 发送抢主线程；手动触发之外的编码放到 `requestIdleCallback`（带 1000ms 超时）。
- **图片格式**：优先 `image/webp`，`blob.type` 不是 webp 时（Firefox / Safari 的 `toBlob` 会回 PNG）按 PNG 上传。**服务端不信 `blob.type`，按魔数嗅探**。

### 5.4 标注：按平台决定是否合成

发布端的墨迹**不在主窗口里**：它画在一个独立窗口的 `LiveAnnotateOverlay`（`web/src/components/live/MeetingStage.vue:265` 的 Teleport），主窗口只把它的控制帧转发给观众（`:269` 的 `@send="share.sendControl"`）。所以直接抓视频轨**拍不到任何批注**。

但「拍不到」在不同平台上不是同一回事，合成必须跟着平台走：

| 路径 | 墨迹在视频轨里吗 | 控制帧转发吗 | 截图 |
|---|---|---|---|
| Windows / macOS 原生覆盖层 | 否（`setContentProtection`，`desktop/src/main/annotateOverlay.ts:110`） | 是 | **必须合成**，否则图上看不到批注 |
| Linux 原生覆盖层 | 是（无采集排除 API） | 否（转发会画两遍） | 镜像为空，天然不合成 |
| 浏览器 Document PiP 回退 | 取决于共享范围 | 是 | 合成；共享整屏时 PiP 浮窗本身可能也在画面里（已知限制） |

做法：`useLiveShare` 里放一个本地 `useInkBoard()` 镜像，喂它的地方有两个——

- `sendControl` 里 `t: 'ink' | 'ink-undo' | 'ink-clear'` 的帧（发布端自己画的，原生覆盖层经 `onAnnotateInk` 也汇到这里）；
- `onViewerControl` 里同样这三类帧。协议**允许**观众发墨迹（`VIEWER_MESSAGE_TYPES` 含 `ink`，`liveControl.ts:73`），今天没有 UI 发它，但镜像不该假设这一点——漏了这一路，将来观众能画时截图就会少笔画。

截图时把镜像的 `strokes` 按归一化坐标重绘到同一张 canvas 上（算法照抄 `LiveInkLayer.vue:37` 的 `draw()`，线宽按 `w/800` 缩放）。

- **合成的是镜像里有的笔画**，不做平台判断：Linux 上没人转发，镜像本来就空，不会重复绘制；win/mac 上转发了，正好补上被排除的那层；
- 激光点**不烧进图**：它是瞬时指向，不是内容；
- 标注窗口关闭时清空镜像（原生覆盖层的 `onAnnotateClosed`、PiP 的 `@close` 两处），否则下一张截图会带上已经撤掉的旧笔迹；
- 远程控制注入的是指针/键盘事件，不是笔画，与这里无关。

### 5.5 不抓什么

- **没有画面在播时**：语音会议与纯摄像头会议默认没有屏幕轨（`captureKind` 为 `null`），不抓、也不留空目录。判断依据是「此刻有没有在播的画面」，不是会议类型——语音会议里成员共享屏幕后照样有画面可抓（见 §5.6）；
- **页面隐藏时**：桌面端不受影响——发布期间 `setPublishingActive(true)` 已经把发布窗口的节流关掉（`desktop/src/main/capture.ts:123`），最小化也照常按触发拍。安卓与浏览器会被节流到 1 次/分钟，接受这个降级，不为此加常驻 rAF（rAF 在隐藏页直接停摆）。

### 5.6 成员共享的画面

成员共享时，发布端通过 `onRemoteScreen`（`useLiveShare.ts:1874`）收到那一路轨，并原样中继给其他观众。所以截图器什么都不用改——它抓的本来就是「共享者那一路」（`screenSharer ? remoteScreenStream : previewStream`）。

几个必须说清的点：

- **能截，而且语音会议里也能**。中继 m-line 由观众端的 offer 创建（`liveViewer.ts:591`），不依赖主持人自己有没有共享屏幕；
- **两边同时共享时截成员那一路**：观众的主画面仍是主持人的屏，但被讲解的是成员那一路（时间线里共享者是唯一的），截图跟共享者走；
- **画质等于观众所见**：这是经 WebRTC 收下来再中继的一路，不是成员本机的源轨。主持人自己共享时抓的是本机源轨，最清晰；成员共享时略低一档。截图用于回看，不承担「保存原始素材」的职责；
- **没有批注可合成**：标注窗口只叠在主持人自己的画面上，成员共享时主持人也画不上去，所以这一路镜像为空，天然不合成；
- **`ref` 记 `viewer:<id>`**，详情里显示成「成员共享」而不是「打开文件」；
- 成员停止共享后源自动回落到主持人自己的屏（或没有），因为源是抓的瞬间现读的。

## 6. 数据与存储

### 6.1 表

`live_notes_shots`，加进 `internal/service/live_notes.go` 的 `LiveNotesTablesDDL`（`CREATE IF NOT EXISTS`，无需 ALTER），索引加进 `LiveNotesIndexesDDL`：

```sql
CREATE TABLE IF NOT EXISTS live_notes_shots (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT NOT NULL,
	project_id INTEGER NOT NULL DEFAULT 0,
	client_shot_id TEXT NOT NULL,      -- 客户端生成，重试幂等
	at_ms INTEGER NOT NULL,            -- 发布端 Date.now()，与 segments 同一时钟
	reason TEXT NOT NULL,              -- manual|file|sharer|switch|speech|change
	ref TEXT NOT NULL DEFAULT '',      -- 文件路径 / viewerId / 段 id，可空
	file_name TEXT NOT NULL,           -- "shots/1700000000000-manual-3f2a.webp"，相对会议目录
	width INTEGER NOT NULL DEFAULT 0,
	height INTEGER NOT NULL DEFAULT 0,
	bytes INTEGER NOT NULL DEFAULT 0,
	hash TEXT NOT NULL DEFAULT '',     -- 十六进制 dHash，排查与二次去重用
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(token, client_shot_id)
);
CREATE INDEX IF NOT EXISTS idx_live_note_shots_token ON live_notes_shots(token, at_ms);
```

`file_name` 是**相对会议目录**的路径，由服务端拼出；客户端永远不传路径，因此没有目录名解析与路径穿越问题（与 `dir_name` 的既有原则一致）。

`client_shot_id` 不能用裸 `crypto.randomUUID()`：局域网是 http，不是安全上下文，这个 API 在某些环境下不存在。照 `useGlobalEvents.ts:160` 的写法做 `crypto.randomUUID?.() ?? 时间戳+随机串`。

### 6.2 目录

```
<项目>/.clawbench/notes/2026-11-03-1430-发版评审-a1b2c3/
    minutes.md
    transcript.vtt
    transcript-revised.md
    chat.md
    shots/
        .gitignore          ← "*"，与 audio/ 同理
        1730612345678-file-3f2a.webp
    audio/*.webm
```

`LiveNotesPaths` 增加 `Shots`。落盘走 `EnsureMeetingDir`（会议行在开场时已存在，标题与开始时间从行里取，客户端不传），再 `MkdirAll(shots)` 并尽力写一份 `.gitignore`（与 `audio/` 同理）。**目录按需创建，不在 `ensureLiveNotesDirs` 里预先建**：空目录会在资源页签里多出一个没有内容的条目。命名 `%013d-%s-%s.<ext>`：毫秒时间戳保证排序与不撞名，reason 便于人工翻目录，短哈希区分同毫秒。

单张上限 2MB、单场 100 张，服务端两侧都校验。预期一张 80–300KB，一场会议 3–20MB。

尺寸不必猜：它就是源轨的尺寸。桌面 `screenCaptureConstraints()` 封顶 1920×1200，安卓 `ANDROID_CAPTURE_MAX_WIDTH=1280` 且原生已压成 JPEG q55，所以安卓上截出来的图上限就是 1280 宽、且带着那一次 JPEG 的损失。这不是截图引入的，是推流本身的画质。

### 6.3 为什么不写进 minutes.md

`minutes.md` 里写 `![](shots/x.webp)` 看起来最省事，但相对路径的基准在两个消费者里不同：

- 文件预览按 `baseDir = dirname(文件)` 解析（`useMarkdownRenderPipeline.ts:56`）；
- 引用到聊天后按项目根解析。

同一段 Markdown 不可能同时满足两者。所以纪要文件保持纯文本，画面只在应用内展示（转写时间线 + 资源页签）。这也让 `minutes.md` 可以安全地被复制、外发、进 git。

### 6.4 生命周期

- 跟随会议记录：删记录 = 删目录（`shots/` 在目录里，自动带走）+ 删行；
- 单张可删：只删文件与行，不动其他产物；
- 项目删除：`internal/service/project_delete.go:40` 的清单加 `live_notes_shots`；
- `DeleteMeetingRecord` 的 DELETE 列表（`live_notes.go:1243`）加一条。

## 7. 接口

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/live-share/notes/shot` | 登录 + 项目 + token 归属 | multipart：`token, clientShotId, at, reason, ref, width, height, file` |
| GET | `/api/live-share/notes/shots?token=` | 登录 + 项目 + token 归属 | 全量列表（几十条），每条带服务端算好的 `offsetMs`（详情页按它插进转写） |
| DELETE | `/api/live-share/notes/shot?token=&id=` | 登录 + 项目 + token 归属 | 删行 + 删文件 |
| GET | `/api/live-share/notes?token=&since=` | 登录 + 项目 | **响应增加 `shots`**（刷新后恢复计数用） |
| GET | `/api/live-share/notes/sessions?token=` | 登录 + 项目 | 记录增加 `shotCount`（列表徽标 / 详情） |

**图片不新开端点。** 缩略图用现成的 `/api/fs/thumb?target=<项目相对路径>&w=480`（`FileThumb`，带 ETag / 304 / 解码缓存），全图用现有的图片查看器 `openFilePath()`。理由：`resolveAbsPath` 已经给了项目作用域与路径校验，再写一个二进制端点只是把同一件事做第二遍。

**但要先让缩略图认识 WebP**：`thumbDecodeExts`（`internal/handler/file_thumb.go:206`）只有 png/jpg/jpeg/gif，WebP 会直接 404「unsupported image format」。改动是加 `_ "golang.org/x/image/webp"`（依赖已在 `go.mod`）与 `.webp` 一项，顺带补上文件管理器本来就缺的 WebP 缩略图。

- token 归属复用 `liveNotesOwnedByProject`（房间存活或已有听记行），与 segment / files / screen 完全一致；
- `reason` 白名单校验，非法值 400；`at` 落在 `[meeting.started_at, now]` 之外时**钳到区间**（客户端时钟偏差不该让整张图丢掉）；
- 魔数只接受 WebP / PNG / JPEG；其余 400 并丢弃请求体（不落盘、不落库）；
- 幂等：`(token, client_shot_id)` 冲突时返回已有行，客户端重试不会产生第二张；
- 公开端点 `/api/share/{token}/notes` **不返回 shots**（§2.2）。

`openapi.yaml` 同步这三条新路径与 `/notes` 的字段；路由注册在 `internal/handler/handler.go` 的 `RegisterRoutes`。

## 8. 前端结构

| 文件 | 职责 |
|---|---|
| `web/src/share/shotCapture.ts` | 叶子模块（无 Vue）：dHash、编码、调度、配额、上传队列。仿 `liveNotes.ts` 的可单测形态 |
| `web/src/composables/useMeetingShots.ts` | 单例：`start/stop/pause/resume/captureNow`，暴露 `count`、`paused`、`lost` |
| `web/src/composables/useMeetingOptions.ts` | 新增 `shots` ref（默认 `true`），创建页与会议中共享同一份 |
| `web/src/utils/meetingFileTracker.ts` | 新增订阅接口 `onMeetingFileOpened(cb)`，返回取消函数；在 `noteMeetingFileOpened` 里同步触发，不等 5 秒批量上传 |
| `web/src/composables/useLiveShare.ts` | `watch(currentScreenSharer)`（`:942`）与 `switchSource` 里调 `shots.noteSourceChanged()`；`sendControl` / `onViewerControl` 里镜像 `ink*`；`start` / `stop` 起停截图器 |
| `web/src/components/live/TranscriptList.vue` | 新增 `kind: 'shot'` 行：缩略图 + 原因 + 时间（这个组件同时被发布端面板、观众面板和记录详情使用，没有截图数据时只是不会出现这种行） |
| `web/src/components/live/MeetingDetail.vue` | `load()` 里多取一次截图列表，按 `offsetMs` 并进 `transcriptCues` 再交给 `groupNoteSegments` |
| `web/src/components/live/MeetingStage.vue` | 听记页签底部一行：「已留存 N 张 · 暂停截图」；「其他」分组加一个「关键画面」勾选框 |
| `web/src/components/live/MeetingList.vue` | 元信息行加「N 张画面」（`meeting-row__meta`） |
| `web/src/composables/useMeetingResources.ts` | 新增 `shots/` 分组（与 `audio/` 分支同构；现在 `shots` 只会显示成一个可点开的目录项） |

### 8.1 依赖方向

`useLiveShare` → `useMeetingShots` → (`shotCapture`, `meetingFileTracker`) 单向。`useMeetingShots` **不得** import `useLiveShare`（否则成环）；它通过 `start({ getSource, getStrokes, token })` 拿取源与笔画，在**抓的瞬间**调用 getter，不做缓存。切源/成员共享只调 `noteSourceChanged()` 触发一次拍摄，不负责更新源——源永远现读。

### 8.2 状态归属

- `shots` 开关：`useMeetingOptions` 的模块级 ref，创建页与会议中同源（与 `notes` 一致）；
- 计数 / 暂停 / 丢失数：`useMeetingShots` 单例，切页签不中断；计数是**客户端本地**的，不轮询服务端（刷新页面后由 `/notes` 的 `shots` 恢复）；
- 会议结束：**不能 await**。`stop()` 里同步停采集，然后把队列连同 token 交出去自己跑（与 `notes.finishUploads()` 同一个模式）；否则一场结束要卡 10 秒才回到列表。纪要生成同样不依赖截图，两者不互相等待。

### 8.3 异步代际

`useLiveNotes` 用 `sessionGen` 防止「上一场会议的迟到请求写进下一场」。截图器同样需要：`start()` 递增代际，所有 await 之后比对，不匹配就丢弃。这是同一个单例模式下的同一类缺陷，别重新发明一遍。

### 8.4 详情页合并

`MeetingDetail` 的转写块来自 VTT cue（`startedAt` 相对时间线原点）。截图带的是绝对 `at_ms`，服务端在列表时顺带算好 `offsetMs`（用 `LiveNotesTimelineBase`，`internal/service/live_notes.go:1540`），客户端直接按 `offsetMs` 插入排序，不自己换算。

三个实现约束：

- **必须并进 `transcriptCues` 再排序**：`groupNoteSegments`（`liveNotes.ts:159`）只按输入顺序遍历、不排序，直接 push 到末尾会把截图全排到会议结尾；
- **`text` 不能为空**：`groupNoteSegments` 开头 `if (!s.text.trim()) continue` 会静默丢掉空文本行。截图行要带一句可读文本（如「打开 architecture.md」），图片放在附加字段里；
- **并进同一个数组还顺带解决了空状态**：详情页的转写页签在 `transcriptLines.length === 0` 时显示三种空状态（`MeetingDetail.vue:104`），只有截图没有转写的会议会被判成「没有转写」而整块吞掉。合并之后长度不为 0，自然显示。

其余：

- 没有转写时（听记关着但截了图）`LiveNotesTimelineBase` 返回 0，此时退回会议 `started_at` 作原点，否则所有偏移都会变成「1970 年以来的毫秒数」；
- 早于第一条转写的截图（开场就打开了文件）`offsetMs` 可能为 0，排在开头，正常；
- 不写进 `transcript.vtt`：VTT 表达不了图片，写一行假 cue 只会让外部播放器多一条看不懂的字幕。

### 8.5 i18n

新增 `meeting.shots.*`（`reasonManual / reasonFile / reasonSharer / reasonSwitch / reasonSpeech / reasonChange`、`count`、`pause`、`resume`、`delete`、`deleteConfirm`、`lost`、`empty`）与 `meeting.create.shots`。中英同步，缺键会被全仓守卫抓到。

另外**两处**既有的删除确认都要补「画面」：`meeting.list.deleteConfirm`（列表行删除，`zh.ts:4010`）与 `meeting.detail.deleteConfirm`（详情 footer，`zh.ts:4117`）。它们现在都写着「纪要、转写、录音、聊天」，漏了会让确认框与实际删除范围不一致。

## 9. 界面

### 9.1 会议中

听记页签底部一行状态（有截图时才出现）：

```
已留存 6 张    [暂停截图]
```

不弹窗、不打断。暂停是临时的（只影响本场），不落配置。

### 9.2 详情 · 转写

截图作为时间线行插入，与文件/共享事件同列：

```
[icon] 画面   12:30 · 打开 architecture.md
              ┌──────────────┐
              │  (缩略图)     │
              └──────────────┘
```

- 点击放大（复用文件预览的图片查看器）；
- **删除放在放大视图里**，不在缩略图上挂小叉：触屏上 16px 的按钮点不准，而误删是不可逆的；
- 原因文案由 `reason` 决定；`ref` 是文件路径时追加文件名；
- 缩略图走 `/api/fs/thumb?target=<项目相对路径>&w=480`（`loading="lazy"`），点开走现有图片查看器；两者都不新开端点。

### 9.3 详情 · 资源

`shots/` 作为一个分组列出（与 `audio/` 同构），可直接拖进聊天引用。

## 10. 隐私与默认值

- **开关独立**：创建页「声音与听记」下新增「关键画面」，**与听记完全解耦**（可以只要文字不要图，也可以只要图不要文字），默认开；会议中可关。关掉即不抓、不上传。中途打开则从打开那一刻开始抓，不补拍之前的内容。
- **开关始终可见**，包括语音会议：语音会议随时可能有人共享屏幕，藏起来会留下「成员共享了但一张没拍」的洞。
- **开关只归主持人**：成员端没有开关也没有提示，与听记一致（听记同样在录成员的声音）。开关管的是「这台主持人机器留不留档」，成员共享的画面同样受它约束。
- **只抓共享轨**：共享单个窗口时，绝不为了「拍全」去抓整块屏幕——那会把没共享的窗口也存下来。
- **可见**：会议中显示已留存张数；暂停后状态明确。
- **可删**：单张可删；整场随记录删除。
- **不新增权限**：抓的是已经在发送的那一路轨，不触发第二次系统授权。

## 11. 边界与异常

| 情况 | 行为 |
|---|---|
| 上传失败 | 1s/3s/9s 退避重试，仍失败则计数并在界面显示「N 张未保存」，不静默丢 |
| 服务端返回 404（房间已销毁、行不存在） | 停止整场上报，与听记一致 |
| 编码抛错（内存/画布） | 丢弃该张，计数，不重试 |
| 切源中（新轨未就绪） | 等首帧；2 秒仍无帧则放弃本次触发 |
| 页面隐藏 | 桌面不受影响（发布窗口已禁用节流）；安卓/浏览器降频，手动与文件触发在页面可见时仍工作 |
| 会议被强制结束（进程退出） | 已上传的都在；未上传的随内存消失，最多丢队列里那几张 |
| 时钟偏差 | 服务端把 `at` 钳进会议区间 |

## 12. 测试

**Go**

- `internal/service/live_notes_shots_test.go`：插入/排序、幂等（同 `client_shot_id` 两次）、配额上限、单张删除、路径拼接不接受客户端路径、`DeleteMeetingRecord` 连带清理、项目删除清单；
- `internal/handler/live_notes_test.go`：鉴权三态（缺项目 403、他人项目 404、房间已结束但有行 → 允许）、`reason` 白名单、魔数嗅探（`text/plain` 被拒）、超限 413、`at` 钳位、`/notes` 响应带 `shots`、`/shots` 的 `offsetMs` 在「无转写」时以会议开始为原点；
- `internal/handler/file_thumb_test.go`：WebP 源图能出缩略图（新加的 decoder 注册），以及 `.webp` 之外的既有格式没有回归；
- 迁移测试：旧库（无 `live_notes_shots`）启动后表与索引都在。

**前端**

- `web/src/share/__tests__/shotCapture.test.ts`：dHash 距离与阈值、同屏丢弃、最小间隔、配额降级、WebP→PNG 回退、单飞队列（第二张排队而非并发）、手动不受最小间隔限制、源为空不抓、`srcObject` 换源后必须等新尺寸（旧尺寸的帧不许拍）；
- `web/src/composables/__tests__/useMeetingShots.test.ts`：文件事件触发（reason/ref 正确）、关键词触发只对每个段 id 触发一次、暂停期间不抓、成员共享时截的是中继流（`ref` 为 `viewer:<id>`）且成员停止共享后回落到主持人的屏、`stop()` 后迟到的编码不得上传（代际守卫，用 pending Promise 在请求在飞时断言）、重启后计数清零；
- `web/src/components/live/__tests__/MeetingDetail.shots.test.ts`：`offsetMs` 插入位置（乱序输入也要按时间排好）、原因文案、缩略图 URL、删除后从列表移除、**只有截图没有转写时不显示「没有转写」空状态**；
- `web/src/share/__tests__/useInkBoardMirror.test.ts`（或并入 `useLiveShare.test.ts`）：`sendControl` 与 `onViewerControl` 两个方向的三类 ink 帧都被镜像、窗口关闭后清空、激光不写入 strokes。

**e2e**：抓帧依赖真实 `getDisplayMedia` 与帧回调，headless 下不可靠，不进 e2e；用 `live-share.spec.ts` 保证会议主链路没被改坏即可。

## 13. 文档同步

- `docs/spec/features/live-share.md`：会议产物增加 `shots/`，接口表增加三条（上传/列表/删除）；
- `AGENTS.md` 的会议段落（`live_notes_*` 描述）补一句「关键画面」；
- `internal/api/openapi.yaml`：三条新路径 + `/notes` 与 `/sessions` 的响应字段。

## 14. 已知限制

- 语音触发滞后 2–5 秒（STT 往返），抓的是「仍在看的那一屏」，不是精确帧；
- 只对屏幕/窗口/成员共享生效：语音会议与纯摄像头会议本身没有画面，成员共享后才有；
- **截图的上限就是推流的画质**：桌面封顶 1920×1200，安卓 1280 宽且已被原生压成 JPEG q55，画面里的细小文字在安卓上就是糊的——要更清楚只能先提高推流画质，截图救不回来；
- 成员共享的截图来自中继流，清晰度等于观众所见，低于主持人自己共享时的本机源轨；
- 安卓切到后台后 WebView 的定时器与帧回调会被节流，此时由文件/共享触发的截图可能拍不到（桌面端不受影响，发布期间窗口禁用了节流）；
- 不支持观众端截图；观众要留帧需要另设「请求留存」通道（v2）；
- 不做 OCR / 视觉描述，因此纪要模型看不到图（`minutes.md` 也不嵌图）。

## 15. 实现顺序

1. **最小可用**：表 + 目录 + 上传/读取/删除接口 + 文件打开与切源两种触发 + 手动按钮 + 转写内联与资源分组；
2. **降噪**：dHash 去重、最小间隔、配额、关键词触发、计数与暂停；
3. **收尾**：标注合成、丢失计数与重试、资源页签缩略图、文档同步。

每步都能独立上线：第 1 步已经能回答「这场会看了什么」，第 2 步只影响留存数量，第 3 步只影响还原度。

## 附：代码现状核对（2026-11-03）

设计定稿前逐条读过代码，下面这些是**已验证的事实**，实现时不必重新推导；带「注意」的是容易踩反的地方。

| 结论 | 依据 |
|---|---|
| 服务端拿不到媒体，只有 SDP/ICE 中转 | `internal/handler/live_share.go`；`internal/liveshare/` 只有房间/发言/信令 |
| 成员共享的那一路在发布端本地 | `onRemoteScreen(viewerId, track)`，`useLiveShare.ts:1874` |
| 中继 m-line 恒存在，与会议有没有画面无关 | 观众端 `addTransceiver` 固定四条（`liveViewer.ts:591`），发布端按序绑定（`useLiveShare.ts:1545`） |
| 「谁在共享」已有唯一判据，且成员优先于主持人 | `currentScreenSharer()`（`useLiveShare.ts:900`），`watch` 在 `:942` |
| **注意**：主持人换源不会触发上面那个 watcher | 前后都是 `'host'`，`captureKind` 不变；必须在 `switchSource`（`:1709`）里显式通知 |
| 停止共享时 `screenSharer` 会被清空 | 服务端 `broadcastScreenState` 发的是 `room.ScreenSharer()`，空即 `""`（`live_share.go:1083`）；客户端 `:1428` 直接赋值 |
| 文件事件是即时的，上传才是 5 秒批 | `FLUSH_INTERVAL_MS`（`meetingFileTracker.ts:19`）与 `noteMeetingFileOpened`（`:135`） |
| **注意**：短于 1 秒的访问会被从时间线删掉 | `closeCurrent`（`:163`），所以截图要等到 1.2 秒 |
| 标注画在独立窗口，主窗口只转发控制帧 | `MeetingStage.vue:265`（Teleport）与 `:269`（`@send="share.sendControl"`）；原生覆盖层 `onAnnotateInk` → `sendControl`（`:985`） |
| win/mac 的覆盖层被排除在采集之外，Linux 不被排除且不转发 | `desktop/src/main/annotateOverlay.ts:1-20` 与 `:110` |
| 协议允许观众发墨迹 | `VIEWER_MESSAGE_TYPES` 含 `ink`（`liveControl.ts:73`），当前 UI 不发 |
| 墨迹镜像可直接复用状态机 | `useInkBoard().apply(msg)` 已处理 ink / ink-undo / ink-clear（`useInkBoard.ts:98`） |
| 详情页 5 个页签，转写来自 `transcript.vtt` | `MeetingDetail.vue:343`（tabs）、`:521`（`parseVttCues`） |
| **注意**：`groupNoteSegments` 不排序、且丢弃空文本 | `liveNotes.ts:159-186` |
| **注意**：转写页签为空时会被三种空状态接管 | `MeetingDetail.vue:104-132`，只有截图没有转写的会议会被吞掉 |
| 缩略图端点只解 PNG/JPEG/GIF | `thumbDecodeExts`（`file_thumb.go:206`）；WebP 需补一行注册 |
| 缩略图端点的鉴权与路径校验已具备 | `resolveAbsPath`（`handler.go:313`）按项目 cookie 解析相对路径 |
| 采集分辨率上限：桌面 1920×1200，安卓 1280 + JPEG q55 | `liveMediaPolicy.ts:40`；`useLiveShare.ts:49-51` |
| 桌面发布期间窗口禁用了后台节流 | `setPublishingActive` → `desktop/src/main/capture.ts:123` |
| 安卓的「屏幕」本来就是 MediaStream | `androidScreenCapture.ts` 把 JPEG 帧写成 `VideoTrackGenerator` 轨，所以三端可用同一套抓帧 |
| `crypto.randomUUID` 在局域网 http 下可能不存在 | `useGlobalEvents.ts:160` 已有回落写法 |
| 会议行在开场就存在，目录按需创建 | `StartLiveNote` + `EnsureMeetingDir`（`live_notes.go:933`），标题/开始时间可从行里取 |
| 记录列表的产物标记是**探目录**得出的 | `liveNoteArtifactFlags`（`live_notes.go:1147`），加 `shots` 计数即同构 |
| 没有现成的抓帧/编码工具可复用 | `requestVideoFrameCallback` / `OffscreenCanvas` / `toBlob` 全仓无命中；`createImageBitmap` 只在安卓采集里解码 JPEG 帧（`androidScreenCapture.ts:173`） |

## 16. 实现记录（2026-11-03）

| 层 | 文件 |
|---|---|
| 表与存储 | `internal/service/live_notes_shots.go`（DDL 在 `live_notes.go` 的两个常量里）、`LiveNotesPaths.Shots`、产物计数、删除清单 |
| 接口 | `internal/handler/live_notes.go`：`ServeLiveShareNoteShot`（POST 上传 / DELETE 删除）、`ServeLiveNotesShots`（列表 + `offsetMs`） |
| 缩略图 | `internal/handler/file_thumb.go` 注册 `x/image/webp` |
| 抓帧 | `web/src/share/shotCapture.ts`（dHash / 调度 / 单飞队列）、`web/src/composables/useMeetingShots.ts`（DOM 抓帧与触发） |
| 接线 | `useLiveShare`（墨迹镜像、源变化、起停）、`meetingFileTracker`（开文件订阅）、`useMeetingOptions.shots` |
| 界面 | `TranscriptList`（截图行）、`MeetingDetail`（合并 / 打开 / 删除）、`MeetingStage`（开关与计数）、`MeetingList`（徽标）、`MeetingResources`（`shots/` 分组） |
| 文案 | `meeting.create.shots`、`meeting.shots.*`、`meeting.list.shotShort`；两处删除确认补「画面」 |

### 与设计的偏差

1. **删除入口放在转写行上**，不只在放大视图：现有的图片查看器没有删除动作，把删除藏进去等于没有入口。行内按钮是 22px 命中区 + 确认框，触屏可点。
2. **`shots/` 目录按需创建**，`ensureLiveNotesDirs` 不预先建（见 §6.2）。
3. **截图行不进 `transcript.vtt`**，详情页在客户端合并；`/notes/shots` 返回的 `offsetMs` 已经算好原点，客户端不再换算。
4. **`serveLiveShareNoteShotUpload` 拆成了五个小函数**（`parseShotUpload` / `shotFields` / `readShotImage` / `shotQuotaAvailable` / `shotMeeting`）：原实现圈复杂度 25，超过仓库的 15 上限。

### 2026-11-03 追加：画面行复用 markdown 图片控件

画面行最初是自绘的 `<button><img></button>`（360dp 上限），点击经 `openFilePath` 跳进文件管理器查看器。现改为**渲染成 markdown 图片同一个 figure**（`media-block.css` 的 `.image-block-wrapper` + `--thumb` 变体）：

- **缩略图**：inline `<img>` 仍走 `w=480` 缩略端点；新增 `data-full-src` 指向 `shotFullUrl`（同端点 `w=1600`，服务端按需栅格化，已是它能给出的最大宽度），灯箱用它而不是缩略图；
- **打开**：不再有 `shot-open` 事件——打开是 `Lightbox.vue` 的文档级委托点击（桌面点悬浮查看按钮、触屏点图片本体），与 markdown 图片同一条路。分享页的 `Lightbox` 是 `ShareView` 的兄弟组件，provide 不到，正是这条委托路径让分享端**原本点不动的画面**获得了查看器；
- **翻页**：列表根元素标 `data-media-gallery`，`handleLightboxClick` 的画廊容器选择器从 `.markdown-body, .chat-message` 扩到它（转写不能挂 `markdown-body`——那会给整个时间线套阅读排版）；
- **尺寸**：`--thumb` 变体按**高度**封顶 150dp（宽 260dp 兜底横长截图），否则时间线里一张 16:9 截图占掉整个可视高度；这是 figure 体系里第一个「时间线行」尺寸场景，与「聊天即文档」的单一尺寸策略不冲突——时间线行的图不是内容本体。
- 测试：`TranscriptList`（figure 类与 data-full-src / 画廊标记 / 不再自持点击）、`MeetingDetail`（data-full-src 指向 w=1600）、`liveNotes`（thumbUrl 与 fullUrl 分离）、`Lightbox`（`data-media-gallery` 集合翻页）；两个曾用窄 lucide mock 的测试改为 spread `importOriginal`（共享子组件加图标即崩的旧坑）。

### 性能（2026-11-03 优化）

- 抓帧拆成 `domSample()`（drawImage + dHash）与惰性的 `encode()`：被调度器丢弃的帧不再编码。兜底触发的成本从 ~30–60ms 降到 ~3ms，按现在的触发频率约省掉 3/4 的编码；
- 两张 canvas 复用（全尺寸 + 9×8 采样），会议结束释放那块 ~9MB 的帧缓冲；
- `captureNow` 单飞（`capturing` 标志）：canvas 是共享的，两次抓帧交错会编码出对方的画面。

### 测试

- `internal/service/live_notes_shots_test.go`：幂等、排序、命名与落盘、删除、路径钳制、计数；
- `internal/handler/live_notes_shots_test.go`：鉴权、魔数拒绝、入库 + 文件、`at` 钳位、`offsetMs`（有/无转写）、删除幂等；
- `internal/handler/file_thumb_webp_test.go`：WebP 能出缩略图；
- `web/src/share/__tests__/shotCapture.test.ts`：dHash、去重、间隔、配额、重试分类、队列溢出、id 回落；
- `web/src/composables/__tests__/useMeetingShots.test.ts`：上传字段、同屏去重、手动绕过、暂停、结束代际、文件 1.2s 触发、关键词触发；
- `web/src/components/live/__tests__/MeetingDetail.test.ts`：截图按偏移插入、缩略图 URL、删除、只有截图时不显示「没有转写」；
- `web/src/utils/__tests__/meetingFileTracker.test.ts`：订阅在记录点触发、取消订阅、同文件不重复；
- `web/src/composables/__tests__/useLiveShare.test.ts`：墨迹镜像与清空；
- `web/src/composables/__tests__/useMeetingResources.test.ts`：`shots/` 独立分组。
