# 摘要管线

ClawBench 的摘要系统将 AI 助手的长回复压缩为简短摘要，服务于两种不同场景：TTS 语音播放（需要纯文本、无格式、口语化）和阅读摘要（需要保留 Markdown 格式和代码片段）。两条管线独立配置、独立缓存，互不影响。理解摘要系统是理解聊天完成后的后续处理链和 TTS 语音合成的关键。摘要结果携带结构化卡片元数据（SummaryCards），包含工具卡片、定时任务 ID 和 ask-question 卡片，前端据此渲染摘要视图。

## 流程图

### 摘要管线主流程

```mermaid
flowchart TD
    A[聊天/任务完成] --> B{summarizeTarget 调度}
    B -->|disabled| Z[不生成摘要]
    B -->|simple| C[summarizeSimple: ExtractLastAnswerFromBlocks]
    C --> D[直接保存为摘要 + SummaryCards]
    B -->|ai| E[提取结论文本: ExtractLastAnswerFromBlocks]
    E --> F{文本 < 300 字符?}
    F -->|是| G[保存空摘要<br/>前端显示原文]
    F -->|否| H[AI 摘要调用]
    H --> I{结果 > 4KB?}
    I -->|否| J[保存摘要 + SummaryCards]
    I -->|是| K[二次摘要 Pass 2]
    K --> J
    K -->|失败| L[使用 Pass 1 结果]
    L --> J
    H -->|失败| M[使用已提取结论文本]
    M --> J
```

### TTS 摘要流程

```mermaid
flowchart LR
    A[TTS 请求] --> B{有 messageId?}
    B -->|否| C[使用前端传入文本]
    B -->|是| D[加载消息 + ExtractLastAnswer]
    D --> E[提取 AskUserQuestion 块]
    E --> F[合并结论+问题文本]
    C --> G{tts_summaries 缓存?}
    F --> G
    G -->|命中| H[跳过摘要]
    G -->|未命中| I[TTS 摘要管线<br/>PreserveMarkdown=false]
    I --> J[StripMarkdown 清理]
    J --> K[语音合成]
```

## 功能与设计要点

### 功能清单

- **聊天自动摘要**：聊天会话正常完成时自动生成摘要（取消/断线的会话不触发）。`summarizeTarget` 统一调度入口根据 `chatSummaryMode` 配置选择摘要策略：`simple` 模式直接提取最后回答（`summarizeSimple`），`ai` 模式异步调用摘要后端，`disabled` 不生成摘要。完成后通过 `summary_update` WS 事件推送前端（含 SummaryCards）
- **任务执行摘要**：定时任务执行完成后生成摘要，与聊天摘要共享 `summarizeTarget` 调度入口和存储模型，续接对话时无需类型转换
- **SummaryCards 结构化卡片**：摘要结果携带结构化卡片元数据（`SummaryCards`），持久化到 `summaries.summary_cards` 列。包含三类卡片：工具卡片（`SummaryTool`，记录工具名称和输入摘要）、定时任务 ID（关联执行记录）、ask-question 卡片（`AskQuestionCard`，含标题和选项）。前端据此在摘要视图中渲染工具调用摘要和交互选项，无需加载完整消息内容
- **TTS 语音摘要**：TTS 请求触发时按需生成语音专用摘要。提取 AI 结论和 AskUserQuestion 内容，合并为可朗读文本。结果缓存到独立的 `tts_summaries` 表
- **多 pass 压缩**：AI 摘要结果超过 4KB 时自动触发二次摘要，最多两轮。防止超长中间结果传递给下游（尤其是 TTS）
- **Block 提取算法**：`ExtractLastAnswerFromBlocks` 跳过中间推理，提取最后一个 tool_use 之后的文本作为 AI 结论。无后续文本时回退到最长的文本块——AI Agent 的对话模式通常在工具调用后给出最终综合回答
- **Markdown 清理**：TTS 模式的 `StripMarkdown` 多阶段清理：代码块移除、行内代码按长度保留或删除（短变量名保留，长代码片段移除）、粗体/标题/列表/表格/脚注剥离、AskUserQuestion 块转为自然语言朗读格式
- **热重载**：摘要配置通过 PATCH 端点即时生效，TTS 和 Task 摘要器原子重建，进行中的调用继续使用旧实例

### 设计要点

- **双管线分离**：TTS 摘要（纯文本、激进压缩）和阅读摘要（保留 Markdown 和代码）使用独立配置、独立实例和独立缓存。TTS 摘要器故障不影响阅读摘要，反之亦然
- **summarizeTarget 统一调度**：聊天和定时任务共享 `summarizeTarget` 调度入口，根据 `chatSummaryMode` 配置选择摘要策略。之前定时任务绕过 `chatSummaryMode` 直接走 AI 路径，导致短任务输出被存为空摘要——统一调度后所有场景都尊重用户配置
- **短文本绕过**：300 字符以下的文本不调用 AI——TTS 模式清理后直接返回，阅读模式保存空摘要（前端显示原文）。避免无意义的 API 调用和延迟
- **降级使用结论文本**：AI 摘要失败时，`ExtractLastAnswerFromBlocks` 已经提取了最后实质性回答，直接使用该文本作为摘要——比截断更保留语义完整性
- **降级链**：每条摘要路径都有降级——AI 摘要失败时直接使用已提取的结论文本（`ExtractLastAnswerFromBlocks` 的结果），无需二次截断；二次摘要失败使用一次结果。系统不会因为摘要失败而丢失可用内容
- **AskUserQuestion 保留**：TTS 清理时专门解析结构化问题块，转为自然语言（"问题：...选项：A, B, C"），确保 TTS 能朗读权限审批的问题和选项
