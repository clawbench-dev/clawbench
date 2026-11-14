# 01 · 模型：核心页签、可选插件、三种可用性

## 1. 一个插件由什么组成

一个 Dock 插件在当前版本有四块：

| 块 | 现在在哪 | 说明 |
|---|---|---|
| 身份 | `DOCK_TABS`（`dockTabs.ts`） | id、标题、描述、分组、能否关、开发相关、功能/能力标记 |
| 图标 | `dockTabMeta.ts` 或功能 `dock.ts` 的 `icon` | 静态图标在 meta；随状态变的（Forge）在 `dock.ts` |
| 面板 | 功能 `dock.ts` 的 `panel.load` | 由 `DockPanelHost` 统一渲染；核心文件管理/文件预览仍在 App.vue |
| 行为 | 功能 `dock.ts` | 角标、可用性、`onEnter`、后台资源 |

下一阶段会把“图标 + 面板 + 行为”收进每个功能自己的 `dock.ts`，见 [04-runtime.md](04-runtime.md)。当前先按现状写。

## 2. 核心页签 vs 可选插件

`DOCK_TABS` 里的 `closable` 决定用户能不能在插件管理器里关掉它。

| 类型 | `closable` | 例子 | 规则 |
|---|---|---|---|
| 核心 | `false` | 聊天、文件管理、文件预览、通知中心、设置 | 永远可见，管理器里没有开关 |
| 可选 | `true` | 其余 10 个 | 可以隐藏、可以排序（部分） |

为什么核心不能关：

- **聊天**：没有它就没有主界面。
- **文件管理**：很多跳转（任务打开文件、通知跳转、空状态按钮）默认落到 `browse`。它一关，这些路径就没有落点。
- **文件预览**：打开文件的唯一去处。
- **通知中心**：它是只追加的事件日志，没有已读、没有角标，藏掉入口只会让用户错过提醒；它本身也不占空间。
- **设置**：插件管理器在设置里；设置一关，用户就再也打不开被关的插件了。

## 3. `primary` 不等于“不可关”

`primary` 只表示“宽屏 Dock 的固定头部”，不表示“不能关”。

- `browse` / `view`：`primary: true, closable: false`
- `history`：`primary: true, closable: true` —— 它在固定头部，但用户可以隐藏。

因此宽屏 Dock 渲染固定头部时必须先按可见性过滤：

```ts
const visiblePrimaryTabs = computed(() =>
  primaryDockTabs({ hidden: hiddenDockTabs.value }),
)
```

不能直接 `v-for="tab in WIDE_SCREEN_PRIMARY_TABS"`，否则用户刚隐藏的 `history` 还会出现。

## 4. 三种可用性

这是整个模型最重要的一刀。三种“关闭”不是一回事：

| 类型 | 谁决定 | 关掉后 | 现有例子 | 控件 |
|---|---|---|---|---|
| 能力 capability | 设备/宿主 | 不是关，是本来就没有 | 终端（PTY）、端口映射（没有可用传输） | 灰掉 + 原因 |
| 功能 feature | 服务端配置 | 服务端也停 | Forge（停轮询、清绑定）、邮件（停 IMAP 同步） | 真开关 |
| 偏好 preference | 这个客户端 | 只是不在 Dock 显示 | 任务、统计、分享、历史、知识库、会议 | 眼睛/显示开关 |

### 4.1 组合规则

```
可见 = 能力可用 && 功能已开 && (不可关 || 用户没有隐藏)
```

- 核心页签不受偏好影响。
- 能力不满足时，即使功能开着、用户没隐藏，也不可见。
- 功能关闭时，Dock 不渲染；如果当前正打开它，宿主切到兜底页签。
- 偏好隐藏只影响入口，已经挂载的面板保持挂载，状态不丢；客户端角标轮询（邮件未读）会停。

原因优先级：能力 > 功能 > 偏好。管理器只显示最根本的一条。

### 4.2 当前代码怎么落

- **偏好**：`useDockPlugins` 的 `hidden` 集合，由插件管理器写入。
- **功能**：`forge/dock.ts` / `mail/dock.ts` 的 `available()` 读服务端配置。
- **能力**：`terminal/dock.ts` / `proxy/dock.ts` 的 `available()` 读运行时状态。

`App.vue` 把所有插件的 `available()` 汇总成 `unavailableDockTabs`，再交给 `secondaryDockTabs` 过滤；程序化导航统一过 `isDockPluginAvailable`。

清单里的 `feature` / `capability` 字段是**声明**：管理器据此显示“服务端功能”“环境能力”标签，也提醒开发者这个插件依赖什么。真正的检查在功能自己的 `dock.ts` 里。新增插件若需要门控，两处都要写：清单里声明，`dock.ts` 里实现 `available()`。

## 5. 预设（profile）

预设是给用户的一步式选择，也是插件可以读取的“环境信号”。

| 值 | 含义 | 隐藏 |
|---|---|---|
| `developer` | 开发者 | 无 |
| `standard` | 标准 | `devRelated` 的页签（终端、端口映射、Forge） |
| `minimal` | 极简 | 所有 `closable` 页签 |
| `custom` | 自定义 | 用户手动改过；没有固定语义 |

默认是 `developer`（保持旧行为：所有页签都显示）。

### 5.1 预设不是权限

预设只表达“这个用户想把界面调成什么样”，**不能拿它做权限、安全或功能可用性判断**。真正的门控是能力/功能/偏好三轴。

### 5.2 插件怎么读

```ts
import { usePluginProfile } from '@/composables/useDockPlugins'

const { isDeveloper, isStandard, profile } = usePluginProfile()
```

建议：

- `isDeveloper`：显示调试、原始数据、高级开关；
- `isStandard`：隐藏开发相关的高级区块；
- `custom` / `minimal`：回到中性默认，不要猜用户意图。

详见 [05-profile.md](05-profile.md)。

## 6. 顺序规则

Dock 的顺序分三段：

1. **固定头部**：`primary` 页签，顺序由清单决定（`browse` → `view` → `history`）。
2. **可排序尾部**：`REORDERABLE_DOCK_TABS`，用户在管理器里拖动排序。
3. **固定尾部**：`settings`，永远最后。

`REORDERABLE_DOCK_TABS` 的定义是：

```ts
DOCK_TABS.filter(t => !t.primary && t.closable && t.id !== 'settings')
```

所以：

- `history` 是 primary，位置固定，但可以隐藏；
- `settings` 是 core，不可隐藏、不可排序，永远最后；
- 其余可选页签可以排序。

### 6.1 新增页签的位置

如果用户存过顺序，而新页签不在里面，`mergeDockOrder` 会把它按清单默认顺序追加到尾部。这是有意的：新插件不会因为旧存储而消失，也不会插到用户排好的前面。

## 7. 跨插件依赖

插件可以“单独打开”，但不一定“单独有内容”：

| 插件 | 单独打开时 | 依赖 |
|---|---|---|
| 文件预览 | 最近文件 / 空状态 | 文件管理、Git 历史、聊天提供文件 |
| 分享 | 已分享列表（可能为空） | 文件/会话/会议产生内容 |
| 统计 | 用量数据（可能为空） | 聊天产生用量 |
| Forge | 绑定仓库引导 | 当前项目 |
| 其余 | 自己的空状态 | 不依赖别的插件 |

依赖不等于不能关。关掉上游时，下游显示空状态 + 引导，不要报错、不要白屏。
