# Dock 插件开发指南

这套文档写给“要给 ClawBench 的 Dock 加一个功能页签”的人。读完应该知道：页签怎么注册、面板怎么接、什么时候能用、角标和顺序怎么管、测试写在哪、哪些坑不能踩。

## 先弄清“插件”在这里指什么

这里的插件是**内部功能模块**，不是第三方可安装的扩展：

- 一个插件 = 左栏的一个页签（`chat` 是右栏常驻，不在此列）；
- 它有自己的清单条目、面板、可用性、角标、设置入口；
- 用户可以决定它在左栏显不显示、排在第几位；
- 不支持远程加载代码，也没有沙箱。

如果你要的是“第三方往我的应用里装插件”，那是另一个量级的工程，不在这套文档范围内。

## 当前落地到哪一步

| 能力 | 状态 |
|---|---|
| 清单（id / 标题 / 分组 / 能否隐藏 / 功能 / 能力） | 已落地，见 `web/src/composables/dockTabs.ts` |
| 功能编排（显示开关、拖动排序、三个预设、服务端开关） | 已落地，设置 → 功能编排 |
| 左栏渲染按「一条有序列表 + 隐藏集合 + 运行时门控」 | 已落地，宽屏与窄屏共用 `dockTabs()` |
| 服务端功能开关（Forge / 邮件 / 终端 / 知识库 / 任务 / 分享） | 已落地，唯一读写入口 `useFeatureFlags` |
| 预设可被插件读取（`usePluginProfile`） | 已落地 |
| 每个插件自己的 `dock.ts`（角标 / 门控 / onEnter / 后台资源） | 已落地，见 `web/src/components/<feature>/dock.ts` + `dockPluginRuntime.ts` |
| 面板加载 | 已落地：各功能 `dock.ts` 的 `panel.load`，由 `DockPanelHost` 统一渲染 |

所以现在新增一个插件，仍然要按 [07-checklist.md](07-checklist.md) 手工接线；`dock.ts` 自动注册是下一阶段的目标，设计见 [Dock 插件化重构设计](../plans/2026-11-14-dock-plugin-architecture-design.md)，本轮控制与排序的调整见 [Dock 控制与排序重构设计](../plans/2026-11-14-dock-controls-refactor-design.md)。

## 14 个左栏页签

聊天固定在右栏，不占左栏的一行。

| 类型 | 页签 | 能否隐藏 |
|---|---|---|
| 常驻 | 文件管理 `browse`、文件浏览 `view`、设置 `settings` | 否（开关灰显，写“常驻”） |
| 可选 | 历史 `history`、Forge `forge`、任务 `tasks`、终端 `terminal`、统计 `stats`、邮件 `mail`、知识库 `kb`、会议 `live`、分享 `share`、端口映射 `proxy`、通知中心 `notifications` | 是 |

常驻页签关不掉的原因：

- 文件管理是很多跳转的兜底落点；
- 文件浏览是打开文件的唯一去处；
- 通知中心是事件日志：藏掉入口等于让用户错过提醒，而它本身不占地方；
- 设置是重新打开其他功能的唯一入口。

**没有固定头部**：四个常驻页签只是不能隐藏，位置和其他页签一样可以拖。

## 管理器里的开关分三类

| 开关类型 | 页签 | 关掉之后 | 文案 |
|---|---|---|---|
| 服务端功能 | Forge、邮件、终端、知识库、任务、分享 | 服务端一起停（轮询 / 同步 / 调度 / 索引 / 分享链接），页签消失 | 启用 |
| 仅显示 | 历史、统计、会议、端口映射、通知中心 | 只是不在左栏显示，服务端不受影响 | 在 Dock 中显示 |
| 常驻 | 文件管理、文件浏览、设置 | 不能关 | 常驻（灰显） |

另有**环境能力**一档：终端依赖 PTY（Windows 没有）、端口映射依赖可用传输。宿主跑不了时，开关灰显并在行内写原因——这不是用户能开的开关。

功能编排里的开关是**唯一入口**。各功能自己的设置页只显示「已启用 / 已停用」+「去功能编排」，不要再放第二个总开关：两处开关会让用户以为它们各管一半，也容易绕过关闭前的确认。

## 预设

设置 → 功能编排提供三个预设，**同时决定显示、顺序和服务端开关**：

| 预设 | 隐藏 / 关闭 | 顺序 | 适合谁 |
|---|---|---|---|
| 开发 `developer` | 全开 | 默认顺序 | 开发、调试 |
| 标准 `standard` | 隐藏 Forge / 终端 / 端口映射，关闭 Forge / 终端 | 开发相关三项挪到设置之前 | 日常办公 |
| 极简 `minimal` | 只留常驻四项，其余全部隐藏并关闭 | 常驻三项在前、设置最后，其余按默认顺序排在中间 | 想要最干净的界面 |

