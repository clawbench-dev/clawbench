# 大屏双栏布局（Big-Screen SplitView）设计

日期：2026-08-03
状态：已确认

## 1. 目标与范围

为 ClawBench 桌面大屏场景提供独立的双栏界面布局，替代当前单一底部 Dock + 单栏 TabPanel 的移动优先布局。

**进入方式**：窗口/视口宽度 ≥ 1024px 时自动进入大屏双栏；低于该宽度自动回退现有单栏布局。无需手动切换。

**范围**：
- 仅影响前端 `web/src/`。后端无改动。
- 仅覆盖已登录后的 `.app-container` 区域，登录页 / 欢迎页等不受影响。
- 所有现有面板组件（Chat/File/Git/Proxy/Terminal/Task/Settings）保持单实例，不复制挂载。

## 2. 需求要点（已与用户确认）

| 编号 | 需求 | 决策 |
|---|---|---|
| R1 | 进入/退出 | 宽度 ≥1024px 自动进入，<1024 回退单栏 |
| R2 | 右栏 | 始终为聊天面板（Chat），恒活 |
| R3 | 左栏标签 | 通过左侧纵向 Dock 切换 browse/history/proxy/terminal/tasks/settings；**不含 chat** |
| R4 | 分隔线 | 可左右拖动调整两栏宽度 |
| R5 | 持久化 | 分隔线比例与 leftTab 均持久化到 localStorage |
| R6 | 顶部 Header | 保留 |
| R7 | 底部 Dock | 大屏下隐藏 |

### 已确认的细化决策

1. **leftTab 连续性优先（Q1A）**：进入大屏时若 `activeTab` 为非 chat 标签，`leftTab` 采纳该值；`activeTab === 'chat'` 时用持久化值；仅首次（无持久化记录）默认 `'browse'`（文件管理器）。
2. **useTabDrawer 双激活标签（Q2A）**：大屏下 `chat` 与当前 `leftTab` 同时视为激活标签，抽屉均可打开。
3. **activeTab 跟随 leftTab（Q3B）**：大屏下切换左栏标签时同步写 `activeTab`（但不触发 `onTabSwitch`），收窄窗口回单栏时显示最后使用的左栏标签。
4. **左栏 Dock 角标（Q4A）**：复用现有计数 —— history 显示 Git 变更数、tasks 任务未读数、terminal 会话数、proxy 端口转发数。
5. **分隔线存储比例（Q5A）**：存 0~1 比例，跨窗口尺寸等比换算。
6. **纵向 Dock 实现（方案一）**：在 App.vue 内新增纵向 Dock 块，复用现有 scoped Dock 样式与辅助函数，不做组件抽取。
7. **chat 恒活（Q7A）**：大屏下 chat `:active` 恒为 true，自动滚动与快捷键保持生效。
8. **对称最小宽度**：左右栏均 320px。
9. **leftTab 跨项目保留（Q9A）**：`hotSwitchProject` 不重置 leftTab。
10. **纵向 Dock 无 overflow**：6 个标签纵向可全部容纳，不做折叠菜单。

## 3. 架构

### 3.1 新增文件

| 文件 | 职责 |
|---|---|
| `web/src/components/common/SplitView.vue` | 通用双栏容器：`#left`/`#right` 插槽 + 可拖拽分隔线 + 比例持久化。可复用，不绑定业务 |
| `web/src/composables/useBigScreenLayout.ts` | 模块单例。`isBigScreen`（matchMedia 1024）、`leftTab`、`splitRatio`、`switchLeftTab`、`setSplitRatio`、`clampSplitRatio`（纯函数）、`resetBigScreenState` |
| `web/src/composables/__tests__/useBigScreenLayout.test.ts` | useBigScreenLayout 单元测试 |
| `web/src/components/common/__tests__/SplitView.test.ts` | clamp 纯函数与持久化测试 |
| `web/css/big-screen.css` | 大屏布局全局样式（引入 index.html）：`.main-content.big-screen` 的 flex-row 变体、`.big-dock` 尺寸定位等结构规则；`layout.css` 不改动 |

### 3.2 修改文件

