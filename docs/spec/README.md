# ClawBench 系统设计规格

ClawBench 是移动端交互适配优先、桌面端完整支持的多端 AI 工作台，将多种 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、DeepSeek Harness、MiMo-Code、Pi、Copilot、Kimi、Antigravity、Grok Build、ZCode、Cursor、Hermes Agent、Command Code、OpenClaw）包装为 Web 可访问的平台。Go 后端通过 shell 调用 CLI 工具并经 WebSocket 流式输出 JSON，同时支持 ACP（Agent Client Protocol）stdio 传输，提供结构化的模式切换、斜杠命令和权限管理。Vue 3 前端实时渲染流式事件。支持 SSH 隧道端口映射、SSH 公网隧道、任务系统（含 GitHub/GitLab 事件触发）、会话标签、**多智能体群聊**、零配置启动引导、聊天自动摘要、钉钉/飞书/微信企业推送、系统资源监控、thinking 惰性加载和消息聚类分析。

> 本目录是**系统设计规格**（面向开发者与 AI 的心智模型）。面向使用者的图文操作手册在 [`docs/user-guide/README.md`](../user-guide/README.md)。

## 模块地图

### 数据模型

| 模块 | 说明 |
|------|------|
| [数据库表结构](database.md) | 主库 `ClawBench.db`（SQLite，WAL，foreign_keys=ON，RAG 独立连接池）：按全新安装实测的 **36 张表**逐表列定义（chat_sessions（含群成员 `agent_name`/`agent_avatar` 快照列、`title_source` 改名等级、`pinned`/`sort_order` 排序）/ chat_history（含运行中插话 `injected` 列）/ chat_metadata（`cost_usd` 单轮增量 vs `cost_session_total` 原始累计）/ chat_tool_calls / chat_thinking / summaries / tts_summaries / scheduled_tasks（`trigger_mode` cron·event）/ task_executions（前置脚本字段）/ agents（含自定义头像 + `disabled` + `project_id` 作用域 + `mcp_servers` 策略）/ projects / recent_projects / queued_messages / chat_recommendations / btw_questions / session_tags / session_tag_links / file_shares / session_shares / project_forges / forge_items / forge_events / forge_pipeline_runs / forge_sync_state / notifications / pending_events / dingtalk_subscribers / feishu_subscribers / group_pending_bcc / message_clusters_cache / message_clusters_meta / forwarded_ports / terminal_quick_commands / terminal_key_config / chat_quick_send / schema_migrations）、外键级联汇总（**DB 外键仅 4 条**；独立用量台账 `chat_metadata` 刻意不级联、`projects` 不被他表声明外键）、RAG 三表（`rag_chunks` / `rag_chunks_fts` / 惰性创建的 `rag_vec`）与 ER 关系图 |

### core/ — 核心业务

