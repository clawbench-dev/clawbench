# 事件体系

ClawBench 的事件体系是系统实时性的基础设施——会话状态变更、任务更新、权限待审等事件从后端产生，经 EventBus 到达 WebSocket Manager，再推送给在线客户端；离线客户端则由 JPush 兜底。这套体系保证所有客户端在任何网络条件下都能收到关键状态变更通知。

## 流程图

### 事件从产生到推送

```mermaid
sequenceDiagram
    participant 事件源
    participant EventBus
    participant ws.Manager
    participant 在线客户端
    participant JPush
    participant 离线客户端

    事件源->>EventBus: 发布事件
    EventBus->>ws.Manager: BroadcastEvent()
    ws.Manager->>在线客户端: WS 推送
    在线客户端-->>ws.Manager: ack
    alt WS 断开 + JPush 可用
        ws.Manager->>JPush: 发送推送通知
        JPush-->>离线客户端: 通知栏显示
    else WS 断开 + JPush 不可用
        ws.Manager->>ws.Manager: 缓冲事件
        Note over ws.Manager: 等待重连后回放
    end
```

### 客户端生命周期

```mermaid
flowchart TD
    A[客户端连接 WS] --> B[分配 Subscription]
    B --> C[接收实时事件]
    C --> D{连接断开?}
    D -->|是| E{有 push 注册?}
    E -->|是| F[JPush 通知]
    E -->|否| G[缓冲事件 10s]
    D -->|否| C
    F --> H[10 天无活动后清理]
    G --> I[120s 无活动后清理]
    A --> J[重连]
    J --> K[回放缓冲事件]
    K --> C
```

## 功能与设计要点

### 功能清单

- **WebSocket 事件通道**：`/api/ai/events/ws` 提供 3 种事件类型（session_update、task_update、summary_update），其中 `session_update` 的 status 字段区分 running、completed、cancelled、permission_pending、permission_resolved 等状态，客户端实时感知系统状态变化
- **JPush 推送后备**：WS 断开且 JPush 可用时，事件转为推送通知。保证离线客户端也能收到关键通知（如任务完成）
- **断线缓冲与回放**：WS 断线后缓冲 10s 内的事件（最多 50 条），重连后自动回放。配合推送确保不丢失关键通知
- **Push Registration ID 管理**：客户端通过 WS `register` 消息上报 JPush Registration ID，绑定到登录级别。WS 重连后不需要重新注册
- **摘要推送**：`summary_update` 事件在聊天或任务摘要生成后实时推送，前端 `SummaryToggle` 组件可立即切换显示摘要，无需轮询
- **心跳保活**：WS 30s ping/pong，5min 空闲超时。防止半开连接占用资源
- **客户端容量限制**：最多 20 个 WS 订阅，防止单个服务端过载

### 设计要点

- **WS 替代了 SSE 系统事件**：系统事件最初使用 SSE（`/api/events`），后来迁移到 WebSocket。WS 的双向通信能力更适合 ack 确认和 register 注册场景，也减少了连接数（一个 WS 复用多种事件类型）
- **推送感知策略是自动的**：WS 断开 + push 可用 → JPush 推送；WS 断开 + push 不可用 → 缓冲等待重连。不需要客户端决定走哪个通道，服务端根据 push 注册状态自动选择
- **断线清理有两组超时**：无 push 注册的客户端 120s 后清理（可能只是网络抖动），有 push 注册的客户端 10 天后清理（JPush 可以长期投递）——两种客户端的"活跃"期望不同
- **ack 机制用于确认而非可靠投递**：客户端发送 ack 表示已收到事件，但不触发重发。事件缓冲是时间窗口而非确认驱动——简化了服务端逻辑
