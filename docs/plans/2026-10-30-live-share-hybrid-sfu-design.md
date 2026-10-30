# 直播共享的融合方案：小会 P2P + 大会 SFU

日期：2026-10-30
前置：[桌面屏幕与声音共享](2026-10-28-desktop-screen-audio-share-design.md)、[AI 听记](2026-10-30-live-share-ai-notes-design.md)

## 目标

一套产品同时覆盖两种规模，不维护两套 UI、不要求用户理解「拓扑」：

| 场景 | 规模 | 特征 | 传输 |
|---|---|---|---|
| 小会 | ≤8 | 所有人可能都要说话，低延迟，临时开 | **P2P 星形**（现状） |
| 培训 | ≤100 | 一个主讲，观众以听为主，少数人举手连麦 | **SFU 转发** |

**非目标**：不做客户端互助中继（P2P CDN）。它省的是带宽，代价是不可控的锚点、关键帧要不到、后台标签页被节流——局域网场景里带宽不是瓶颈，可预测性才是。

## 为什么不干脆全用 SFU

SFU 也能服务 5 个人。保留 P2P 的理由不是性能，是**依赖面**：

- P2P 模式：不需要媒体服务器、不需要开 UDP 端口、不需要多一个进程、不需要额外运维。分享链接发出去就能用。
- 对「临时拉三个人看个屏幕」这种最高频的用法，这是决定性的。

所以：**P2P 是默认，SFU 是按需升级**。

## 为什么不在会议中途自动切换

拓扑切换意味着所有连接重建（ICE、DTLS、关键帧），会有 3–8 秒黑屏，且所有人同时重连。

因此模式在**创建共享时选定**，中途不变。人数接近上限时提示「需要更多人请结束并重建为培训模式」——这是诚实的做法，比偷偷降级好。

## 整体结构

```
                    ┌──────────────────────────────┐
   小会（≤8）        │  ClawBench 服务端             │
   发布端 ◀──WS──▶  │  · 房间/权限/状态（内存）      │
     │  ╲           │  · 信令中转（SDP/ICE）         │
     │   ╲ P2P      │  · 听记落库 + 纪要             │
     ▼    ▼         │  · 分享页 / 管理接口            │
   观众  观众        └──────────────┬───────────────┘
                                    │ 签发 token / webhook
                    ┌───────────────▼───────────────┐
   培训（≤100）      │  LiveKit（SFU，独立进程）      │
   发布端 ──1 路上行─▶│  · 媒体转发 / simulcast        │
   观众  ◀──N 路下行──│  · 关键帧、NACK、带宽估计       │
                    └───────────────────────────────┘
```

服务端在 P2P 模式下**不碰媒体**；在 SFU 模式下**仍然不碰媒体**——媒体在 LiveKit，ClawBench 只做控制面（签发 token、管房间、收 webhook）。这条边界很重要：它让两种模式的业务代码（听记、权限、分享页）几乎不用分叉。

## 前端：一个接口，两个实现

融合的关键不是后端，是让 UI 完全不知道底层是哪种拓扑。

```ts
// web/src/share/transport.ts
export interface LiveTransport {
  /** 加入房间；成功后 onState('playing') */
  connect(token: string, opts: TransportOptions): Promise<void>
  /** 发布自己的屏幕 / 麦克风（没有就传 null 停止） */
  setScreen(track: MediaStreamTrack | null): Promise<void>
  setMic(track: MediaStreamTrack | null): Promise<void>
  /** 订阅某一路；返回的流交给 <video> */
  subscribe(source: SubscribeSource): MediaStream | null
  on(event: 'state' | 'screen' | 'talk' | 'notes' | 'error', cb: (p: any) => void): void
  disconnect(): void
}
```

- `P2PTransport`：把现有 `useLiveShare`（发布端）和 `LiveViewer`（观众端）包一层。
- `SFUTransport`：包 `livekit-client`。
- 房间元数据里带 `mode: 'p2p' | 'sfu'`，`/api/share/{token}/meta` 返回它，分享页据此选实现。

**UI 不变的部分**：分享页、dock、听记面板、屏幕共享按钮、状态覆盖层、权限开关——全部复用。变的只有「媒体怎么到对端」。

## 角色模型

两种模式共用一套角色，P2P 只用到前两个：

| 角色 | 小会 | 培训 | 能力 |
|---|---|---|---|
| `host` | ✓ | ✓ | 发布屏幕/声音/麦克风、管权限、结束共享、转让主讲 |
| `guest` | — | ✓ | 发布麦克风（+ 可选屏幕），被 host 拉上台的人 |
| `viewer` | ✓ | ✓ | 观看；可举手；被批准后升级为 guest |

培训模式的发言链路：**观众举手 → host 批准 → 服务端改角色 → SFU 允许发布 → 该观众开麦**。
P2P 模式沿用现状（房间开关 + 各自开关），因为 8 个人不需要舞台管理。

## 媒体组织

### P2P（现状，不改）

- 发布端 → 每个观众一条 PeerConnection，三条 m-line（音频 / 画面+上行 / 转发画面）
- 每个观众一份定制混音（排除自己）
- 成员共享屏幕：观众 → 发布端 → 转发给其余人

### SFU

- host 发布：`screen` + `screenAudio` + `mic` 三个 track（不混音）
- guest 发布：`mic`（+ 可选 `screen`）
- 观众订阅：
  - 必订：host 的 `screen` / `screenAudio` / `mic`
  - 按需：当前发言 guest 的 `mic`（服务端标记「谁在台上」，客户端只订这几路）
  - **不订自己**（回声问题由订阅关系解决，不再需要「排除自己」的混音）