| 文件 | 修改 |
|---|---|
| `web/src/App.vue` | `.content-area` 内包 `col-left`/`col-right` + `SplitView`；新增纵向 Dock 块与相关状态/函数；`switchTab` 模式感知；Dock 隐藏；CSS 竖排变体 |
| `web/src/composables/useTabDrawer.ts` | 大屏感知的 `effectiveOpen` 与 autoRestore watcher |
| `web/src/components/common/BottomSheet.vue` | 大屏下 `.bs-panel` 全局限宽 + `wide` prop 逃逸通道（见第 6.5 节） |
| `web/index.html` | 引入 `big-screen.css` |
| `web/src/composables/__tests__/useTabDrawer.test.ts` | 增加大屏双激活标签用例 |

### 3.3 布局结构

```
.app-container (flex-column)
├── AppHeader（保留，R6）
├── main.main-content（宽屏加 .big-screen class → flex-row）
│   ├── .big-dock（仅大屏，纵向 Dock，~64px，6 个非 chat 标签）
│   └── .content-area (id="contentArea")
│       └── SplitView(:enabled="isBigScreen" :ratio="splitRatio")
│           ├── 窄屏（enabled=false）：单列块容器，分隔线隐藏
│           │   ├── col-left   (6 个非 chat 面板，activeTab 互斥 v-show)
│           │   └── col-right  (chat 面板，activeTab==='chat' 时 v-show)
│           └── 宽屏（enabled=true）：flex-row，左栏宽 = ratio，分隔线在中间
│               ├── col-left   (leftTab 决定内部哪个面板 v-show)
│               ├── 分隔线（可拖动）
│               └── col-right  (chat 恒显)
└── .bottom-dock-wrapper（宽屏时 v-show 隐藏，R7）
```

**关键约束**：所有 TabPanel 保持单实例。`col-left`/`col-right` 为 `position:relative` 容器，作为内部绝对定位 TabPanel 的定位上下文。**SplitView 通过 `enabled` prop 决定两种形态**（true=双栏+分隔线，false=单列堆叠+分隔线隐藏），col-left/col-right 的显隐逻辑（chat 恒显 / activeTab 互斥 / leftTab v-show）由 App.vue 插槽内容负责，SplitView 保持业务无关、可复用。

## 4. 状态与路由设计

### 4.1 `useBigScreenLayout.ts`（模块单例）

- `isBigScreen: Ref<boolean>`：`window.matchMedia('(min-width: 1024px)')`，监听 `change`。若 `window.matchMedia` 不存在（测试环境 jsdom）则安全降级为 `false`，不挂监听，不抛错。
- `leftTab: Ref<string>`：宽屏左栏标签。默认 `'browse'`；进入大屏时连续性采纳（见 R 细化 1）。持久化键 `clawbench-bigscreen-left-tab`。
  - **进入大屏时的确定性规则**：`leftTab = activeTab !== 'chat' ? activeTab : (persistedValue ?? 'browse')`，并**立即持久化采纳结果**（此后大屏内切换由 `switchLeftTab` 持续写入）。
- `splitRatio: Ref<number>`：左栏宽度比例 0~1。默认 0.5。持久化键 `clawbench-bigscreen-split-ratio`。写入前经 `clampSplitRatio` 归一。
- `switchLeftTab(tab)`：`leftTab = tab`，并同步写 `activeTab`（R 细化 3，通过回调注册，见下）；**不调 `onTabSwitch`**。复用现有 side-effect：`browse → store.loadFiles(currentDir)`、`tasks → 清 taskUnreadCount + loadTasks`。
- `clampSplitRatio(ratio, containerWidth)`（纯函数，可测）：clamp 到 `[minLeft, containerWidth - minRight] / containerWidth`，其中 `minLeft = minRight = 320`。**边界守卫**：当 `containerWidth <= minLeft + minRight` 时返回 0.5（理论不会发生，因大屏触发宽度 ≥1024，但函数必须自洽）。
- `resetBigScreenState()`：重置 ref 为默认值（供测试与需要时调用）。