| 模块 | 说明 |
|------|------|
| [聊天流程](core/chat-flow.md) | 用户发消息到 AI 回复的完整链路：handler → 唯一 turn 实现 → AI 后端 → WebSocket StreamHub → 前端；含 ACP 权限审批、交互式提问卡（`<clawbench-ask-question>` → AskUserQuestion 工具调用，单选可取消，答案状态跨重渲染持久化）、/cb-* 内置命令注入（含 `conversationProjectsList` 跨项目定位）、`/btw` 旁路问答（摘要模型基于压缩快照作答，独立表 `btw_questions`、不进对话）、请求构造唯一实现（直发与排队路径一致）、**异常终止自动续接**（分类器与消息构造唯一实现，取消优先）、文件附件行范围、自动摘要（AI 失败降级结论文本）、分叉上下文按优先级压缩（保留全部用户消息）、thinking 惰性加载、子智能体内容分组（按 `_meta` 父工具调用 id 折叠进父 Agent 卡片；父卡片缺失时按成员名兜底归组）、工具调用耗时、消息元信息区在气泡外（用户消息同样支持复制/详情）、会话重置（卡死会话一键重启进程保留上下文）、消息回溯 Rewind（原址截断会话历史并重启 AI 会话，同时清空计划面板）、完成通知（后台事件时纯通知卡片，类别 chip + 事件类型 chip + 主体名称 + 最多 4 行摘要 + 跨项目整卡换色与区隔带 + 跳转顺带标记已读 + 关闭，5 秒自动关闭，覆盖会话/任务/仓库且与系统通知对齐，详见[应用内完成通知](features/completion-popup.md)）、未读自动清除、错误码透传与展示、取消耗时与 finalize 阶段计时、滚动保持机制、按项目恢复上次会话、输入草稿与会话快照恢复、独立表消息队列（`queued_messages` 出队才落库，drain loop 原子出队 + 出队熔断 + 兜底回收器；DB id 顺序即对话顺序）、**运行中插话**（排队条目并入正在生成的回复，合并式后端标 `injected`）、流式块保序合并（mergeOrderedBlocks）、ACP `_meta` Token/成本明细（最新完整快照合并，供[用量统计](features/usage-stats.md)聚合）展示 |
| [AI 后端抽象](core/ai-backend.md) | 双传输后端（CLI shell-out + ACP stdio）、流式事件累加（AccumulateBlock + 回放检测 + 连续 thinking 合并 + AskQuestion 转换）、ACP 状态提取（mode/thinking/model）、ACP 崩溃诊断、acpStdoutFilter 协议修复（含 SessionModelState 提取）、ACP context_state 持久化、ACP 会话恢复重试与 NewSessionFallback（Initialize 握手超时短路为类型化错误、不作断连重试）、**用户自定义 ACP 智能体复用同一链路**（`custom-acp` 无 spec ⇒ 无 CLI 传输、无 ModelSource，能力全靠 agent 自报）、**智能体自检两级**（静态不 spawn 不计费 / 深度真跑一轮计费，分两个路由）、thinking 惰性加载、CodeWhale 字段重映射、Grok Build 双传输（ACP + streaming-json CLI）、ZCode ACP 桥接（zcode-acp-server）、共享规则模板、跨智能体 Skill 发现（`internal/skill`：递归扫描 + 五类来源去重（含项目级 `.agents/skills`）+ git 源同步 + `service.AppendSkillsSection` 两处 prompt 注入）、连接管理（AgentID/BackendID 无锁防死锁、用户取消保护存活连接、ensureAliveWithSession 使用 ResumeSession）、LoadSession 异步回放、ListSessions 磁盘扫描回退、EnsureAlive、**MCP 服务注入**（四条建会话路径全覆盖 + 每轮策略哈希比对触发重连，见 [MCP 服务](features/mcp.md)）、CodeBuddy Plugin Skills 竞态修复、CodeBuddy Agent Teams 桥接（`teamUpdate`/`memberEvent` 按成员名回填 parent id）、客户端能力协商按 agent 定制（Terminal / fs.readTextFile 作为行为开关）、ACP `_meta` 扩展元信息解析（per-agent 归一化 → chat_metadata）、子智能体父工具调用归属（`_meta` parentToolCallId / parentToolUseId → ParentToolCallID）、压缩后系统提示重注入（compact_detect）、重放过滤按整轮 requestId 集合判定、无进度看门狗按模型进展判定（与连接活性双信号分离）、**运行中插话策略按后端分列**（CodeBuddy `session/steer` 可切分 / Claude·Codex `_session/steering` 合并降级，`MidTurnSplitsReply` 能力位 + `PromptInFlight()` 幽灵轮守卫，注入走裸 `CallRaw` 通道）、**后端专属 ACP 约束落成能力位**（`ACPResumeIsProcessLocal` / `ModesMirrorThoughtLevel` / `ToolCallWrapperTitlePrefixes`，OpenClaw 的 Gateway 桥四条约束） |
| [流式传输体系](core/streaming.md) | 单一 WebSocket StreamHub（含断线 ≤10s 缓冲重放、≤50 条上限、>120s 清理订阅）+ 旁注小 SSE/WS 通道；含前端重连状态同步、subscribeOnly 模式、replay_done 事件、投递可观测（`/api/ws/delivery-stats`）、关键事件等待/高频增量丢弃的双层分级、遥测类事件非缓冲投递、流式入库批量合并与 Finalize thinking 单事务重写、前端有界缓冲回放 |
| [会话生命周期](core/session-lifecycle.md) | 聊天会话的创建、执行、排队、取消、归档（软删除）、物理删除（Destroy）、续接对话（标题时间戳前缀 + 锁定）、分叉（含 beforeMessageId、可选 Agent）、会话标题派生（transcript 双候选提取）、设置即时持久化、会话标签、异常终止自动续接（取消原因优先，与其他路径共用分类器）、过期归档自动清理、Codex 项目级历史会话发现（磁盘扫描 + ACP 合并）、单一 owner runner（运行态与可取消性同源）、唯一 turn 实现与队列兜底回收、优雅退出（WaitStreamsDrained + GracefulStopAll 等待流落库再回收进程） |
| [摘要管线](core/summarization.md) | 双管线（TTS vs 阅读摘要）、summarizeMessage 统一调度、SummaryCards 结构化卡片、摘要视图 warning/error 横幅通道、多 pass 压缩、Block 提取算法、降级链（AI 失败使用结论文本）、热重载、推荐回复（stable/rolling 分离 + prompt caching） |

### features/ — 功能特性

