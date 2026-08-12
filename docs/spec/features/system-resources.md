# 系统资源监控

系统资源监控让用户实时了解服务器状态——CPU 使用率、内存占用、磁盘空间、网络吞吐和负载。移动端场景下用户无法登录服务器执行 `top` 或 `htop`，Web 面板提供了等价的可视化体验。WS 断线时资源面板自动隐藏，改为显示连接状态提示，避免用户误以为数据不刷新是服务端问题。

## 流程图

### 系统资源数据流

```mermaid
sequenceDiagram
    participant sampler
    participant handler
    participant 前端
    participant WS

    sampler->>sampler: 500ms 采样缓存<br/>CPU/网络需要间隔计算
    前端->>handler: GET /api/system/resources
    handler->>sampler: GetResources()
    sampler-->>handler: ResourceResponse
    handler-->>前端: JSON (Cache-Control: no-store)
    前端->>前端: useSystemResources 1s 轮询<br/>页面可见时轮询，隐藏时暂停

    Note over WS: WS 断线
    WS-->>前端: disconnected
    前端->>前端: 隐藏资源面板，显示连接状态
```

## 功能与设计要点

### 功能清单

- **实时资源面板**：`SystemResourcesPanel` 在 AppHeader 的 Gauge 图标弹出菜单中展示 CPU、内存、磁盘、网络和负载指标。用户无需离开聊天界面即可了解服务端负载
- **压力指示图标**：AppHeader 的 Server 图标在系统资源压力异常时（CPU/内存超过阈值）切换为压力指示图标，即使资源面板未打开也能提醒用户关注资源状态
- **6 类指标**：CPU 使用率+核心数、内存使用率（排除 buffers/cache）、磁盘使用率（数据目录所在分区）、磁盘 I/O 速率、网络上下行速率、1/5/15 分钟负载均值。覆盖了运维关注的核心指标
- **WS 断线状态提示**：WebSocket 断开或重连时，资源面板自动隐藏并改为显示连接状态指示器（"disconnected" / "reconnecting"）。避免展示过时数据误导用户
- **页面可见性感知**：`useSystemResources` 在页面可见时 1s 间隔轮询，隐藏时暂停——移动端切到后台时停止请求，回到前台时恢复
- **前台/后台双速轮询**：`startPolling`（前台，1s 间隔）和 `startBackgroundPolling`（后台，5s 间隔）双模式。前台消费者（如资源面板）激活时使用快速轮询，仅后台消费者（如压力指示器）时使用慢速轮询，按需切换节省请求
- **双次初始采集**：CPU 使用率和网络吞吐率需要间隔采样（两次采集才能计算速率），composable 启动时立即发起两次请求，第二次请求返回有效数据。首次加载不会显示零值

### 设计要点

- **采样缓存而非实时采集**：`GetResources()` 使用 500ms 缓存，同一窗口内的多个请求共享同一份采样结果——gopsutil 的 CPU/网络采集需要间隔计算，每次请求都重新采集会产生零值或不准确数据
- **认证端点**：`GET /api/system/resources` 需认证。当前面向单用户场景可接受；多租户场景下应限制为管理员