**App.vue 与 useBigScreenLayout 的耦合方式**：useBigScreenLayout 导出 `registerSwitchLeftTabSideEffects(cb)` 或由 App.vue 在挂载时设置 `switchLeftTab` 的 side-effect 回调（依赖 `store`、`loadTasks`，这些在 App.vue 作用域）。useBigScreenLayout 只管理纯状态；side-effect 通过回调注入，保持可测试。

### 4.2 `switchTab` 模式感知（App.vue）

```
function switchTab(tab) {
  if (isBigScreen.value) {
    if (tab === 'chat') return        // chat 恒显，no-op
    switchLeftTab(tab)                 // 路由到左栏
    return
  }
  ...现有窄屏逻辑不变...
}
```

`switchTab` 的既有注入方（AppHeader、useTaskTab.registerSwitchTab、useSessionIdentity、FileManagerContent open-terminal 等）在大屏下自动获得正确路由：非 chat → 左栏；chat → 无害 no-op。

### 4.3 面板 active 计算（App.vue）

- chat TabPanel `:activeTab`：`isBigScreen ? 'chat' : activeTab`
- chat 面板 `:active`：`isBigScreen || activeTab === 'chat'`（恒活，R 细化 7）
- 其余 6 个面板 `:activeTab`：`isBigScreen ? leftTab : activeTab`
- terminal/tasks/settings/git 面板 `:active`：`isBigScreen ? leftTab === tabId : activeTab === tabId`

### 4.4 模式切换的抽屉同步

- 进入大屏：`onTabSwitch('chat')` —— 聊天抽屉（Session/MessageClusters/UserMsgIndex 等）保持可用。
- 退出大屏：`onTabSwitch(activeTab)` —— 与单栏 currentTab 对齐。
- 进入大屏：强制 `overflowMenuOpen = false`。

## 5. useTabDrawer 扩展

模块内新增共享大屏状态（`isBigScreen`、`leftTab` 两个 ref，由 App.vue / useBigScreenLayout 写入）。

```
effectiveOpen = computed(() => {
  const tabActive = currentTab.value === tabId
                 || (isBigScreen.value && (tabId === 'chat' || tabId === leftTab.value))
  return tabActive && openRef.value
})
```

autoRestore:false 的 watcher 从仅监听 `currentTab` 扩展为同时监听 `currentTab` 与（大屏下的）`leftTab`：大屏下切走左栏标签时对应抽屉正确关闭；恢复该标签时 `openRef` 保留语义不变。

**兼容性**：窄屏（`isBigScreen === false`）下行为与现有 `useTabDrawer.test.ts` 用例完全一致。

## 6. SplitView 组件

**Props**：`enabled`（boolean，false=单列堆叠+分隔线隐藏，true=双栏+分隔线）、`ratio`（初始比例，默认 0.5）、`minLeft`/`minRight`（默认 320）、`gutterSize`（视觉宽度，默认 6px）。

**插槽**：`#left`、`#right`（App.vue 传入 `col-left`/`col-right` 容器）。

**受控组件**：SplitView 为**受控组件**，`ratio` 由外部传入，拖动时 emit `update:ratio`。App.vue 绑定 `:ratio="splitRatio"` + `@update:ratio="setSplitRatio"`；**持久化由 useBigScreenLayout.setSplitRatio 负责**，SplitView 自身不做 localStorage 读写（保持通用可复用）。

**形态**：根节点 `height:100%`。`enabled=false` 时块级单列、两插槽内容各占满容器（由外部 v-show 控制显隐）；`enabled=true` 时 flex-row，左栏 `width: ratio * containerWidth`、右栏 `flex:1`、分隔线在中间。

**交互**：
- 分隔条默认窄（6px），hover / 触碰时加宽（命中区约 14px）+ 视觉反馈，松手恢复窄条。
- `pointerdown` → `setPointerCapture` → `pointermove` 实时计算新左栏宽度 → clamp → 更新；`pointerup` 释放。
- 拖动期间给 `document.body` 加 `user-select: none`，结束后移除。
- `title` 提示"拖动调整面板宽度"。
- 宽度比例由外部（useBigScreenLayout.setSplitRatio）持久化；窗口 resize 时按当前容器宽度与 `clampSplitRatio` 归一。