- 屏幕共享：同一时刻一人，房间元数据记 `screenSharer`，SFU 侧对应 track 只允许一个发布者
- Simulcast：host 的 `screen` 发三层（1080p / 720p / 360p），LiveKit 按每个观众的带宽选层

**发布端不再混音**：这是 SFU 模式最大的简化——现有 `LiveMixer`（每观众一条混音图 + 一路编码）整块不需要了。

## 听记

- **P2P**：现状（发布端切段上传 → STT → 落库）。
- **SFU v1**：仍走发布端上传。理由：代码已存在、已验证，且 host 本来就是桌面客户端（HTTPS 已解决）。SFU 只是让观众也能说话，而 guest 的音频在 host 端也能拿到（host 订阅了 guest 的 mic），所以仍由 host 统一切段上传即可。
- **SFU v2（可选）**：LiveKit Egress 旁路录音，服务端直接转写，host 不再上传。收益是 host 掉线也能继续记；代价是多一个 egress 进程 + 存储。

## 服务端改造

| 模块 | 改造 |
|---|---|
| 房间 | `Room` 增加 `Mode`（p2p/sfu）、`GuestIDs`；`Create` 时按请求的模式决定 |
| 签发 | 新增 `POST /api/live-share/token`：给当前参与者签发 LiveKit JWT（房间名 = share token，身份 = 角色+id，权限 = canPublish/canSubscribe/canPublishData） |
| Webhook | 新增 `POST /api/live-share/webhook`（LiveKit 回调）：参与者加入/离开 → 同步房间成员、清理「幽灵共享者」 |
| 角色 | 新增 `POST /api/live-share/guest`：host 把某人拉上台 / 请下台；观众 `POST /api/live-share/raise-hand` |
| 配置 | `live.sfu.url` / `api_key` / `api_secret`；未配置时培训模式入口禁用并说明 |
| 容量 | `live.max_viewers` 按模式不同：p2p 8，sfu 100（可配） |

现有 `liveshare.Room` 的发言状态机、屏幕共享状态机在 SFU 模式下仍然有效——它们管的是「谁被允许发布」，和媒体怎么走无关。

## 部署

```yaml
# docker-compose 片段
services:
  clawbench:
    ports: ["20000:20000"]
  livekit:
    image: livekit/livekit-server
    command: --config /etc/livekit.yaml
    ports:
      - "7880:7880"        # 信令（ws/wss）
      - "7881:7881"        # TCP fallback
      - "50000-50100:50000-50100/udp"   # 媒体
```

- LiveKit 复用 ClawBench 的证书目录（`wss://<同域名>:7880`）
- 单节点不需要 Redis；多节点才需要
- 100 人 × 2 Mbps ≈ 200 Mbps 下行，千兆网卡足够；CPU 约 1–2 核

## 容量与降级

| 规模 | 模式 | 服务器媒体 | 发布端上行 |
|---|---|---|---|
| ≤8 | P2P | 0 | N × 码率 |
| 9–100 | SFU | ~N × 码率（下行） | 1 × 码率 |
| >100 | SFU 多节点 | 分摊 | 1 × 码率 |

降级规则：
- 培训模式但 SFU 不可用 → 创建时禁用该选项，不提供「半残」的培训房间
- 观众超过上限 → 拒绝并提示，不进房再踢
- SFU 掉线 → 房间结束（和 host 掉线同语义），不尝试降级到 P2P（人数已经超了）

## 分阶段

| 阶段 | 内容 | 产出 |
|---|---|---|
| P0（现状） | P2P，8 人，听记，成员共享 | 已上线 |
| P1 | 前端抽出 `LiveTransport` 接口，P2P 实现接上（行为零变化） | 纯重构，为 P2 铺路 |
| P2 | 接入 LiveKit：token 签发、SFU 传输实现、模式选择 | 100 人可用 |
| P3 | 培训角色：举手、拉上台、静音全体、主讲转让 | 培训可用 |
| P4 | 聊天/问答、录制回放（Egress）、多节点 | 完整培训产品 |

P1 是关键：**先把接口抽出来，再换实现**。否则两条传输路径会在 UI 里长成一团。

## 取舍与风险

| 取舍 | 说明 |
|---|---|
| 多一个进程 | LiveKit 独立部署，运维多一件事；换来 100 人不用自己写 SFU |
| 多一组 UDP 端口 | 防火墙要开；内网通常没问题 |
| 培训模式依赖 SFU | 不可用时整个模式不可用（而不是降级成 8 人） |
| 两种拓扑两套代码 | 用 `LiveTransport` 隔离，业务层不感知；代价是前端多一层间接 |
| 听记仍由 host 上传 | v1 复用现有链路；host 掉线则听记中断（和共享结束同语义） |

## 未决问题

1. **主讲转让**：现在 host 断开 = 共享结束。培训场景常常需要「换人讲」而不结束房间——是否在 P3 一起做？
2. **观众上限与计费**：100 是硬上限还是按配置？是否需要按房间并发做限制？
3. **录制归属**：Egress 录制文件放哪、保留多久、谁能下载（现在听记是 7 天 + 项目内 Markdown）。
4. **跨网段**：培训如果有外网参与者，LiveKit 需要配 TURN（它自带），这会把「仅局域网」的限制打破，是否允许？