| 模块 | 说明 |
|------|------|
| [首次访问欢迎面板](features/setup-wizard.md) | WelcomeOverlay 后端检测面板（非 5 步向导）；Agent 创建走自动发现 + AgentInstallDialog；20 个后端规格；检测期间显示加载指示器 |
| [任务](features/scheduled-tasks.md) | cron 调度 → AI 执行 → 摘要推送，支持暂停/恢复/手动触发/续接对话，运行中流式状态展示，**异常终止自动重试**（与聊天路径共用分类器，跨轮保持 running），执行历史事件驱动（订阅 `task_update`，不再 3s 轮询），执行级逐条已读（不再切 tab 自动清零），**前置自定义脚本**（cron 可选，静默成功则跳过 AI 且不发通知，脚本阶段不计入「运行中」但可见可取消）；含事件触发任务（GitHub/GitLab 事件唤起，只读事件上下文注入（未选事件时上下文块整块隐藏），列表行与聊天预览卡均展示订阅事件而非空白 cron 字段，见 [Forge 集成](features/forge-integration.md)） |
| [Forge 集成](features/forge-integration.md) | GitHub/GitLab Issue + PR/MR 只读浏览（仓库绑定 + 列表/详情/评论）、面板内「动态」页签（未读/已读/全部条目聚合）、后台轮询感知变化（水位线 + 快照 diff）、CI 完成事件（per-run 去重表 + 按 run 去重 debounce）、流水线 ↔ PR 双向跳转、按条目未读与通知、事件触发 AI 任务、URL 附件「引用到对话」、按 host 凭据隔离与自部署实例 http/https、下拉菜单「打开仓库」 |
| [会话标签](features/session-tags.md) | 按项目隔离的标签定义（`UNIQUE(name, project_id)`，全局标签用 `project_id=0` 哨兵）+ 会话关联、长按菜单打标签、会话行标签行、顶部过滤栏（仅列在用标签，可换行 + 高度封顶）、可读性校准的哈希配色、胶囊即选中控件、失败可见、PATCH 全量替换语义 |
| [AI 群聊](features/group-chat.md) | 多智能体群会话（跨后端）：**主持人模式**（成员轮流控场，AI 决定发言者，标签路由 + 失败回退轮转）与**自由模式**（无主持人，@ 提及接力，@用户昵称交回人类）；**自动巡航**（正交开关，把终止从「交回用户」改为成员确认完成，host 一人拍板 / free 至少半数，六种主持人收工形态全拦改确认回合，开关每回合实时读，每回合按时间线高水位重置，会话信息栏「自动巡航」chip 显示「巡航中」）；成员是隐藏 `chat_sessions` 行（`group_member`）复用连接池/resume/空闲回收，消息只存群时间线一份；**同一智能体可多实例**（首个保留本名、重复实例取名池名 `Alice`/`Bob`…，池尽回退 `本名 序号`；成员名以 `agent_name` 为权威、头像以 `agent_avatar` 快照为准、真实智能体名经 `SpeakerIdentity.agentName` 披露）；增量注入游标（`seen_cursor`，失败不推进）、**@ 成员卡**（公开点名 + 密送批注）、**用户昵称**（`chat.user_nickname`，与智能体名双向唯一）、密送（`private` 提及 + `group_pending_bcc`，注入 fail-closed / 显示 fail-open）、成员**禁用/启用**（`chat_sessions.disabled`，只影响调度、不动行与上下文，启用后补齐禁用期间内容）与加人系统事件、**按成员的上下文用量（花名册 chip + 嵌套详情面板，数据源是成员行自己的 `context_state.usage`，未上报显示 `—`）与单成员压缩**（`POST /api/group/member-compact`，不走 `/api/ai/chat`、不在时间线产出输出、期间消息排队，开始/完成/失败写系统行）、干净收尾写「轮到用户发言」系统行、AskUserQuestion 交回、并发发言（快照不变 + 按 id 定位流式行）、单成员失败不终止整群、ACP sweep 保护、整轮只跑一次摘要 |
| [语音合成](features/tts.md) | 多引擎 TTS（Edge / OpenAI 兼容 / Piper / Kokoro / MOSS-Nano），总结后端三选一（simple / api / **auto 按内容杂质比例分流**），保守文本清理（不篡改技术文本），**群聊按发言人轮流朗读**（speakerName 前缀 + FIFO 队列），**缓存键并入合成配置**（引擎/音色/模型/速率，API key 排除），缓存策略 |
| [屏幕与声音共享](features/live-share.md) | Windows 桌面发布屏幕 + 系统声音 + 麦克风，token 链接给匿名观众在浏览器观看；**纯 P2P WebRTC**（服务端只转发 SDP/ICE，零新增端口）；**多人语音经发布端混音**（每观众独立混音排除自己，0 条额外连接，不需 SFU）；发言两层授权（房间开关默认关 + 每人麦克风开关）；**AI 听记**（按信道分别转写系统声音 / 发布端麦克风 / 每个观众，按时间重排成带说话人的时间线，结束后生成会议纪要）；**成员反向共享屏幕**（一次一人，发布端转发给所有人，成员端需 HTTPS）；观众上限 8，无 TURN 中继（仅同一局域网） |
| [语音输入](features/stt.md) | 双模式语音识别（流式 WS + 非流式 POST）、vLLM Whisper 引擎、增量识别 + 最终全量、安全上下文检测、快捷键触发 |
| [推荐回复](features/chat-recommendation.md) | AI 回复完成后自动生成下一步建议、stable/rolling 分离支持 prompt caching、快捷指令感知、离线恢复、会话隔离 |
| [Web 终端](features/terminal.md) | PTY 多标签会话（独立进程组防 /dev/tty 阻塞）、三模式手势系统（浏览/手势/选择）、拖拽选择+浮动复制栏、选中即复制（默认开启，150ms 沉降收敛手势，移动端复制栏降级保留）、虚拟修饰键、键位/符号配置、终端主题切换、终端输入抽屉、终端帮助抽屉、TUI 应用支持 |
| [Git 管理](features/git-management.md) | 历史浏览（含工作区变更页按需重拉）、文件 Diff 抽屉（prev/next 顺序导航）、提交详情面板文件/路径行标注跳转（经宿主 emit 记录跳转来源）、Worktree 隔离、分支/标签 CRUD、内联操作按钮、代码量统计（存量 cloc 快照，两层排除=内置规则 ∩ 项目 `.gitignore` + 增量 git stats 时间窗，双子页）、停靠页与抽屉共享同一套历史视图逻辑 |
| [文件管理](features/file-management.md) | 目录浏览（browse）+ 文件查看（view）独立 Tab、按 `.gitignore` 灰显 git 不跟踪的条目（仅淡化、仍可操作）、右键菜单命中范围与资源管理器一致、停靠预览窗格（工具栏开关 → 列表下方可拖拽高度的预览区，目录列出内容、文件复用代码切片/媒体渲染）、预览按行窗口取数（`lineStart`/`lineEnd` + `totalLines`，大文件不整体传输，行窗口不受整文件大小上限约束）、CodeMirror 代码编辑（浏览/编辑双模式）、**语法高亮覆盖 170+ 种语言且按需懒加载**（编辑器与 highlight.js 预览两条路径合计，首屏只带约 56 种高频语言，长尾每种一个动态 chunk；未加载时回落纯文本、语法包到达后由版本号触发重渲染）、VS Code 风格 sticky scroll、Markdown 标题锚定滚动同步、Markdown HTML 导出（共享渲染管线重建自包含单文件）、内联缩放（桌面端点图就地放大，滚轮围绕光标、拖拽平移）、代码链接预览（点击验证过的代码文件路径/path:line 链接弹出代码切片浮层卡片，图片目标可开灯箱，详见文件管理规格）、文件分享链接（capability token 公开只读，边界=创建时快照的根）、Excalidraw 画布编辑（iframe 内嵌独立构建 + 保存写回原文件）、内联音频/视频播放器、二进制文件处理（64KB/512KB 截断 + forceText）、目录导航栈、双候选路径解析、文件刷新与差异高亮（useFileRefresh 统一三种触发 + Markdown 块级差异 + 代码行级差异 + 两阶段闪烁）、刷新跳过加载遮罩、编辑、上传/下载产品内进度条（累计字节，三端统一）、上传（含文件夹上传/目录树下载/粘贴上传）、目录跳转（文件管理器接受绝对或项目相对路径、项目选择对话框仅绝对路径，提示文案分别如实描述）、**Windows UNC 共享（Samba）作为独立根**（无需映射盘符；拒绝裸服务器名与设备命名空间，POSIX 不启用）、拖放移动、面包屑拖拽到聊天（含多选一次性拖拽）、低频动作收进常驻「更多」菜单、预览滚动懒加载（无行数上限）、空文件不误报越界、Office 预览随容器尺寸自适应（ResizeObserver 覆盖分隔条拖拽与 dock 切换）、面包屑返回上一级、预览跟随键盘选择、排序、网格视图、键盘快捷键、代码符号提取、归档打包 |
| [文件发现](features/file-discovery.md) | 文件名搜索融合进文件管理器主界面（内嵌视图，结果复用目录条目交互与 git 忽略灰显）、按内容搜索（独立对话框：递归/正则/全词/大小写 + 包含排除 glob + 范围切换，SSE 流式、按文件分组、点击跳行）、结果展示所在目录、全局搜索蕴含递归、PC Shift 范围选、最近文件、统一覆盖层打开行为 |
| [附件与系统分享](features/attachments-and-share.md) | 多文件附件（含行范围）、上传历史（支持删除 + 一键清空，**合并聊天上传与文件管理器上传**：项目内清单 `.clawbench/upload-log.json` 记下落在 `uploads/` 之外的文件，每行带来源标记决定「删除」是否连文件一起删）、Share In（支持删除 + 一键清空）、**PWA 分享目标**（`/share-target` 免鉴权路由自行校验后 303 回应用）、**移动端拍照/照片/文件三入口**（各配独立隐藏 input）、**上传目录 TTL 清理**（默认关，按名字比对引用、读不到引用集整项目跳过）、文件夹上传（保持目录结构）、目录树下载（File System Access API）、粘贴上传、面包屑拖拽附件、多选一次性拖拽附件、缩略图与项目隔离、**引用/附件的去向按意图分**（评论类默认直投当前活跃会话、长按浮条按钮才弹目标会话选择，无活跃会话或投递失败回退选择器；选择器支持只添加 / 添加并打开 / 新建，含跨会话引用） |
| [会话导航与分叉](features/session-navigation.md) | 对话索引（用户 + 助手交错时间线，助手行带阅读摘要、角色图标、单行化 + 连贯时间轴；含搜索框即时过滤 + 命中高亮）、跨分页定位、Ctrl+Up/Down 跳转消息、重命名支持 AI 自动生成标题、从指定消息创建对话分支（含 beforeMessageId、可选 Agent）、分叉上下文字符预算压缩（优先保留全部用户消息 + 助手条目填充剩余额度）、**滑动 / 快捷键切换会话按列表顺序**（右滑与 `Ctrl+→` 都是「下一个」，与手势直觉一致） |
| [会话分享](features/session-share.md) | 选中消息的公开只读链接（capability token，无记录 404、重新生成即旋转且**沿用已冻结的选择**）、**冻结的是选择不是内容**（只存消息 id，正文每次访问实时装配，装配须内联工具输入输出与思考文本、不经过 `ContentBlock` 往返）、超限按最长工具输出裁剪（16 MiB / 4 KiB 下限 + `truncated` 标记）、分享页展示会话总耗时并**默认显示摘要**、导出装配结果 JSON、自定义头像与成员名册**实时解析**（含群成员名册 `groupMembers` + 顶栏头像堆叠）、目录导航（宽屏导航栏 / 窄屏抽屉，跳转底色脉冲）、不连续选择之间的缺口分割线、**注入机器可读的发现信息**（`rel=alternate` + JSON-LD + `<noscript>`，token 只校验不放行、标题逐上下文转义，HEAD 放行）、折叠思考块不预渲染 Markdown、管理列表按项目隔离、入口常驻、归档保留分享（可撤销、不可打开）、硬删除与保留期清理同事务撤销、只读渲染复用聊天管线（`readOnly` 须覆盖每个动作按钮）、**链接存在后对话框转只读**（`selectedIds` 驱动）、状态指示放在 `⋮` 菜单入口 |
| [快捷操作](features/quick-actions.md) | 聊天 Quick Send、终端 Quick Commands、CRUD 与排序 |
| [RAG 检索](features/rag.md) | 文档分块（含 chunk_overlap 配置）、向量化（可独立开关）、SQLite vec0 向量索引、混合检索（含 search_mode 配置）、三级索引重建（向量重建 + 全量重建 + 独立 FTS 重建）、可配置批次大小（`rag.batch_size`）、索引磁盘占用展示、会话聚合搜索（内容 + 会话名称双通道，名称命中置顶 + `title_match_positions` 高亮，RAG 未配置时仍可按名称搜）、**群聊会话类型筛选与发言人归属**（三类互斥 + `rag_chunks.agent_id` 透传）、消息聚类分析、索引进度跟踪 |
<<<<<<< HEAD
| [推送通知](features/push-notifications.md) | WebSocket 实时推送、通知音效开关（防止蓝牙耳机中断）、应用内完成卡片开关、桌面端系统通知开关（与 `push_mode` 解耦，覆盖会话/任务/仓库事件）、订阅中途补齐问题气泡（按会话判定的存储闸门 + user_message 先于 stream_start 补发）、权限待审推送、离线事件持久化与游标拉取、已读门控（suppress_notification + 游标推进与通知解耦）、已读后主动消除原生通知（WS-only read 事件 + Android/Electron dismiss）、钉钉/飞书/微信推送（Stream API / WebSocket / iLink 长轮询；Markdown 单聊 / 交互式卡片 / 纯文本 + 会话交互命令；微信走扫码登录且 `context_token` 持久化） |
=======
| [推送通知](features/push-notifications.md) | WebSocket 实时推送、通知音效开关（防止蓝牙耳机中断）、应用内完成卡片开关、桌面端系统通知开关（与 `push_mode` 解耦，覆盖会话/任务/仓库事件）、订阅中途补齐问题气泡（按会话判定的存储闸门 + user_message 先于 stream_start 补发）、权限待审推送、离线事件持久化与游标拉取、已读门控（suppress_notification + 游标推进与通知解耦）、已读后主动消除原生通知（WS-only read 事件 + Android/Electron dismiss）、钉钉/飞书企业机器人推送（Stream API + 交互式卡片/Markdown 单聊 + 会话交互命令 `/ls` `/show` `/stop` `/new`，命令与附件互斥、短 ID 形状校验、`/new` 序号稳定） |
>>>>>>> 7416f94cf
| [应用内完成通知](features/completion-popup.md) | 后台事件时滑入纯通知卡片：头部区（图标 + 主类别徽章 + 事件类型纯文字标题 + 关闭）+ 正文区（标题段 + 最多 4 行摘要）+ 跨项目整卡换色与区隔带 + 跳转（顺带标记已读），5 秒自动关闭；事件覆盖与系统通知完全对齐（会话/任务/仓库），队列上限 3 + 同仓库合并 |
| [智能体用量统计](features/usage-stats.md) | 按项目聚合 `chat_metadata` 用量行（独立台账，不随会话删除丢失）的数据统计：用量总览环形图 + 缓存命中下钻、按指标拆分的图表（bar/pie/trend 可切换）、24h/7d/30d/自定义时间窗与 model/backend/agent 筛选、**AI 工作时长**（`wall_ms` 合计，取消回合也计，升级时 `MigrateLedgerWallClock` 回填历史；按量级显示为一位小数的 毫秒/秒/分钟/小时/天）、费用两位小数统一、移动端纵向堆叠；`/cb-usage` 支持 `scope=all` 跨项目全量统计（仅本机 AI 令牌，附 project 维度）；数据统计页签另含代码存量/代码增量双子页（见 [Git 管理](features/git-management.md)） |
| [用量提醒](features/usage-meter.md) | 一条全局规则（**独立启用开关**；起始时间 / 周期 / 额度**全部可选**，按已配置内容做最大程度展示，缺额度只统计不设预算、缺周期则永不滚动；周期**固定天数或自然月**二选一：自然月按起始时刻的日号每月滚动、分母取当月实际天数、小月钳到月末且不漂移），按**线性匀速**算出「此刻最多该用多少」并与实际对比；header 胶囊带基准线刻度、浮层展示实际/应花/额度三条同轴对比与累计曲线（含用尽预测）、移动端并入资源面板末尾；越线与用尽**各提醒一次**（通知中心 + 原生 + IM），**只提醒不拦截**；触发状态随周期起点自动重新武装 |
| [MCP 服务](features/mcp.md) | 全局 MCP 定义（`config.yaml` 的 `mcp.servers`，stdio/http/sse）+ 按智能体启用策略（all/none/selected）；三层过滤（策略 → 后端黑名单 dsh/deepseek/pi → 能力位 http/sse）；四条建会话路径都注入、每轮前比对策略哈希决定是否重连；配置页带真实握手测试；`/cb-mcp` 支持对话内配置 |
| [系统资源监控](features/system-resources.md) | gopsutil 采集 CPU/内存/磁盘/网络/负载、500ms 采样缓存、WS 按订阅需求推送（`metrics_preference` 声明速率）、可见性感知、WS 断线时显示连接状态 |

