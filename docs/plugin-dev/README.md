# Dock 插件开发指南

这套文档写给“要给 ClawBench 的 Dock 加一个功能页签”的人。读完应该知道：页签怎么注册、面板怎么接、什么时候能用、角标和顺序怎么管、测试写在哪、哪些坑不能踩。

## 先弄清“插件”在这里指什么

这里的插件是**内部功能模块**，不是第三方可安装的扩展：

- 一个插件 = 左栏 Dock 的一个页签（`chat` 是右栏常驻，不在此列）；
- 它有自己的清单条目、面板、可用性、角标、设置入口；
- 用户可以决定它在 Dock 里显不显示、排在第几位；
- 不支持远程加载代码，也没有沙箱。

如果你要的是“第三方往我的应用里装插件”，那是另一个量级的工程，不在这套文档范围内。

## 当前落地到哪一步

| 能力 | 状态 |
|---|---|
| 清单（id / 标题 / 分组 / 能否关 / 开发相关 / 功能 / 能力） | 已落地，见 `web/src/composables/dockTabs.ts` |
| 插件管理器（显示/隐藏、上下移、预设） | 已落地，设置 → 插件与 Dock |
| Dock 渲染按隐藏集合与用户顺序 | 已落地 |
| 预设可被插件读取（`usePluginProfile`） | 已落地 |
| 每个插件自己的 `dock.ts`（角标/生命周期自动注册） | **尚未落地**，目前角标、门控、打开副作用仍在 `App.vue` 里接线 |

所以现在新增一个插件，仍然要按 [07-checklist.md](07-checklist.md) 手工接线；`dock.ts` 自动注册是下一阶段的目标，设计见 [Dock 插件化重构设计](../plans/2026-11-14-dock-plugin-architecture-design.md)。

## 15 个页签的分类

| 分类 | 页签 | 能否关 |
|---|---|---|
| 常驻核心 | 聊天、文件管理、文件预览、设置 | 否 |
| 工作区 | Git 历史 | 是（位置固定在宽屏头部） |
| 项目工具 | Forge、任务、终端、统计、分享、端口映射 | 是 |
| 沟通与资料 | 邮件、知识库、会议 | 是 |
| 系统 | 通知 | 是 |

“常驻核心”关不掉的原因：

- 聊天是主界面；
- 文件管理是很多跳转的兜底落点；
- 文件预览是打开文件的唯一去处；
- 设置是重新打开其他插件的唯一入口。

## 三种“关闭”必须分开

| 类型 | 例子 | 用户关掉后 | 界面 |
|---|---|---|---|
| 能力（capability） | 终端、端口映射 | 不是“关”，是这台设备/宿主本来就没有 | 灰掉 + 原因 |
| 功能（feature） | Forge、邮件 | 服务端也停（轮询、同步、接口拒绝） | 真开关 |
| 偏好（preference） | 任务、统计、通知、分享、历史、知识库、会议 | 只是不在 Dock 显示，后台照常 | 眼睛/开关（只影响入口） |

插件管理器里的显示开关属于**偏好**，不会停服务端。Forge / 邮件的服务端开关在它们自己的设置页。不要把三种语义合成一个布尔。

## 预设

设置 → 插件与 Dock 提供三个预设：

| 预设 | 隐藏什么 | 适合谁 |
|---|---|---|
| 开发者 | 不隐藏任何页签 | 开发、调试 |
| 标准 | 终端、端口映射、Forge（`devRelated`） | 日常使用 |
| 极简 | 所有可关页签，只留聊天、文件管理、文件预览、设置 | 想要最干净的界面 |

手动改任意一项后，当前状态变成“自定义”。插件通过 `usePluginProfile()` 读取预设，但应把 `custom` 当作中性默认值，不要拿它做权限判断。

## 快速开始

新增一个可选插件的最小路径（当前版本）：

1. 在 `web/src/composables/dockTabs.ts` 的 `DOCK_TABS` 加一条清单。
2. 在 `web/src/composables/dockTabMeta.ts` 加图标。
3. 在 `web/src/App.vue` 加面板 import 与 `<TabPanel>` 块。
4. 如果需要角标 / 门控 / 打开副作用，在 `App.vue` 接线。
5. 如果需要自己的设置页，在 `settingsFieldMap.ts` 加分类，并在 `SettingsIndex.vue` 加入口。
6. 补单测，跑 `npm run typecheck`、相关 vitest、`npm run build`。

详细步骤与检查项见 [07-checklist.md](07-checklist.md)。

## 文件地图

| 文件 | 作用 |
|---|---|
| `web/src/composables/dockTabs.ts` | 纯数据清单 + 顺序/可见性纯函数 |
| `web/src/composables/dockTabMeta.ts` | 图标 |
| `web/src/composables/useDockPlugins.ts` | 隐藏集合、顺序、预设、`usePluginProfile` |
| `web/src/utils/panelState.ts` | 持久化：`dockHidden` / `dockOrder` / `dockPreset` |
| `web/src/components/settings/DockPluginSetting.vue` | 插件管理器 UI |
| `web/src/App.vue` | Dock 渲染、面板挂载、角标、门控、打开副作用 |
| `web/src/components/common/TabPanel.vue` | 面板保活外壳 |
| `web/src/components/settings/settingsFieldMap.ts` | 设置分类与字段 |
| `web/src/i18n/locales/{zh,en}.ts` | 文案 |

## 阅读顺序

1. [01-model.md](01-model.md) — 先理解模型：核心/可选、三种可用性、预设。
2. [02-manifest.md](02-manifest.md) — 清单字段怎么填。
3. [03-panel.md](03-panel.md) — 面板怎么接、怎么懒加载、空状态。
4. [04-runtime.md](04-runtime.md) — 角标、门控、生命周期现在在哪，未来去哪。
5. [05-profile.md](05-profile.md) — 预设怎么读、插件怎么跟着调整。
6. [06-testing.md](06-testing.md) — 测试与守卫。
7. [07-checklist.md](07-checklist.md) — 照着做。
8. [08-pitfalls.md](08-pitfalls.md) — 踩之前先看。
9. [09-settings.md](09-settings.md) — 给插件加自己的设置页。

## 相关文档

- 完整设计：[2026-11-14 Dock 插件化重构设计](../plans/2026-11-14-dock-plugin-architecture-design.md)
- 视觉规范：[design-guide.md](../spec/client/design-guide.md)
- 前端架构：[frontend-architecture.md](../spec/client/frontend-architecture.md)