**最小宽度兜底**：容器 CSS `min-width` 同步设 320px，避免首帧跳动。

### 6.5 大屏下抽屉宽度（方案 B：全局限宽 + 逃逸通道）

**问题**：`.bs-panel` 为 `position:fixed` 全视口底部对齐（BottomSheet.vue:158-191），大屏下横跨全宽不美观。

**方案**：
- BottomSheet.vue 样式新增：
  ```css
  @media (min-width: 1024px) {
    .bs-panel { max-width: 560px; margin: 0 auto; }   /* 底部对齐 + 水平居中 */
    .bs-panel.bs-wide { max-width: 820px; }
  }
  ```
- 新增 `wide` prop（Boolean）→ `.bs-wide` class。内容型抽屉（会话列表、文件搜索、Agent 选择器、Git 历史等富内容抽屉）按需标记 `wide`。
- 阈值 1024px 与大屏触发宽度一致；窄屏（<1024）行为完全不变。
- **不锚定栏内**（方案 D 否决）：BottomSheet 硬编码 `<Teleport to="body">`、95 处用法、栏矩形动态跟踪、双形态定位，改造面过大。

## 7. 纵向 Dock（App.vue 内，方案一）

- 新增 `.big-dock` 块（`v-show="isBigScreen"`），位于 `.main-content` 内、`.content-area` 左侧。
- 渲染 browse/history/proxy/terminal/tasks/settings 六个按钮（**无 chat**），复用现有 `.dock-btn`/`.dock-badge`/scoped 样式与辅助函数（`dockTabIcon`/`dockTabTitle`/`formatBadgeCount`/角标动画 ref）。
- 纵向激活指示条：新增 `bigDockActiveIndex`（基于 leftTab 在 6 项中的序号）与 `translateY` 指示样式；复用现有 `DOCK_STEP`（46px）。
- 角标：history→`gitWorkingTreeChangeCount`、tasks→`taskUnreadCount`、terminal→`terminalSessionCount`、proxy→`portForwardActiveCount`；无 chat 的未读/运行光圈。
- 无 overflow 折叠（6 项纵向可全容）。

## 8. 已知取舍

1. **"飞入聊天"粒子动画**：FileHeader / FileManagerContent / useQuoteQuestion 中 `querySelector('.dock-center')` 在底部 Dock 隐藏时返回 null（现有代码均用可选链 + `?? null` 防护），动画不播但无异常。后续可改为瞄准右栏聊天头部。
2. **App.vue 体积**：新增约 100 行 Dock 相关模板/样式，符合现有" Dock 逻辑集中在 App.vue"的既有模式。
3. **窄屏 Dock overflow**：大屏下 `.bottom-dock` 为 display:none，`useDockOverflow` 测得尺寸为 0，会临时把标签推进折叠菜单，但不可见且回窄屏后自动重测恢复。

## 9. 测试计划

| 测试文件 | 覆盖 |
|---|---|
| `useBigScreenLayout.test.ts` | matchMedia mock（有/无 matchMedia）；isBigScreen 翻转；leftTab 默认 browse/连续性采纳/持久化；clampSplitRatio 边界（0.5 默认、极值、容器过窄）；switchLeftTab side-effect 回调触发 |
| `useTabDrawer.test.ts`（扩展） | 大屏下 chat 与 leftTab 双激活；autoRestore:false 随 leftTab 切换关闭；窄屏行为回归不变 |
| `SplitView.test.ts` | enabled 两种形态；ratio→px 换算与 clamp；拖动 pointer 数学（纯 helper）；update:ratio 事件 |
| `BottomSheet`（如已有测试则扩展） | `wide` prop 绑定 `.bs-wide` class（宽屏限宽为 CSS 媒体查询，jsdom 下验证 class 绑定即可） |

## 10. 不做（YAGNI）

- 手动切换大屏按钮（R1 决定自动进入）
- 分隔线键盘无障碍（←/→ 调整）
- 大屏模式下 chat 可移至左栏
- 独立标签状态入 store 的重构
- 抽屉栏内锚定（方案 D / D-lite，见 6.5）
- DockBar.vue 组件抽取（方案二，留待将来）