### infra/ — 基础设施

| 模块 | 说明 |
|------|------|
| [认证与中间件](infra/auth-and-middleware.md) | SHA-256 密码认证、本机 AI 短时令牌（12 小时 HMAC，地址+签名双条件）、隧道场景信任边界限制、按路由认证、API 密钥加密（`agent_api_keys` 已移除）、请求链（含 NoCache）、panic 恢复 |
| [国际化](infra/i18n.md) | go-i18n bundle、嵌入式 YAML 翻译、X-Locale/Cookie/Accept-Language 优先级链、推送通知独立 Localizer |
| [SSH 隧道](infra/ssh-tunnel.md) | 双传输：SSH 通道（direct-tcpip 正向 + tcpip-forward/forwarded-tcpip 反向）与 HTTP/2 流隧道（`/api/tunnel/stream` 数据面 + `/api/tunnel/control` 反向控制面，单端口 20000 多路复用，`-R` 靠控制流通知 + 单次 token 认领）、传输方式 `port_forward.transport` **已钉死为 `both`**（服务端在 `ApplyDefaults` 与 PATCH 路径均无条件归一，`ssh`/`h2` 不可达——该字段只剩 web 端健康检查门控一个消费者，两个原生客户端都不消费：Electron 写死 SSH、Android 用本地 SharedPreferences 开关 `tunnel_transport_h2_enabled`；故「服务端只准 ssh」不可执行，已移除可配置性；字段保留以免 web 读到 `undefined` 而误判 ssh-only）、半关闭与 SSH **有界近似**而非逐条等价、方向语义、反向映射仅绑 127.0.0.1 且禁绑保留端口、密码认证、自动 host key、暴力破解防护、端口白名单默认 1024-65535（ISS-186 修复）、端点按受众拆分（公开 `/api/ssh/info` 仅端口发现，`/api/ssh/info/full` 需鉴权） |
| [Proxy 注册表](infra/proxy.md) | 方向感知（forward/reverse）、反向代理与 Host 头重写（仅正向）、服务器端口分配与保留端口、特权端口映射、健康检查（反向跳过拨号）、并行端口探测（有界并发 + 扫描错误可见）、前端端口展示、CORS 代理（Swagger UI "Try it out"） |
| [配置与自动发现](infra/config-and-discovery.md) | 零配置启动、DB-backed Agent 存储、双传输选择、供应商注册表、Model 自动发现（启动同步加载 + 后台探测广播 `agents_updated`；含 Kimi 与 Codex 自定义模型发现函数）、ACP 运行时模型验证、**用户自定义 ACP 智能体**（`#557`：`backend=custom-acp` 刻意不入注册表，否则 `rescan` 会重写用户填的命令；命令按空格拆分、≤4096 字节；`acp_command` 仅「无 spec」的 agent 可改，前端用服务端 `backendHasSpec` 判定只读）、**智能体自检两级**（配置自检不 spawn 不计费 / 深度自检真跑一轮计费，分两个路由以免误触付费）、**智能体生命周期管理**（会话/任务计数 + 禁用开关 + 删除守卫，会话数含活跃群成员关系、排除任务执行会话）、**「仅当前项目」作用域**（`project_id` 只拦新建入口不追溯历史；默认智能体不可被限定、项目级智能体不可为默认）、**拖拽排序**（`PUT /api/agents/reorder` + `model.SortAgentsByOrder` 唯一排序规则）、**「修改路径」**（`PATCH /api/projects/detail?id=`：只改登记不动磁盘，拒绝占用/不存在/运行中，改当前项目时重写 cookie）、多实例 Cookie 隔离、TLS 证书自动发现、Schema 迁移（列探针 + `schema_migrations` 数据迁移台账）、项目 id 化注册表（项目路径→`project_id`，改名即一条 UPDATE）+ 项目注册表管理面（列表/详情/删除数据，锁序与缓存失效陷阱）、默认项目持久化、配置连通性测试、覆盖率门禁 |
| [事件体系](infra/event-system.md) | ws.Manager 系统广播、StreamHub 会话扇出、断线缓冲重放、遥测非缓冲投递（`system_resources`）、投递丢弃计数（`/api/ws/delivery-stats`）与关键事件可靠投递、摘要与权限事件推送 |
| [应用自升级](infra/self-upgrade.md) | 版本检查、安装目录可写预检、镜像 tarball URL 归一化、备份替换、进度推送、服务重启与断线轮询、容器内强制就地替换 |
| [版本号策略](infra/versioning.md) | versionCode（`major*1e8+minor*1e5+patch*1e3+distance`，决定 Android 能否覆盖安装）与 versionName（仅展示）两套口径；CI 走 `--tag-only` 只拉 tag ref 不拉历史，本地走 `git describe` 含 distance；位宽防 `v0.100.0`/`v1.0.0` 撞码；release 带 `--assert` 防退化；桌面载荷只经 npm 分发 + npm 发布走 OIDC Trusted Publishing |
| [本地文件服务](infra/local-file-serving.md) | `/api/fs/raw/` 路径编码、媒体预览、下载与访问边界、目录树列表、批量文件存在检查、批量图片 Base64 |
| [Docker 部署](infra/docker-deployment.md) | 单阶段运行时镜像、数据卷持久化、GHCR 双架构发布、容器内升级提示镜像优先 |
| [系统资源监控](infra/system-resources.md) | CPU/内存/磁盘/磁盘 I/O/网络/系统负载实时采集、gopsutil 采样、500ms 缓存、MetricsPusher 按订阅需求推送（非缓冲投递）、前台/后台双速、AppHeader 压力指示图标、WS 断线状态展示、Gauge 弹出面板 |
| [CLI 子命令](infra/cli-reference.md) | 仅剩 upgrade-replace（应用自升级内部机制）；业务子命令 task/rag 已移除，改由内置斜杠命令直调 HTTP API |
| [Bugfix 工作流](infra/bugfix-workflow.md) | 自动化 bugfix 生命周期：扫描分类→worktree 隔离修复→测试验证→PR+CI→合并关闭 |