- 应用「标准 / 极简」前会列出会被停掉、撤销的东西并要求确认；取消则整个预设不生效。
- 服务端开关一次提交（一个 PATCH），失败整体回滚——不会留下半套状态。
- 手动改任意一项后，当前状态变成“自定义”。插件通过 `usePluginProfile()` 读取预设，但应把 `custom` 当作中性默认值，不要拿它做权限判断。

## 快速开始

新增一个可选插件的最小路径：

1. 在 `web/src/composables/dockTabs.ts` 的 `DOCK_TABS` 加一条清单。
2. 在 `web/src/composables/dockTabMeta.ts` 加图标（除非图标随状态变，那就在 `dock.ts` 里给 `icon`）。
3. 在功能目录加一个 `dock.ts`：角标、可用性、`onEnter`、后台资源、面板加载都放这里。
4. 面板组件放在功能目录；`dock.ts` 的 `panel.load` 指向它。App.vue 的 `DockPanelHost` 会统一渲染，不需要逐插件写 `<TabPanel>`。
5. 需要服务端总开关的，在 `DockFeature` 里加 key，并在 `FEATURE_FLAG_DEFAULTS` 补默认值（缺键 = 开）。
6. 如果需要自己的设置页，在 `settingsFieldMap.ts` 加分类，并在 `SettingsIndex.vue` 加入口；`DOCK_TABS` 里填 `settingsCategory`。
7. 补单测，跑 `npm run typecheck`、相关 vitest、`npm run build`。

详细步骤与检查项见 [07-checklist.md](07-checklist.md)。

## 文件地图

| 文件 | 作用 |
|---|---|
| `web/src/composables/dockTabs.ts` | 纯数据清单 + 顺序/可见性纯函数（`dockTabs` / `mergeDockOrder`） |
| `web/src/composables/dockTabMeta.ts` | 静态图标 |
| `web/src/composables/dockPluginTypes.ts` | `dock.ts` 的契约类型（只含类型） |
| `web/src/composables/dockPluginRuntime.ts` | 聚合层：id → 各功能 `dock.ts` 的解析函数 |
| `web/src/composables/useDockLifecycle.ts` | 后台资源启停、`onEnter` / `onLeave` |
| `web/src/composables/useDockPlugins.ts` | 隐藏集合、顺序、预设、`usePluginProfile` |
| `web/src/composables/useFeatureFlags.ts` | 服务端功能开关的唯一读取入口（`isFeatureEnabled`） |
| `web/src/components/<feature>/dock.ts` | 该插件的角标 / 可用性 / 打开副作用 / 面板加载 |
| `web/src/components/dock/DockPanelHost.vue` | 通用面板宿主：按 id 取 `dock.ts` 的 panel，绑定 props/events |
| `web/src/utils/panelState.ts` | 持久化：`dockHidden` / `dockOrder` / `dockPreset` |
| `web/src/components/settings/DockPluginSetting.vue` | 功能编排 UI |
| `web/src/App.vue` | 左栏渲染、核心面板（文件管理/文件浏览）、角标展示 |
| `web/src/components/common/TabPanel.vue` | 面板保活外壳 |
| `web/src/components/settings/settingsFieldMap.ts` | 设置分类与字段 |
| `web/src/i18n/locales/{zh,en}.ts` | 文案 |

## 阅读顺序

1. [01-model.md](01-model.md) — 先理解模型：常驻/可选、一条有序列表、三种开关、预设。
2. [02-manifest.md](02-manifest.md) — 清单字段怎么填。
3. [03-panel.md](03-panel.md) — 面板怎么接、怎么懒加载、空状态。
4. [04-runtime.md](04-runtime.md) — 角标、门控、生命周期现在在哪，未来去哪。
5. [05-profile.md](05-profile.md) — 预设怎么读、插件怎么跟着调整。
6. [06-testing.md](06-testing.md) — 测试与守卫。
7. [07-checklist.md](07-checklist.md) — 照着做。
8. [08-pitfalls.md](08-pitfalls.md) — 踩之前先看。
9. [09-settings.md](09-settings.md) — 给插件加自己的设置页。

## 相关文档

- 控制与排序（当前模型）：[2026-11-14 Dock 控制与排序重构设计](../plans/2026-11-14-dock-controls-refactor-design.md)
- 插件化架构（下一阶段）：[2026-11-14 Dock 插件化重构设计](../plans/2026-11-14-dock-plugin-architecture-design.md)
- 视觉规范：[design-guide.md](../spec/client/design-guide.md)
- 前端架构：[frontend-architecture.md](../spec/client/frontend-architecture.md)