### api/ — API 规格

| 模块 | 说明 |
|------|------|
| [OpenAPI 规格](../../internal/api/openapi.yaml) | 完整 OpenAPI 3.0 单文件（187 路径 / 240 操作）：所有 HTTP 端点、鉴权标注、统一错误体、请求/响应 schema；WebSocket 与 SSE 端点以说明形式收录。源文件已迁至 `internal/api/openapi.yaml` 以支持 `go:embed`（详见 [API 文档说明](api/README.md)） |

### client/ — 客户端

| 模块 | 说明 |
|------|------|
| [前端架构](client/frontend-architecture.md) | 单页布局、reactive store、composable 模式、统一 WebSocket 单通道 + 单标签页守卫（Web Locks 主 + BroadcastChannel 回退，第二标签页不挂载应用）、聊天渲染管线（useChatRender + useMarkdownRenderer + 数学块提取保护 + KaTeX 内部 svg 不被媒体块劫持 + 合并 parse 往返）、静态 Block 缓存按 projectRoot/locale 分键、交互式提问卡（AskUserQuestion 选项卡片 + 单选可取消 + 答案状态跨重渲染持久化 + 解析器前后端镜像）、ACP 会话管理（含 context_state 持久化恢复）、标注管道（文件路径 + localhost URL + commit hash + Worktree）、thinking 惰性加载（useThinkingContent）、CodeMirror 代码编辑器（浏览/编辑双模式 + sticky scroll）、Excalidraw 画布编辑（iframe 内嵌独立构建 + postMessage）、会话重置、终端三模式手势 + 选择模式、终端帮助抽屉、统一搜索控件（SearchBar + CodeMirror 搜索面板 + Markdown 内嵌搜索条）、Read 工具行范围展示、流式渲染帧调度（StreamFrameScheduler）+ 只重渲源文本变化过的块（跳过子智能体块 + 增量复用缓存）+ 渲染热路径记忆化（工具图标预建 Map / 去无谓 cancelAnimationFrame / shallowRef Map 块缓存）、滚动监听显式 passive（双向契约守护）、路径标注重校验（缓存命中与 thinking 两条死链面，onMounted + onUpdated 双钩）、滚动帧内批量读几何（禁用 content-visibility：与列表 key 重挂载叠加会致发消息滚动跳变）、前台恢复自包含重连、appLog 强制日志规范、历史加载 DB 权威重建（陈旧快照下按"会话在跑 + 快照无 streaming 行"保留活跃占位符）、代码链接预览（useCodeLinkPreview 单例状态机 + CodeLinkPreview 浮层/底部抽屉，详见前端架构规格）、FileHeader 三层弹性布局、键盘交互（DialogOverlay/BottomSheet Esc/Enter）、系统资源面板（WS 按需推送）、左侧面板按项目记忆（useProjectPanel）、统一返回状态机、文件/Agent/Provider 图标、智能体禁用过滤（`selectableAgents`，统一覆盖所有新建入口）、自定义智能体头像（DiceBear 40 风格懒加载 + 沙箱 `<img>` 渲染）、会话搜索抽屉（含会话类型筛选）、最近项目按 git 仓库分组、WS 断线连接状态、消息聚类抽屉、LocalLinkGuard 全局链接拦截 + 外链统一为可选中文本 + 点击确认（三条渲染路径共用同一节点，data-href 双向校验）、F5 刷新让位于页面内用途、文本选择感知、消息排队与 needs_start 重提交、文件刷新与差异高亮、Diff 前后导航、快捷键提示系统（shortcutTips）、会话身份管理（useSessionIdentity）、文件上传管理（useFileUpload）、异步组件重试（useAsyncComponent + 陈旧构建 chunk 失败自愈：`vite:preloadError` 监听 + 成串失败才 reload）、内联缩放（点击进缩放态，滚轮围绕光标、拖拽平移，仅指针设备）、36 命名主题系统（data-theme 机制 + 快捷选择器 + 外观深链 + logo 深链「关于」 + 实时跟随系统 + 跨主题强调色装饰按表面分形 + 切换圆形扩散动效）、自定义壁纸背景（半透明层透出 + 文件页"设置为主题背景"入口）、自定义字体双通道（代码/界面 + 备选）、TOC 停靠栏左右侧切换、宽屏聊天区切换、Dock 数据统计页签（StatsTabHost 三子页：用量/代码存量/代码增量）、统一刷新按钮（RefreshButton）、项目切换并发请求合并（coalescedJson）、会话列表扁平化、Git 历史视图共享逻辑（useGitHistoryView）、Android 输入恢复（useSelectAllDeleteRecovery）、计数角标统一（.count-badge） |
| [统一返回与跨界面导航](client/unified-back-navigation.md) | 两级分层栈（界面内文件历史 useFileNavStack + 跨界面 jump origin useNavigationContext）、useNavigationStateMachine 确定性优先级裁决、覆盖层关闭顺序等于层叠顺序（BottomSheet 按实例创建序自注册、上层优先）、useNavigationCoordinator 全局文件打开唯一入口、目录游历事务 useDirectoryReturn、滚动/阅读位置精准还原 useFileScrollRestore、双击退出协议、多端返回入口（文件顶栏导航簇 / 右缘手势 / Android 物理键） |
| [视觉设计指导手册](client/design-guide.md) | 改 UI 前必读：样式三层归属（`web/css/` 全局 → `src/assets/` 全局原语 → SFC scoped）、设计 token（7 档字号 / 间距 / 圆角 / 命名 z-index / 时长 / 透明度，含"为何是 px 非 rem"）、36 命名主题机制与新增主题改动清单、布局骨架（宽屏阈值 960px 纯 CSS 视口、安全区、壁纸层与 z-index 提升）、组件约定（`.fbtn` / `.count-badge` / `.chat-message` / `PopupMenu` / lucide 图标 14px 默认）、动效 token 与 keyframes 复用；**七条红线**（v-html 匹配不到 scoped、共享类基规则也必须全局、app-region 豁免只能是控件、对比度不能靠固定跳一档背景、content-visibility 滚动跳变、Android WebView 像素怪癖（**合成分层只有一个分界且不在会变高的元素上**：分界在尺寸稳定的聊天面板，子树内禁止 `will-change`/`backdrop-filter`）、`scrollbar-color` 弃用 `::-webkit-scrollbar-*` 致箭头复活）；含改动检查清单与 CSS 守卫测试索引 |
| [Android 集成](client/android-integration.md) | JS Bridge（25+ 方法）、12 个 Java 类模块（BackgroundService / PendingEventsWorker / FloatingStatusView 悬浮状态窗 + 会话面板 / LiveUpdateManager 实时更新等）、Android 全量国际化、APK 嵌入（`build.sh --android` → `go:embed` → `/api/apk`）、AppLog 兼容日志端点、推送感知生命周期、版本不匹配 Overlay、硬件返回键同步委托（evaluateJavascript 读 `__clawbenchBackHandled` + 双击退出） |
| [多服务器管理](client/multi-server.md) | 服务器列表、凭据按服务端分别保存、登录页选择、**应用内切换对话框**（资源面板末尾入口，列出全部已保存服务器并标出当前那台，缺密码就地补；退出登录与切换共用同一对话框）、认证失败分类提示 |
| [客户端安装与 App 模式](client/install-and-app-mode.md) | PWA 安装（零缓存 Service Worker + 稳定 manifest URL）、iOS 手动安装、APK 下载与原生模式识别 |
| [桌面端客户端](client/desktop-client.md) | Electron 壳：窗口与链接策略（Windows/Linux 无边框 + 顶栏自绘窗口控制，macOS 保留原生帧）、登录/启动加载屏（对齐 Android splash）、原生上下文菜单、最小化仍可投递的系统通知（`isDesktopApp` 与 Android 省电策略分野）+ 已读消除原生通知、多服务器凭据（每服务端各存各的密码）、界面「自动适配」按钮（一次计算写入滑块）与原生 `Ctrl+滚轮` 缩放、SSH 端口映射（desired 状态 + 单飞 + 保活与断线重连重建）、主进程 HTTP 调用自带 jar Cookie（下载/分享修复）、应用级快捷键走 `before-input-event`（F5 放行给页面）、侧装+指针自升级（应用自读指针，坏指针自愈）+ 增量载荷包（复用 Electron 运行时，主版本/壳指纹双重门控）、GitHub Release 唯一分发渠道 |

## 参考资料

`docs/dev/` 存放各 AI 后端的工具定义、调用样例与协议逆向分析，供接入/排查时查阅。

| 文档 | 说明 |
|------|------|
| [CodeBuddy ACP 非标准扩展接口清单](../dev/codebuddy_acp_extensions.md) | `codebuddy --acp` 在 ACP v1 之外的扩展面：17 个 `extMethod` 请求、19 个自定义 `session/*` 方法（steer / 消息队列 / inject_history / 回滚 / multitask）、10 个 agent→client 扩展通知、7 类非标准 `sessionUpdate`、163 个 `codebuddy.ai/*` meta key、`initialize` 双向能力协商与各接口门控条件；含**真机探针实测结论**（`session/steer` 可用、裸 JSON-RPC 通道可行、`_codebuddy.ai/question` 在 stdio 下不触发） |
| [Claude ACP 扩展面（与 CodeBuddy 对比）](../dev/claude_acp_extensions.md) | `claude-agent-acp` 是桥接 Claude Agent SDK 的适配器，**无 `extMethod` 兜底**：无 steer / 队列 / inject_history 等私有方法，`sessionUpdate` 全为标准类型，扩展仅靠 `_meta`（`claudeCode` / `_claude/origin` / `terminal_*`）；但有内建 prompt 排队（`promptQueueing`，并发调标准 `session/prompt`）——注意 ClawBench 现有 `RegisterSession` 覆盖写不支持并发 |

## 核心技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.25+、SQLite（WAL + vec0 向量索引）、robfig/cron、gotreesitter（符号提取）、gopsutil（系统资源）、go-i18n/v2（国际化） |
| 前端 | Vue 3 + TypeScript、Vite、CodeMirror（代码浏览+编辑）、xterm.js、marked + hljs（选择性语言注册）、KaTeX（字符串级渲染）、vue-draggable-plus |
| AI 集成 | Shell-out 到 CLI 工具、ACP JSON-RPC over stdio、stream-json 解析 |
| 实时通信 | WebSocket `/api/ai/events/ws`（统一推送：聊天 + 系统事件 + 摘要 + 推荐待审 + 权限待审 + replay_done + cluster_progress，`StreamHub` 会话级扇出）、旁注小通道（`/api/file/watch/ws`、`/api/tts/audio/ws`、`/api/stt/transcribe/ws` WS；`/api/dir/search` SSE）、SSH（端口映射） |
| 安全 | SHA-256 密码存储、AES-256-GCM API 密钥加密（`agent_api_keys` 已移除）、HKDF-SHA256 密钥派生 |
| 移动端 | Android WebView、原生后台服务 |
