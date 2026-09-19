---
name: clawbench-user-guide
description: 为 ClawBench 生成桌面端与移动端图文使用说明 / 用户手册配图。桌面端：720p 降采样截图参数（2560×1440 视口 + zoom=2 再 LANCZOS 降到 1280×720）、左栏 441px 构图约束、浮层抑制与自检（三个 fixed 浮层的 offsetParent 判据失效陷阱）、完成通知卡片必须 reload 清除、演示数据搭建与还原、按模块的取景清单与 DOM 选择器、三段式文档结构。移动端：CDP 设备模拟（zoom 方案对移动端完全无效）、390×844@3 竖屏参数与 585×1266 后处理、底部 Dock 与全屏 TabPanel 布局事实、手势阈值速查、Android App 模式 bridge stub 注入（必须 dispose）、移动端验收判据。触发词：使用说明、用户手册、user guide、图文说明、功能说明文档、手册配图、文档配图、移动端文档、手机端说明、竖屏截图。
allowed-tools: Bash(playwright-cli:*), Bash(curl:*), Bash(python3:*)
---

# ClawBench 桌面端使用说明（截图 + 文档）

为 ClawBench 生成「图文并茂的桌面端使用说明」：在真实实例上截取 720p 功能图，配上按模块组织的说明文字。

> 与 `clawbench-screenshot` skill 的区别：那个是**宣传图/hero 图**导向（1080p、2x、追求好看、无遮挡深色）；这个是**文档配图**导向（720p、信息完整、能看清操作、允许界面里有真实数据）。两者的登录/浮层/主题流程相通，但参数与验收标准不同。

## 一、截图参数（实测确定，别改）

| 项 | 值 | 说明 |
|---|---|---|
| 视口 | **2560×1440** | 设备像素 |
| `documentElement.style.zoom` | **`'2'`** | 有效 CSS 布局 = 1280×720 |
| 截图原始尺寸 | 2560×1440 | |
| 后处理 | `Image.resize((1280,720), Image.LANCZOS)` | 降采样，输出 720p |
| 单图体积 | 约 400–620 KB | PNG optimize |

**为什么用 zoom 超采样而不是 CDP 的 deviceScaleFactor：**
`Emulation.setDeviceMetricsOverride({deviceScaleFactor: 2})` 在本环境**实测不生效**（`devicePixelRatio` 仍是 1，输出还是 1280×720）。改用「视口放大 2 倍 + `style.zoom=2`」可稳定得到 2x 渲染。

**关键：布局等价性已验证。** zoom=2 / 视口 2560 与直接 1280×720 / zoom=1 的归一化几何**逐值相同**（实测侧栏都是 `[1000, 36, 280, 684]`）。所以不会因为超采样而改变布局——只是渲染更锐利。

**不要固定 1280×720 视口 + zoom=1**：文字会有明显锯齿。也不要 zoom=1.25 配 1280 视口——有效宽度降到 1024，正好卡宽屏断点，三栏被压扁。

### 主题必须显式钉成 github-dark

**新浏览器 profile 的默认主题是 `auto`（跟随系统）**。在本环境系统是亮色 → 截图整片白底，与既有图（深色）完全不一致。

```js
localStorage.setItem('clawbench-settings-theme', JSON.stringify('github-dark'));
```

既有图的实测背景色（用于自检）：`#161B22` / `#0D1117` / `#21262D`。

**同时**这个键是 `localStorage`，所以必须**在 reload 之前写**（reload 后由 `useSettingsConfig` 读取生效）。同理适用于分栏比例与两个浮层抑制键——见 §二、§五。

> 用 `--persistent --profile <dir>` 可让 localStorage 跨浏览器重启保留，省去重复登录与重复设置。但实测本机内存紧张时持久化 profile 更容易被 OOM 杀掉，改用「单次调用内完成全部操作」更稳（见 §二十 第 25 条）。

### 登录页是例外

`.login-page` 是 `min-height:100vh` + `overflow:hidden` + flex 居中（`web/src/components/LoginView.vue:326-334`）。内容（品牌区 + 表单）在 720px 高度下放不下，而 `overflow:hidden` **禁止滚动**，表单会被永久裁掉。

- 解法：登录页用 **zoom 1.5**（视口仍 2560×1440），实测内容完整容纳。
- 不要试图 `scrollIntoView` / `window.scrollTo`——`overflow:hidden` 下无效，会静默失败（截图字节数完全不变是征兆）。

## 二、浮层处理（最容易翻车的地方）

### 陷阱：`offsetParent` 判据对 fixed 浮层恒为 false

现有 `clawbench-screenshot` skill 里的 `!!document.querySelector('.up-overlay')?.offsetParent` **是无效检查**——三个浮层全是 `position: fixed`，而 **fixed 元素的 `offsetParent` 恒为 `null`**。实测：弹窗正盖在画面上，该表达式仍返回 `false`。

可靠判据（`web/src/components/UpgradePromptOverlay.vue:83`、`WelcomeOverlay.vue:237`、`CompletionPopover.vue:304` 均为 fixed）：

```js
const vis = (e) => {
  if (!e) return false;
  const cs = getComputedStyle(e), r = e.getBoundingClientRect();
  return cs.display !== 'none' && cs.visibility !== 'hidden'
    && parseFloat(cs.opacity) > 0 && r.width > 0 && r.height > 0
    && (typeof e.checkVisibility === 'function'
        ? e.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true }) : true);
};
```

`elementFromPoint(innerWidth/2, innerHeight/2)` 也可作交叉验证（返回 `up-panel` 即被盖）。

### 三个浮层的抑制

| 浮层 | 抑制方式 |
|---|---|
| `.welcome-overlay` | `localStorage['clawbench_welcome_dismissed']='true'`（reload 前写） |
| `.up-overlay` | `localStorage['clawbench-upgrade-skip']=<latest_version>`；版本升级后旧值失效，须重新取。也可点 `.up-skip` |
| `.completion-popover-backdrop` | **先 `page.reload()`**（首选，见下）；点「标记已读」循环清空只作兜底 |

### 完成通知卡片：**截图前必须 reload**（用户明确要求）

**这是清除卡片的第一手段，不是兜底。** 循环点「标记已读」只清当前队列，仍有三个漏网场景：脚本没写清队列逻辑、清完又有新会话完成、以及最坑的——**整轮 setup 从未执行**（见下文「登录判定」）。

为什么 reload 可靠（已核源码）：

- `useCompletionPopover` 的 `queue` / `active` 是**模块级内存状态**（`web/src/composables/useCompletionPopover.ts:29-30`），reload 即清零。
- 刷新后的 WS replay 会补发历史 `completed` 事件，但 `web/src/App.vue:1209` 有显式拦截：
  ```js
  if (skipReplay && isReplayingEvents.value) return   // 重放阶段不弹窗
  ```
  → **卡片不会复活**。

**实测代价（第一版踩坑）**：`openfile.sh` / `annot.sh` / `termqc.sh` 三个脚本**没有**清卡片逻辑，导致 **22 张**截图带着卡片交付（全部 `preview-*`、`annot-01/02`、`term-04/05`、两个 `dialog-*`、`sess-03/04`）。用户一眼看出「非常碍眼」。

**像素级自检判据**（比 DOM 检查更可靠，因为卡片是 fixed 定位、DOM 上一直在）：

PC 宽屏 1280px 下卡片几何固定（`max-width: min(680px,92vw)` + `inset:0` + `align-items:flex-start`）→ 占 **x=300..980, y=8..220**。取左右竖边框像素比对参考色：

```python
CARD_L=(47,71,100); CARD_R=(48,73,102)   # github-dark 主题下的实测边框色
l = px[300,110]; r = px[979,110]
card = sum(abs(l[i]-CARD_L[i]) for i in range(3)) < 40 and \
       sum(abs(r[i]-CARD_R[i]) for i in range(3)) < 40
```

命中图 Δ=0–30，干净图 Δ=80–180，双峰分离清晰。**交付前必须对全部图跑一遍**，期望只剩 `00-completion-popover.png`（有意演示）。

- 升级弹窗是**延迟出现**的（登录后数秒），DOM 检查跑早了会误判为无弹窗 → 截完图才发现被盖。**截图动作与浮层检查要在同一次 evaluate 内完成**。
- 机会性取材：升级弹窗本身是「应用自升级」特性、完成通知卡是「完成通知弹窗」特性，遇到了就顺手存档（编号 `00-*`）。

### 登录判定：必须用 `/api/me`，不能用 DOM

**最隐蔽的坑：判定「要不要登录」若用 `document.querySelector('input[type=password]')`，在已登录但 SPA 尚未渲染完时可能为真 → `page.click('button.login-btn')` 等 30s 超时 → 整个 `evaluate` 从未执行 → 截图落成上一轮残留图，且**与上一张 md5 相同**。**

实测：一批 5 张设置图（`set-12`~`set-16`）md5 完全一致，全部是同一张陈旧图。

正确写法：

```js
// 用接口判定登录态，超时可控
let meStatus = 0;
try {
  meStatus = await page.evaluate(async () => {
    try { const r = await fetch('/api/me', { credentials: 'include' }); return r.status; }
    catch (e) { return 0; }
  });
} catch (e) { meStatus = 0; }

if (meStatus !== 200) {
  await page.waitForSelector('input[type=password]', { timeout: 15000 }).catch(() => {});
  // ... 有表单才填，填完 page.click('button.login-btn')
}
```

**并且必须加产出防线**：解析脚本输出，若 `setup` 返回字符串错误（含 `MISS` / `not found`）或 `ov.card === true` 或登录异常，就在文件名后打 `[NEEDS REVIEW]` 标记，不要静默写图。否则残留图会被当成新图混进交付。

## 三、演示数据（可新建，不动存量）

用户授权边界：**可新建**演示会话/任务/文件；**只读浏览**现有会话/文件/Git 历史；**不可改**任何存量数据。

- 记基线：`sqlite3 "file:$HOME/.clawbench/ClawBench.db?mode=ro&immutable=1" "select count(*) from chat_sessions;"`
- 演示会话：会话侧栏 `[data-action=create]` → 弹「选择智能体」`.bs-overlay` → 选 Codebuddy → 发**只读型**提问（如「阅读 docs/spec/README.md 介绍模块划分，不要修改任何文件」）。只读提问能自然触发 Read 工具调用 + Markdown 回复，且不会改动仓库。
- 收尾：删除新建会话；`git status` 比对基线。

**注意**：主工作区可能有其他 agent 在跑（实测遇到），他们的会话/未提交改动会出现在截图里。构图时尽量避开，或接受它们作为背景。

## 四、按模块取景清单与选择器

宽屏布局：`.wide-dock`（左竖栏）+ `.col-left`（左内容）+ `.col-right`（聊天）+ `.session-sidebar`（会话侧栏，固定 280px）。

**Dock 按钮无 `data-tab`，只有 `title`**（中文界面）：文件管理器 / 文件 / 项目历史 / 议题与合并请求 / 任务 / 终端 / 数据统计 / 端口映射 / 设置。

| 模块 | 面板根选择器 | 备注 |
|---|---|---|
| 登录 | `.login-form-card` | zoom 1.5；见上文 |
| 文件管理器 | `.browse-panel .file-manager-content` | 条目 `.file-item[data-path][data-action=file\|dir]` |
| 文件预览窗格 | `.fm-preview-pane` | 先点 `title="开启预览模式：单击文件快捷预览"`，再单击文件 |
| Markdown 查看器 | `.file-viewer` / `.markdown-body` | 双击文件打开；TOC 按钮 `title="目录"` → `.toc-dock` |
| 聊天 | `.chat-tab-panel` / `.chat-messages` | 消息 `.chat-message`，工具卡 `.chat-tool-call`、`.tool-detail.chat-inline-card` |
| Git 历史 | `.git-history-content` | 提交行 `.drilldown-item`；文件行 `.git-file-info` → diff 抽屉 |
| 任务 | `.task-tab` | 含事件任务 + 定时任务 |
| 终端 | `.terminal-panel` | xterm；输入焦点 `.xterm-helper-textarea` |
| 数据统计 | `.stats-tab-host` | 三子页：用量统计/代码存量/代码增量 |
| 设置 | `.settings-page` | 首页 `.settings-index__row`（16 分类，两列） |

**TabPanel 用 `v-show` 不是 `v-if`**：切过的面板留在 DOM（opacity:0）。自检必须判 `.tab-panel-active` 或走 `checkVisibility`，不能数元素个数。

**宽屏分栏比例是全局共享的**（`localStorage['clawbench-widescreen-split-ratio']`，默认 0.5），所有左侧页签共用。拖 `.split-view__divider` 调整。不同面板需要不同宽度时分别拖——**注意这是改浏览器状态，不要改源码默认值**。

## 五、720p 的固有构图约束

- **设置首页 16 个分类**（每行 96px，两列）在 648px 可视高度里只能完整显示 13 个，必然截断。接受它，或用滚动分两张。
- **Git 提交列表**同理，只能显示前几条。

### 左栏必须收窄到 441px（用户明确要求）

**默认要求，不是可选项。** 宽屏三栏在 1280px 下：左栏 + 聊天 + 会话侧栏（固定 280px）。

| 左栏宽 | 聊天可见宽 | 评价 |
|---|---|---|
| 615–619px（第一版实际用的） | **334px** | ❌ 用户反馈「聊天消息显示很小」 |
| **441px（正确值）** | **510px** | ✅ 文件名 0 截断，聊天够宽 |

实测：左栏 441px 时，文件管理器 **338 个叶子节点 0 截断**（`scrollWidth > clientWidth` 计数为 0）、Markdown 预览 **922 个节点 0 截断**、表格 **183 个单元格 0 裁剪**（最长 `package-lock.json`、`CONTRIBUTING.en.md` 完整）。

**为什么第一版会跑到 615px**：分栏比例是**全局共享**的（`localStorage['clawbench-widescreen-split-ratio']`），默认 0.5；1280px 视口下容器 1232px，0.5 就是左 616 / 聊天 335。必须显式设置。

**正确设置方式——写 localStorage 比例 + reload**（不要用拖拽事件，脆弱且被 zoom 干扰）：

```js
// 容器 = (colRight.x + colRight.w) - colLeft.x，实测 zoom=2 下 2464 设备px → 1232 逻辑px
// ratio = 441 / 1232 = 0.3580
localStorage.setItem('clawbench-widescreen-split-ratio', String(441 / 1232));
```

**三个必须注意的时序陷阱**（都实测踩过）：

1. **测量必须在「视口 2560 + zoom=2」已生效之后**。若在 `setViewportSize` 之前测，视口还是默认 1280，容器算成 641，比例偏大一倍 → 左栏被拉到 848px、聊天只剩 103px。
2. **必须在 `.col-left` / `.col-right` 渲染完成之后测**。登录后立刻测会拿到 `no layout`，静默沿用旧比例。
3. **改完比例要再 reload 一次**——`useWideScreenLayout` 只在启动时读 localStorage。

比例生效范围：`clampRatio()` 会把左栏限制在 `[MIN_PANEL_WIDTH=320, container-320]`；441px 在此范围内，安全。

> 这是**改浏览器状态**，不要改源码默认值。

## 六、验收方法

**以 DOM 实测尺寸为主判据，视觉模型只作辅助。**

- 视觉模型（mmx vision）在小尺寸整图上**反复误判**：把右侧固定的 280px 会话侧栏当成「聊天区被遮挡」，报「用户消息被遮挡」而 DOM 实测重叠为 **0**；也把不存在的工具调用报成缺失。**不要据它下结论。**
- 可靠判据：`getBoundingClientRect()` 量各栏 x/width，算重叠；用 `scrollWidth > clientWidth` 判文本截断；用 `.filter(e => e.scrollWidth > e.clientWidth + 1)` 数截断条目。
- 视觉模型仍适合：判断「整体观感是否协调」「有没有明显异常」，以及在多张候选图之间做定性比较。

## 七、三段式文档结构

`docs/user-guid/user-guid.md`，中文单文件：

1. **快速上手** — 登录与多服务器、选择项目、界面总览（三栏/页签/会话侧栏）、发起第一次对话
2. **功能详解** — AI 对话（模式切换、斜杠命令、@引用、附件、权限审批、提问卡、推荐回复、分叉/回溯/续接）、会话管理、文件管理、Git 管理、终端、任务、Forge、RAG、用量统计、系统监控、设置
3. **进阶** — 快捷键速查、多项目与 worktree、上下文与摘要、推送通知、常见问题

图片放 `docs/user-guid/screenshots/`，按 `NN-module-name.png` 编号（编号与章节顺序对应）。**桌面端视角**：移动端专属功能（悬浮窗、手势返回、PWA 安装）不写或只作一句说明。

## 八、Vue 交互陷阱（第二版实测补充，最重要的一节）

给 Vue 3 应用做 playwright 截图时，以下陷阱会静默失败（无报错、图拍出来是错的）：

1. **`element.click()` 不触发 Vue 的 Teleport/响应式渲染，必须用 Playwright 真实鼠标点击。**
   - 实测：`document.querySelector('.toolbar-btn').click()` 后 `.toolbar-dropdown` 始终不存在；换成 `const el = await page.$(sel); await el.click()` 立刻出现。
   - **Why:** 菜单用 `<Teleport to="body">` + `v-if`，合成事件在 Vue 的调度链路上不完整。
   - **How to apply:** 任何展开菜单/抽屉/弹窗的操作，一律用 Playwright 的 `el.click()`，不要用 `page.evaluate` 里调 `.click()`。

2. **点击「已激活」的 Dock 页签会折叠左栏**（VS Code 行为）。左栏宽度变 0，截图里只剩聊天区，**且无任何报错**。切页签前必须判断 `b.classList.contains('active')`。

3. **`page.evaluate` 里不能用 `arguments`**；**`new Function()` 不支持 async 函数体**。需要异步就用 `async () => {...}` 箭头函数字符串。

4. **Node 作用域没有 `document`**。`run-code` 的函数体在 Node 里跑，所有 DOM 访问必须包进 `page.evaluate`。典型错误：把 dock 查找写在 `run-code` 顶层 → `ReferenceError: document is not defined`。

5. **浮层遮罩拦截点击。** 完成通知卡的 `.completion-popover-backdrop` 是全屏 fixed 遮罩，Playwright 报 `intercepts pointer events` 并重试。解法：`el.click({ force: true })` 或先清空通知队列。

6. **完成通知是队列且可能持续涌入。** 若同时有别的 agent 在完成任务，通知会不断新增。清 15 个后可能又冒出新的。

7. **`title` 属性是 i18n 文本，且部分按钮 title 与直觉不符。** 实测：网格视图按钮 title 是「图标视图」（不是「切换为网格视图」）、Git 管理入口 title 是「管理」、预览模式 title 是「开启预览模式：单击文件快捷预览」。**先探测实际 title 再写选择器**。

8. **设置分类进入后没有 `.settings-index__row`。** 分类列表在子页消失，脚本必须先用返回按钮回首页，否则第二次点击分类静默失败。

9. **浏览器会话会中途断开**（报 `Browser 'ug' is not open`）。脚本要能重开 + 重登 + 重设主题。**登录后主题会被重置为 `github-light`**（`index.html` 按 `prefers-color-scheme` 判定），必须再写一次 `clawbench-settings-theme` 并 reload。

10. **`@` 提及菜单在此环境不弹出。** 已确认输入框值正确为 `@`、光标在末尾，但 `.completion-menu` 始终不出现（斜杠 `/` 菜单正常）。疑似依赖 `caretVersion` 的事件链在合成输入下不触发。**不要在这上面耗时间**。

11. **按 Escape 不一定能关抽屉**（尤其 `.bs-overlay`）。可靠做法：点遮罩（`.bs-overlay`）或调 `document.body.click()`，然后断言 `document.querySelectorAll('.bs-overlay')` 可见数为 0。

## 九、逐子功能取景清单（第二版）

按模块细化，每个大模块下的子功能都要单独出图：

**文件管理器**：工具栏 / 排序菜单 / 多选模式 / 网格视图 / 文件右键菜单 / 目录右键菜单 / 预览窗格 / 搜索三模式
**文件查看器**：Markdown+TOC / 代码编辑 / 图片灯箱 / PDF / Office / 分享弹窗
**AI 对话**：输入栏全貌 / 斜杠命令菜单 / @引用 / 附件抽屉 / 消息操作按钮 / 推荐回复 / 工具卡展开 / 权限审批 / 提问卡 / 深度思考 / 会话设置抽屉
**会话**：侧栏全貌 / 搜索抽屉 / 标签过滤 / 右键菜单
**Git**：提交列表 / 分支 / 标签 / 工作树 / diff 抽屉 / 工作区变更
**任务**：列表 / 定时表单 / 事件表单（含事件类型勾选）/ 详情 / 执行历史
**Forge**：动态 / 议题 / 合并 / 流水线 / 详情 / 绑定弹窗
**终端**：标签栏 / 虚拟键工具栏 / 主题选择器 / 键位配置 / 帮助抽屉 / 快速指令
**统计**：用量统计（含筛选与图表切换）/ 代码存量 / 代码增量
**设置**：**16 个分类逐个出图**（外观/项目与文件/聊天/智能体/终端/语音朗读/语音识别/AI 摘要/会话搜索/端口映射/内网穿透/GitHub GitLab 集成/推送通知/安全/调试/关于）

## 十、设置分类截图脚本要点

设置面板有「首页（分类列表）」与「分类子页」两种状态。可靠流程：

```js
// 1. 打开设置（已激活则不重复点）
const b = [...document.querySelectorAll('.wide-dock .dock-btn')].find(x => x.title === '设置');
if (b && !b.classList.contains('active')) b.click();
// 2. 若不在首页，先返回
if (!document.querySelector('.settings-index')) {
  document.querySelector('.settings-page__header button')?.click();
}
// 3. 点分类行（用 startsWith 匹配，文本含计数）
[...document.querySelectorAll('.settings-index__row')]
  .find(x => (x.textContent||'').trim().startsWith(name))?.click();
```

## 十一、截图目录与引用校验

- 输出到 `docs/user-guid/screenshots/`，按 `<模块前缀>-<序号>-<名称>.png` 命名。
- **写完文档必须校验引用**：用 `comm` 双向比对文档里 `grep -o` 出的路径与实际文件，确认**无缺失、无冗余**。
- 迭代过程中会留下被新图取代的旧图，及时删除未引用项。

## 十三、测试素材库（补拍各类型文件预览用）

**`test/` 目录下有全套可直接预览的素材，不要自己造文件：**

| 目录 | 内容 |
|---|---|
| `test/office/` | `docx_sample1-4.docx`、`xlsx_sample1-3.xlsx`、`pptx_sample_1mb.pptx` 等 |
| `test/pdf/` | `sample-local-pdf.pdf`、`pdf-with-toc.pdf` |
| `test/media/` | `voice-demo.mp4`、`voice-demo.mp3` |
| `test/images/` | 多张 jpg + svg |
| `test/markdown/` | `table-demo.md`、`formula-demo.md`（LaTeX）、`mermaid-demo.md`、`images-demo.md`、`code-block-demo.md` |
| `test/openapi/` | `petstore.yaml` 等 |
| `test/excalidraw/` | `demo.excalidraw` |

**这些是人工测试文件，只读使用，绝对不要删除或修改。**

### 打开指定文件的可靠流程

1. 先关查看器：`.file-header-back-btn`（否则无法导航）
2. 切到文件管理器（已激活则不重复点）
3. **点面包屑第一段回项目根**（否则从当前目录出发找不到目标路径）
4. 逐级双击目录进入
5. 双击文件打开

**`data-path` 是相对项目根的完整路径**（如 `test/office/demo1.docx`），进入子目录后不会变成裸文件名。匹配用 `dp === name || dp.endsWith('/' + name)`。

### 各类型的 DOM 判定（用于断言渲染成功）

| 类型 | 判定 |
|---|---|
| PDF | `[class*=pdf]` 存在且 `.file-viewer canvas` 存在 |
| Office | `[class*=office]` 存在 |
| Excel | `.file-viewer canvas` 存在 |
| 媒体 | `.file-viewer audio` 或 `video` 存在 |
| OpenAPI | `[class*=openapi], [class*=swagger]` 存在 |
| Excalidraw | `iframe[src*=excalidraw]` 存在 |
| Mermaid | `.file-viewer svg` 数量 > 0（实测 31 个） |

## 十四、顶栏下拉菜单（不是 bs-overlay）

顶栏的项目切换、分支切换用 `AppMenuPanel` 渲染，类名是 **`.app-menu`**，**不是** `.bs-overlay`。用 `.bs-overlay` 判可见性会永远为 false。

另外顶栏按钮常被**完成通知遮罩挡住**——实测 `document.elementFromPoint()` 在项目按钮坐标处返回 `completion-popover-backdrop`。此时 `el.click()` 无效，需 `el.click({ force: true })`。

## 十六、快捷发送 / 快捷指令（两处易混淆的入口）

| 功能 | 存储表 | 入口 | 菜单类名 |
|---|---|---|---|
| **聊天快捷发送** | `chat_quick_send` | **清空输入框后点 `.chat-send-btn`** | `.quick-send-item`（在 `.popup-menu` 内） |
| **终端快捷指令** | `terminal_quick_commands` | `button[title="快捷指令"]` | 同上 |

两者菜单结构相同：条目列表 + 末尾「编辑」项。点「编辑」打开各自的 `.bs-overlay` 抽屉（`bs-overlay-wide-auto`），抽屉内含拖拽手柄（≡）、右上角「+」新增、「✨」消息聚类（仅聊天）、「⋮」导入导出。

**踩坑：**

1. **发送按钮常被浮层挡住**——实测 `elementFromPoint` 在按钮坐标返回 `ctx-overlay`（右键菜单遮罩）、`modal-overlay`（如「设置标签」弹窗）、`completion-popover-backdrop`（完成通知）。**必须先清这些遮罩**，否则点击静默无效。
2. **`page.evaluate` 里调 `.click()` 有时能开菜单（快捷指令），有时不能（快捷发送）**——不确定时两条路都试：先试 `page.evaluate` 内的 `.click()`，不行再试 Playwright 的 `el.click()`。
3. **不要点菜单项后立刻断言**——菜单是 Teleport 渲染，需 `waitForTimeout(1500+)`。
4. **终端快捷指令按钮在页面里有 3 个同名 `button[title="快捷指令"]`**（顶栏 + 工具栏重复渲染），取 `[0]` 即可。

## 十七、会话标签设置（弹窗 + 过滤栏）

### 弹窗

入口：会话条目**右键 → 设置标签**。弹窗容器是 **`.session-tags-dialog`**（在 `.modal-overlay` 内），**不是** `.bs-panel`/`.bs-overlay`——用后者判可见性会误判为「弹窗没打开」。

内部结构：

| 元素 | 选择器 | 说明 |
|---|---|---|
| 已有标签卡片 | `.st-chip` | 选中态加 `active` 类；名称在 `.st-chip-name`，删除按钮是卡片内的 icon |
| 新建标签输入框 | `.session-tags-dialog input` | 输入后点「+ 创建」 |
| 范围选择 | 按钮文本「仅本项目」/「全局」 | 默认「仅本项目」 |
| 底部 | `.modal-footer` 内的「取消」/「确定」 | 拍完记得点取消，避免写入 |

**可靠流程（单次 `run-code` 内完成，分步做会被 `snap.sh` 的 zoom 重设打断）：**

```js
// 1. 用 dispatchEvent 开右键菜单（Playwright 的 click 会被可操作性等待卡住）
await page.evaluate(() => {
  const row = document.querySelector('.session-row');
  const r = row.getBoundingClientRect();
  row.dispatchEvent(new MouseEvent('contextmenu', {
    bubbles: true, clientX: r.x + 30, clientY: r.y + r.height / 2, button: 2,
  }));
});
await page.waitForTimeout(1200);
// 2. 点「设置标签」（element.click 对 Teleport 菜单有效）
await page.evaluate(() => {
  [...document.querySelectorAll('.context-menu-item')]
    .find(x => (x.textContent||'').trim() === '设置标签')?.click();
});
await page.waitForTimeout(2200);
// 3. 选中 chip（element.click 有效，Playwright 的 el.click 会被拦截）
await page.evaluate(() => document.querySelector('.session-tags-dialog .st-chip')?.click());
```

**要点：**
- 右键菜单项是 `.context-menu-item`（文本「置顶 / 重命名会话 / 设置标签 / 归档」），**没有「删除会话」**。
- 会话行上用 Playwright `el.click()` 会被拦截（行内有 hover 才出现的按钮），但 `el.click({force:true})` 又**不会派发真实事件**导致菜单不出现 → **用 `dispatchEvent` 开右键菜单**是唯一稳定路径。
- 分步操作时弹窗会消失：`snap.sh` 每次都会重设 `zoom`，触发 Vue 重渲染。**打开 → 选中 → 截图必须在同一次 `run-code` 里**。

### 过滤栏

- 数据源 `GET /api/ai/session/tags?inUse=1&project_path=...`，只返回**未归档会话正在使用**的标签。
- **若返回空数组，过滤栏就该隐藏**——这是正确行为，不是缺陷。此时截不到过滤栏，不要反复尝试。
- 要拍到过滤栏，必须有一个**未归档且带标签**的会话。当前库里那条带标签的会话是 `archived=1`（它本身就是标签功能的实现会话），所以拍不到。
- **归档是单向的**（`DELETE /api/ai/session/archive`，无「取消归档」端点），所以不要为了截图去改归档状态。

### 数据安全

标签相关的**全部操作都在真实库上**（`session_tags` / `session_tag_links`）。截图时：
- 只**打开弹窗**看，不点「确定」——点「取消」关闭，数据零改动。
- 验证方式：操作前后比对 `select count(*) from session_tags` 与 `session_tag_links`，并确认没有新增输入的标签名。

## 十八、文件导出与分享

### 入口

导出与分享**都在文件查看器的「更多」菜单里**（工具栏 `button[title="更多"]`）：

| 菜单项 | 说明 |
|---|---|
| 文件详情 / 打开目录 / 文件历史 | 常规 |
| **分享链接** | 打开 `.modal-dialog`（`ShareLinkDialog.vue`） |
| **导出 HTML** | 仅 Markdown 文件可用 |
| 删除 | 危险操作 |

菜单容器是 **`.file-header-dropdown-menu`**，项是 **`.dropdown-item`**。打开方式：先点「更多」，再点菜单项（`element.click()` 对这类菜单有效）。

### 分享弹窗的两种状态（要分别出图）

`ShareLinkDialog` 按 `linkUrl` 是否存在渲染两套 footer：

| 状态 | 关键选择器 | 按钮 |
|---|---|---|
| **未生成** | `.share-dialog-hint`（说明文案） | 「生成链接」（`.fbtn-primary`） |
| **已生成** | `.share-dialog-link-input`（只读链接框） | 「打开页面」+「关闭分享」（`.fbtn-danger`） |

- 链接栏内还有两个图标按钮：复制（`.share-dialog-link-btn`）与重新生成（同上，第二个）。
- 生成链接会**真实写入 `file_shares` 表**，记得拍完撤销（见下）。

### 分享页面（独立 SPA）

链接形如 `https://<host>/share/<32位token>`。用**独立浏览器会话**打开（未登录态才能体现「无需登录」）：

```bash
playwright-cli -s=sh open --browser=chrome "https://<host>/share/<token>"
SESSION=sh ./snap.sh "share-page"
```

页面 `document.title` 是 **`ClawBench Share`**，可按此断言。只读渲染 + 右侧 TOC + 下载入口。

### 已分享文件抽屉

入口：文件管理器工具栏 `button[title="已分享文件"]`。容器是 `.bs-panel`，内部：

| 元素 | 选择器 |
|---|---|
| 一键清空 | `.shared-files-clear` |
| 单条复制链接 | `.shared-file-btn` |
| 单条取消分享 | `.shared-file-btn.danger` |

### 撤销分享（必做，恢复数据）

**UI 上的「取消分享」按钮经常点不动**——实测 `element.click()` 无效，Playwright `el.click()` 报 `intercepts pointer events`。查 `document.elementFromPoint()` 发现是 **`.completion-popover-backdrop`（完成通知遮罩）** 挡着。

**可靠做法是直接调 API**（与 UI 同一端点）：

```js
// DELETE /api/share?path=<绝对路径>   —— 注意是 query 参数 path，不是 body 里的 token
await page.evaluate(async () => {
  const res = await fetch('/api/share?path=' + encodeURIComponent('/abs/path/to/file'), {
    method: 'DELETE', credentials: 'include',
  });
  return res.status; // 200 + {"ok":true}
});
```

**踩过的坑**：我最初按 body `{token}` 提交，返回 400 `缺少路径`（`MissingPath`）。读 `file_share.go` 的 `shareRequestPath` 才确认 **DELETE 走 query `path`，POST/PUT 才读 body**。

### 数据安全

分享是**写入真实库**的操作（`file_shares` 表）。流程：

1. 拍前记基线：`select count(*) from file_shares;`
2. 生成链接 → 拍弹窗与分享页 → 拍抽屉
3. **撤销并验证回到基线**
4. 校验：`select count(*), group_concat(name) from file_shares;`

`file_shares` 的列是 `token / path / name / created_at / root`（**没有 `id`**，别按 id 查）。

## 十九、标注与跳转系统（截图要点）

四类标注的 DOM 选择器与触发条件：

| 类型 | 选择器 | 点击后 | 触发前提 |
|---|---|---|---|
| 文件路径 | `.chat-file-path[data-path-type="file"]` | 打开查看器并定位行 | `data-path-type` 必须是 `file`/`dir`（路径已验证存在） |
| 代码链接预览 | 同上 | 弹 `.code-preview-*` 浮层 | **必须先开「预览模式」** |
| Commit | `.chat-commit-open-btn[data-commit-sha]` | 切项目历史 + 定位提交详情 | — |
| Worktree | `.chat-worktree-btn[data-worktree-path]` | 打开 worktree 文件 | — |
| localhost | `.chat-url-open-btn[data-url][data-port]` | 建隧道 + 开 WebView | — |

**关键坑：**

1. **代码链接预览的 `enabled` 门控在「预览模式」上，不是全局设置。** `FileManagerContent.vue:900` 传的是 `enabled: computed(() => filePreviewMode.value)`。不开启预览模式，点路径只会跳查看器，浮层永远不弹。**必须先点** `button[title="开启预览模式：单击文件快捷预览"]`。
2. **浮层根类名不是 `.code-preview-sheet`**（那是移动端 BottomSheet 模式的类）。PC 下实际渲染的是 `.code-preview-header` / `.code-preview-line-row` / `.code-preview-line-code` 等**没有统一根类**的一组元素。判断浮层是否弹出用 `document.querySelectorAll('[class*=code-preview]').length > 0`，不要找单一根节点。
3. **标注元素在虚拟滚动容器里会被回收**——`el.scrollIntoView()` 后立刻取 `getBoundingClientRect()` 可能拿到 `{x:0,y:0,w:0,h:0}`。滚动与取坐标要在同一次 `page.evaluate` 内完成，或直接用 Playwright 的 `el.click()`。
4. **聊天区可能被折叠**（`colRight` 宽 0），此时聊天内所有元素尺寸为 0、点击无效。检查 `.wide-dock-bottom .dock-btn` 的 `aria-pressed`，必要时先点它展开。
5. **commit 标注点击用「页面内派发 click」最可靠**：`el.dispatchEvent(new MouseEvent('click',{bubbles:true,clientX,clientY}))` 能成功触发跳转；纯 `page.mouse.click` 在虚拟滚动下常落空。
6. **标注管线在流式期间不运行**——流式中只做纯 Markdown 渲染，回复结束才启动标注。所以截图前必须等流式结束。

## 二十、踩坑速查（汇总）

1. `offsetParent` 判浮层 → fixed 元素恒 null，**完全失效**（第一版最重要的一条）
2. 升级弹窗延迟出现 → 检查与截图必须同一 evaluate
3. 完成通知是队列 → 循环关到空
4. CDP `deviceScaleFactor` 不生效 → 用 zoom 超采样
5. 登录页 `overflow:hidden` → 不能滚动，只能降 zoom
6. 截图存进项目目录会污染工作区 → 先放 `/tmp`，定稿再拷
7. 主工作区有其他 agent → 截图里会出现他们的会话与改动
8. `playwright-cli eval` 里不能用 `arguments` → 用 python 生成字面量
9. 会话行点击目标是 `.session-item`（不是 `.session-row`）
10. `run-code` 里的循环必须写在 `page.evaluate` 内
11. **`element.click()` 不触发 Teleport 渲染 → 用 Playwright `el.click()`**
12. **点已激活的 Dock 页签会折叠左栏（无报错）**
13. **设置子页没有分类列表 → 先返回首页**
14. **登录后主题被重置为 light → 重写 localStorage 并 reload**
15. **打开文件前先关查看器 + 回项目根**，否则目录导航静默失败
16. **顶栏菜单是 `.app-menu` 不是 `.bs-overlay`**
17. **完成通知遮罩会挡住顶栏 → 用 `click({ force: true })`**
18. **Git 提交行是 `.drilldown-item`**，点第一项会进「工作区变更」而非提交；点索引 1+ 才是真实提交
19. **PC 布局下终端没有「工具栏配置」和「终端操作帮助」按钮**（仅移动端布局有），不要照文档硬找
20. **视觉模型（mmx）在小尺寸整图上频繁误判** —— 实测把已渲染的 Mermaid 报成「聊天内容」、把视频播放器报成「静态图片」、把提交详情报成「文件树」。**以 DOM 实测为准**，mmx 只用于「整体观感是否协调」这类定性判断。
21. **聊天快捷发送入口是「空输入时点发送按钮」**，不是单独的按钮；终端快捷指令按钮有 3 个同名元素
22. **点菜单/抽屉前先清 `ctx-overlay`、`modal-overlay`、`completion-popover-backdrop` 三种遮罩**，否则点击静默失败

### 第二版补充（返工实测）

23. **完成通知卡片：截图前 `page.reload()`**（首选手段）——卡片是内存队列，reload 即清；`App.vue:1209` 跳过 replay 的历史 completed 事件，故不会复活。第一版三个脚本没做这件事，交付了 **22 张带卡片的图**。判据见 §二（像素级 x=300/979, y=110）。**交付前对全部图跑一遍检测。**
24. **左栏默认收窄到 441px**（聊天 510px）。默认比例 0.5 在 1280 视口下是左 616 / 聊天 **334px**，太窄。设置方式＝写 `localStorage['clawbench-widescreen-split-ratio'] = 441/1232` 再 reload；**测量必须在 zoom=2 且三栏已渲染之后**。见 §五。
25. **`cap.sh` 式的多命令往返在本机不可靠** —— 有 11+ 个并发 `codebuddy --acp` agent（各 ~750MB），可用内存常 < 5GB，playwright 的 chrome 会在两条命令之间被 OOM 杀掉（`Browser 'ug' is not open` / `Session closed`）。**解法：把 open → 登录 → 设置 → setup → 截图压进一次 `run-code`**，并内置重试。
26. **登录判定必须用 `/api/me` 而非 DOM**。用 `input[type=password]` 判会在已登录时误判 → `page.click('button.login-btn')` 等 30s 超时 → **整个 evaluate 从未执行，截图落成上一轮残留图**（实测 5 张设置图 md5 完全相同）。并且**产出侧要加防线**：setup 返回错误字符串 / `ov.card===true` 时给文件名打 `[NEEDS REVIEW]`。
27. **`page.evaluate` 的页面上下文没有裸 `setTimeout`** —— 必须 `window.setTimeout`。统一在 setup 包装里注入 `const sleep = ms => new Promise(r => window.setTimeout(r, ms))`，否则每段 setup 各自声明会撞 `Identifier 'sleep' has already been declared`。
28. **右键菜单只认「图标或名称」命中区** —— 在行的 padding / 元信息列上右键会落到**空白区菜单**（粘贴/新建文件/新建文件夹/在此打开终端，仅 4 项）。文件菜单 9 项、目录菜单 11 项。命中区选择器：`.file-icon-wrap, .grid-thumb, .file-name, .grid-name`。用 `.file-name` 派发 `contextmenu` 最稳。
29. **Git「管理」入口按钮 title 就是「管理」**（`git.manage.title`），点开后是「提交列表 › 管理」，内部三个标签：分支 / 标签 / 工作树。`git-02/03/04` 分别是这三个标签页。
30. **终端 Dock 页签会因状态未加载而缺席** —— `isTerminalDisabled` 来自 `/api/terminal/status` 的异步结果；页面长期未刷新时该页签不渲染。**reload 后即正常出现**。实测终端按钮有 3 个同名 `button[title="快捷指令"]`。
31. **主题必须显式钉成 `github-dark`**（新 profile 默认 `auto` → 跟随系统亮色，整片白底）。见 §一。
32. **交付前检查图片唯一性** —— `md5sum *.png | awk '{print $1}' | sort | uniq -d`。第二版发现 `annot-04-filepath-jump.png` 与 `share-01-dialog-initial.png` **md5 完全相同**（既有错配：前者应为「文件路径跳转后的查看器」，实际却是分享弹窗），已重截修正。**"文件数对得上"不代表内容对**。

---

## 二十一、移动端截图参数（CDP 设备模拟）

**这是第三版最重要的发现：桌面端的 zoom 超采样对移动端完全无效。**

### 为什么 zoom 方案在移动端失效

移动端/桌面端的判定依据是 **CSS 视口宽度**（`useWideScreenLayout.ts:8` 的 `WIDE_SCREEN_MIN_WIDTH = 1024`）：

```js
// web/src/composables/useWideScreenLayout.ts:50
export function computeIsWideScreen(cssWidth, screenWidth, screenHeight, dpr) {
  const physicalWidth = cssWidth * (dpr || 1)
  return cssWidth >= WIDE_SCREEN_MIN_WIDTH
    || (physicalWidth >= WIDE_SCREEN_MIN_PHYSICAL_WIDTH && screenWidth > screenHeight)
}
```

实测：`documentElement.style.zoom = '3'` 之后 `window.innerWidth` **仍是 1170**（≥1024）→ 布局依然判宽屏（`wideScreen:true, bottomDock:false`）。zoom 只改变渲染尺寸，不改变 CSS 视口。

### 正确方案：Playwright 原生移动端 context（`isMobile: true`）

**唯一正确的方法**——这是 DevTools「设备模式」的等价物：

```js
const ctx = await browser.newContext({
  viewport: { width: 390, height: 844 },
  deviceScaleFactor: 3,
  isMobile: true,          // ← 关键：启用移动端语义
  hasTouch: true,          // ← 关键：触摸事件
  userAgent: ANDROID_UA,
  storageState: '/tmp/uidesc/mobile-state.json',   // 复用登录态
});
const p = await ctx.newPage();
await p.goto(URL, { waitUntil: 'load' });
await p.screenshot({ path: '/tmp/uidesc/_raw_m2.png' });   // → 1170×2532
```

实测：`innerWidth:390, bodyW:390, dpr:3, isMobile:true(maxTouchPoints>0)`，
`page.screenshot()` 输出 **1170×2532**。

| 项 | 值 |
|---|---|
| CSS 视口 | **390 × 844**（iPhone 14 逻辑尺寸） |
| DPR | **3** |
| 截图原生尺寸 | **1170 × 2532** |
| 后处理 | LANCZOS 降到 **585 × 1266**（原生的一半） |
| 单图体积 | 约 140–290 KB（量化后） |

> 登录态复用：先用已有会话 `page.context().storageState({ path })` 导出，
> 新 context 用 `storageState` 载入，省去每次登录。

### ⚠️ 最致命的坑：CDP 覆盖 ≠ 移动端（`isMobile` 仍是 false）

**症状**：比例是竖屏、`innerWidth` 也对，但**元素异常小、底部导航栏不可见**，
整体像「桌面端界面被压进竖屏」——用户一眼看出不对。

**根因**：用 `setViewportSize` + CDP `Emulation.setDeviceMetricsOverride` 只改了
「度量」，**`isMobile` 仍是 false、`maxTouchPoints` 仍是 0**。
页面的移动端媒体查询（`@media (hover: none)` / `pointer: coarse`）和触摸语义
**全都没生效**，渲染出的仍是桌面布局。

**A/B 实测对照**（同一页面、同样 390×844）：

| 方法 | `isMobile` | 底部导航栏 | 元素比例 |
|---|---|---|---|
| `setViewportSize` + CDP override | **false** | **不可见** | 异常小 |
| `browser.newContext({ isMobile:true, hasTouch:true })` | **true** | 可见 | 正常 |

**判据速记**：`innerWidth` 对 ≠ 移动端。**必须断言 `navigator.maxTouchPoints > 0`。**

### ⚠️ 第二个坑：截图输出尺寸没被断言

`page.screenshot()` 截的是**视口**，若窗口尺寸与设备像素不一致，
布局按 390 算、却被拉伸填满更宽的画布 → 元素等比放大。
**后处理阶段必须硬断言原始截图尺寸**：

```python
raw_im = Image.open('/tmp/uidesc/_raw_m2.png')
EXPECT = (1170, 2532)          # = CSS 390×844 × DPR 3
if raw_im.size != EXPECT:
    print(f"ERROR: raw {raw_im.size} != {EXPECT}", file=sys.stderr)
    bad = True                  # 打 [NEEDS REVIEW]，绝不静默交付
```

### 三个必须注意的细节

1. **UA 必须覆盖**。不覆盖则 `isPC` 仍为 true（`usePlatformDetect.ts:55` 的
   `isPC = !isAppMode && !AndroidUA && !iOSUA && !iPadOSUA`），
   文件管理器单击行为、终端音量键等移动端分支不会生效。
2. **登录态用 `storageState` 复用**。先用已有会话导出
   `page.context().storageState({ path })`，新 context 传 `storageState` 载入。
   否则每个 context 都要重登，慢且易失败。
3. **App 模式 stub 用 `ctx.addInitScript`**。注入 `window.ClawBenchNative` 后
   `useAppMode` 会置 `<html data-app-mode>`、渲染 appOnly 卡片。
   **context 用完即 `close()`，天然无泄漏**——这比 v1 的 `page.addInitScript` + `dispose()`
   干净（v1 曾因忘记 dispose 污染后续所有 web 截图）。

### 移动端构图是否正确的客观判据（DOM 实测，别靠肉眼/视觉模型）

跑一次 `capm2.sh` 断言这几个值，**全部落在区间内才算对**：

| 指标 | 正确值 | 说明 |
|---|---|---|
| **`isMobile`** | **true**（`maxTouchPoints > 0`） | **最关键**；false 说明没用原生移动 context |
| `innerWidth` | **390** | CSS 视口宽 |
| `bodyW` | **390** | body 布局宽度，>390 说明被拉伸 |
| `wideScreen` | **false** | 必须是窄屏布局 |
| `bottomDock` | **true** | 底部 Dock 可见 |
| `.bottom-dock-wrapper` 高度 | **48px ≈ 5.7% 屏高** | iOS TabBar 标准 49px，吻合 |
| `.bottom-dock .dock-btn` | **34px** | 触摸目标尺寸正常 |
| 原始截图尺寸 | **1170 × 2532** | = CSS × DPR |

> **两个反例特征**：
> 1. `isMobile:false` → 元素异常小、底部 Dock 不可见（桌面布局被压进竖屏）。
> 2. 原始尺寸 ≠ 1170×2532 → 布局被拉伸，元素等比放大。

> **注意**：视觉模型（mmx）在判断「像不像手机 App」时**频繁幻觉**——实测反复把单栏读成「三栏布局」、
> 把 8 个 Dock 图标说成「3 个」、把文件管理器说成「会话列表」。**构图正确性一律以 DOM 实测为准**，
> mmx 只在「整体观感」层面做参考，且需要与真机图并排对比才有意义。

### 图片压缩：自适应量化

UI 截图颜色少，**256 色量化近乎无损**（实测边缘能量仅降 2.6%），能把 556KB 压到 252KB。但照片类图会明显劣化，故按唯一色数自适应：

```python
im = Image.open(raw).convert('RGB').resize((585, 1266), Image.LANCZOS)
im.save(out, optimize=True)
uniq = len(im.getcolors(maxcolors=200000) or [])
if os.path.getsize(out) > 300*1024 and uniq < 60000:   # UI 截图
    im.quantize(colors=256, method=Image.MEDIANCUT).save(out, optimize=True)
```


实测：`uniq≈22000–35000`（UI）会量化；`uniq≈48701`（含照片）跳过。

### 工具：`/tmp/uidesc/capm2.sh`

```
./capm.sh <输出名> [setup] [等待ms] [模式=web|app] [设备=phone|phone-l|tablet]
```

沿用 cap3.sh 的「内置重试 + 产出侧 `[NEEDS REVIEW]` 防线」，核心是 `browser.newContext({ isMobile:true, hasTouch:true, deviceScaleFactor:3 })`。`recapm2.sh` 提供批量重截驱动。

---

## 二十二、移动端断点与布局事实

### 两个反直觉点（文档必须写准，否则误导用户）

1. **窄屏主面板是占满全屏的 `TabPanel`，不是 BottomSheet。**
   `TabPanel.vue:61` 是 `position:absolute; inset:0`。`SplitView :enabled="isWideScreen"`（`App.vue:73`）在窄屏下让 `.split-view:not(.split-view--active) > *` 变 `display:contents`（`SplitView.vue:159-162`），左右列回到普通流；左列 `v-show="isWideScreen || activeTab !== 'chat'"`（`App.vue:80`）、右列 `v-show="isWideScreen || activeTab === 'chat'"`（`App.vue:241`）互斥切换。
   **BottomSheet 只用于二级抽屉**（会话搜索/附件/TOC/工具详情/按键配置等 34 处）。

2. **`chat` 不在 `DOCK_TABS` 注册表。**
   `dockTabs.ts:35` 是 `DockTabId = Exclude<TabId, 'chat'>`；`chat` 是 `App.vue:368` **硬编码的底部 Dock 首项**。宽屏下它反而是 `.wide-dock-bottom` 的独立开关（`App.vue:64-68`）。所以窄屏与宽屏**不是同一份完整注册表**，只有二级项共享 `DOCK_TABS`。

### 底部 Dock 顺序与角标

顺序（`App.vue:368-410`）：**会话 → 文件管理器 → 文件 → 项目历史 → 溢出项 → 更多**

| 页签 | 角标来源 |
|---|---|
| 会话 | `chatUnreadCount` |
| 项目历史 | `gitWorkingTreeChangeCount` |
| 议题与合并请求 | `forgeUnreadCount` |
| 任务 | `taskUnreadCount` |
| 终端 | `terminalSessionCount` |
| 端口映射 | `portForwardEnabledCount` |
| 更多 | 聚合剩余（`overflowBadgeCount`） |

溢出由 `useDockOverflow` 按 ResizeObserver 动态决定（按钮 34px + 间距 12px，主项 4 个）；剩 1 项内联，>1 项收进 Teleport 弹菜单。

> **截图注意**：「设置」页签常被收进「更多」溢出菜单。`recapm.sh` 的 `dock`/`setcat` 已内置「找不到就展开更多」的兜底，否则会返回 `dock not found: 设置`。

### BottomSheet 的两种形态

| 窄屏 | 宽屏 |
|---|---|
| overlay `align-items:flex-end`，panel 底部弹出、顶部圆角（`BottomSheet.vue:192-217`） | `bs-wide-auto` → 居中卡片（`modal-card.css:11-30`），隐藏拖拽手柄（`:390`） |

拖拽手柄 `.bs-handle` 32×4px（`:287`）；整个 `.bs-header` 可点击关闭（`:21`）。

---

## 二十三、移动端专有交互的截图要点

### 手势阈值（源码实测值）

| 手势 | 阈值 | 作用 | 源码 |
|---|---|---|---|
| 边缘滑动返回 | 右缘 **20px** 内起滑、**≥50px**、**<400ms**、纵≤横×0.75 | 返回（web 派发 back-press；app 交给原生） | `useEdgeSwipeBack.ts:14-17` |
| 会话左右滑动 | **≥80px**、**<500ms**、横>纵×0.75 | 左=下一会话，右=上一会话 | `useSwipeSession.ts:118-119` |
| 输入框历史滑动 | 横 **≥60px**、<400ms，**仅 textarea 未聚焦** | 左=更旧，右=更新 | `ChatInputBar.vue:1209-1253` |
| 终端单指滑动 | ≥30px | 方向键 | `useTerminalGestures.ts:104` |
| 终端长按方向 | 500ms 后每 150ms 重复 | 连续方向键 | 同上 `:107-108` |
| 终端双击 | <300ms、位移<10px | `Tab` | 同上 `:109-110` |
| 终端双指捏合 | 距离变化 ≥10px | 字号 | 同上 `:106` |
| 终端双指竖滑 | 中心 ≥30px 同向 | `PageUp`/`PageDown` | 同上 `:105` |
| 通用长按 | **450ms**、位移<10px | 右键菜单 | `directives/longPress.ts:19-20` |
| 语音输入 | **长按发送键 500ms** | 录音（无独立麦克风按钮） | `ChatInputBar.vue:695-724` |

### 几个容易拍错/写错的点

1. **会话滑动默认关闭** —— 需在设置里开 `swipeSession`。文档里必须写明，否则用户以为坏了。
2. **语音输入没有独立麦克风按钮** —— 是**长按发送键**。拍图时拍不到「麦克风图标」，别硬找。
3. **文件管理器「点选中→再点进入」仅在开启预览模式时成立**（`FileManagerContent.vue:2002-2048`）。未开预览模式时单击不进入。
4. **消息操作条 `.chat-meta-bar` 在触屏常显**（`ChatMessageItem.vue:87-140`），不是 hover-only，也不是长按。桌面端才是悬停出现。
5. **软键盘**：`useChatKeyboard.ts:59-66` 用 `visualViewport` 算高度（iOS WKWebView 无 adjustResize），`App.vue` 用 `.chat-keyboard-open{bottom:高度}` 上推。Android adjustResize 由原生处理。
6. **文件管理器的 `data-path` 是相对当前目录的名字**（如 `test`），不是完整路径。逐级导航的匹配逻辑要按 `split('/').pop()` 比。
7. **窄屏下双击进目录不可靠** —— 用「点选中→再点」两次单击；且两次点击之间列表可能重渲染，需**每次重新查询元素**。更稳的做法是用面包屑逐级返回 + 工具栏的「预览模式」。
8. **斜杠补全菜单（`.completion-item`）在模拟环境难以触发** —— `parseSlashQuery` 依赖 caret 位置，直接设 `value` 不更新 caret；即便 `setSelectionRange` 后 `caret` 正确，菜单仍可能因 `isTextareaFocused` 门控不显示。**建议改用「附件抽屉」等按钮触发的浮层**，或把斜杠命令写进正文用文字讲清。

---

## 二十四、Android App 模式模拟（bridge stub）

### 判定机制

- `useAppMode.ts:13-27`：`isNativeApp()` 为真且是顶层 frame 时，`isAppMode = true` 并给 `<html>` 加 `data-app-mode` 属性。
- `usePlatformDetect.ts:55`：`isPC = !isAppMode && !AndroidUA && !iOSUA && !iPadOSUA` → **App 模式与手机浏览器都是 `!isPC`**，但能力不同。

### stub 注入法（已实测可用）

```js
const stub = await page.addInitScript({ content: `
  (() => {
    const noop = () => {}; const p = (v) => Promise.resolve(v);
    window.ClawBenchNative = {
      isNativeApp: () => true, getAppVersion: () => '1.0.0', getPlatform: () => 'android',
      getServerList: () => p([]), getPassword: () => p(''), getServerUrl: () => p(location.origin),
      setKeepScreenOn: noop, setVolumeKeyMode: noop, setNativePushEnabled: noop,
      setFloatingStatusEnabled: noop, setLiveUpdateEnabled: noop, requestFloatingPermission: noop,
      shareText: noop, shareFile: noop, shareFiles: noop, downloadUrl: noop, downloadBlob: noop,
      openUrl: noop, reloadApp: noop, log: noop, logBatch: noop, onBackPressed: noop, __stub: true,
    };
  })();
`});
```

实测：`data-app-mode` 属性出现，**appOnly 设置卡片渲染出来**（「桌面悬浮状态窗」「灵动岛」可见）。

> **必须 dispose！** `addInitScript` 注册在 **context** 上、跨导航持久生效。不销毁会让**后续所有 web 模式截图被污染成 App 模式**（实测踩过：m-02 一度 `appMode:true`）。`addInitScript` 返回 `Disposable`：
> ```js
> await stub.dispose();
> ```
> 若已泄漏，只能 `playwright-cli close` 后重开浏览器。

### appOnly 字段清单

| 设置项 | key | 位置 |
|---|---|---|
| 桌面悬浮状态窗 | `floatingStatusWindow` | `settingsFieldMap.ts:296` |
| 灵动岛 | `liveUpdate` | `settingsFieldMap.ts:297` |
| 重配服务器 | `reconfigureServer` | `settingsFieldMap.ts:262`（调试分类） |

渲染门控：`SettingsCategory.vue:204` 的 `if (entry.spec.appOnly && !isAppMode.value) continue`。

### 不可浏览器渲染的部分

**悬浮状态窗与灵动岛是 Android 原生层渲染**（`FloatingStatusView` / `LiveUpdateManager`），浏览器模拟无法产出 —— 只能拍设置页的开关卡片作为代理图，真机图需用户提供。文档里用**文字图注**说明，不要写 `![]()` 引用（否则链接 404）。

---

## 二十五、移动端验收判据

### 每张图必须断言（capm2.sh 已内置）

**运行期断言（DOM）**：

```js
{
  isMobile: true,        // maxTouchPoints>0；false 说明没用原生移动 context（最关键）
  innerW: 390,           // CSS 视口宽
  bodyW: 390,            // body 布局宽；>390 说明布局被拉伸
  wideScreen: false,     // 必须是移动端布局
  bottomDock: true,      // 底部 Dock 可见
  wideDock: false,       // 宽屏左侧 dock 不可见
  appMode: <按需>,        // app 模式应为 true
  card: false,           // 无完成通知卡片
  dockH: 48,             // Dock 高度；≈141 说明布局按 1170 算（异常）
  dockBtn: 34,           // Dock 按钮高度
}
```

**产出期断言（像素）**——**这一条比上面所有 DOM 断言都关键**：

```python
if Image.open(raw).size != (1170, 2532):   # CSS 390×844 × DPR 3
    bad = True    # → [NEEDS REVIEW]
```

> 只断言 `innerWidth` 会漏掉「布局窄、画布宽 → 元素被放大」的整批事故。
> **输出像素尺寸是唯一能证明 CDP 真正接管视口的判据。**

任一断言失败即打 `[NEEDS REVIEW]`，**绝不静默交付**。

### 交付前的构图抽检（可选，但推荐）

肉眼或视觉模型确认「像不像手机 App」不可靠（见 §二十一 的 mmx 幻觉记录）。
最稳的是**与真机图并排对比**：把 `docs/screenshots/home.png`（1080×2340 真机图）
与自己的图缩到同高并排，看两边 Dock 高度占比、列表行高是否接近。

若要做量化判据，量这三个 DOM 值即可（详见 §二十一 表格）：
`--dock-height`、`.bottom-dock-wrapper` 高度占比（应 ≈5.7%）、`.dock-btn` 尺寸（应 34×34）。


### 全量验收清单

| 项 | 命令/判据 |
|---|---|
| 尺寸一致 | 全部 `585×1266` |
| 唯一性 | `md5sum *.png \| awk '{print $1}' \| sort \| uniq -d` 为空 |
| 引用平衡 | `comm` 双向比对文档引用 ↔ 目录实文件 |
| 卡片残留 | 像素判据（见 §二）；移动端卡片几何与桌面不同，**不能照搬 x=300/979**，以 `ov.card` 为准 |
| 占位图 | 用文字图注，不写 `![]()`（避免 404） |

### 命名与目录约定

- 文档：`docs/user-guid/user-guid-mobile.md`
- 截图：`docs/user-guid/screenshots-mobile/`（与桌面 `screenshots/` 平级隔离，避免混入桌面引用校验）
- 命名：`m-NN-name.png`（`m-` 前缀 + 两位序号 + 语义名）

---

## 二十六、移动端补图：与 README 对齐的完整清单

**触发场景**：README 里已有的截图，移动端文档也要有对应版本。

### README 的 35 张图与移动端对应关系

| README 图 | 移动端文件 | 拍法要点 |
|---|---|---|
| login | m-01-login | 清 cookie 后拍（新 context 不传 storageState） |
| chat-interface / home | m-03-chat | 会话页 + 消息区 |
| project-select | m-19-project-menu | 点 `.project-switch-btn` |
| session-manager | m-35-session-manager | 会话列表（6 条） |
| file-browser | m-09-file-manager | dock「文件管理器」 |
| file-search | m-21-file-search | **必须先开「全局搜索」**，否则搜不到子目录 |
| code-editor | m-22-code-editor | 搜 `vite.config.ts` 并单击（`.cm-editor` 出现） |
| markdown-preview | m-23-markdown-preview | 打开 `AGENTS.md` |
| toc-drawer | m-24-toc-drawer | 查看器内点 `.file-header-btn[title="目录"]`，用**原生 click** |
| image-viewer | m-25-image-viewer | 进 `test/images` 打开 `img_chinese_beauty_001.jpg` |
| audio-player | m-37-audio-player | `test/media/voice-demo.mp3` |
| video-player | m-38-video-player | `test/media/voice-demo.mp4` |
| pdf-preview | m-26-pdf-preview | `sample-local-pdf.pdf` |
| mermaid-diagram | m-27-mermaid-diagram | `mermaid-demo.md`（`.mermaid` 出现） |
| latex-formula | m-28-latex-formula | `formula-demo.md`（`.katex` 出现） |
| terminal | m-12-term-keys | **必须执行真实命令**（见下） |
| terminal-key-config | m-13-term-keybar | 终端底部虚拟键栏 |
| git-history | m-10-git | dock「项目历史」 |
| git-branches | m-29-git-branches | 点「管理」→ 分支标签 |
| git-commit-detail | m-30-git-commit-detail | 点 `.drilldown-item[1]`（**索引 0 是「工作区变更」**） |
| git-comparison-report | m-36-git-working-tree | 点 `.drilldown-item[0]`（工作区变更，55 文件） |
| scheduled-tasks | m-31-scheduled-tasks | dock「任务」 |
| task-create | m-31-task-create | 点「新建任务」按钮 |
| rag-search | m-07-session-search | 点 `button[title="搜索会话"]` |
| system-monitor | m-33-system-monitor | dock「数据统计」（**不在溢出菜单里时直接点**） |
| settings-panel | m-14-settings-index | dock「设置」（**常在溢出菜单**） |
| port-forwarding | m-32-port-forwarding | dock「端口映射」 |
| agent-selector | m-34-agent-selector | 点 `button[title*="选择智能体"]` |
| pc-desktop | — | 桌面专属，跳过 |
| product_hero.en | — | 宣传图，跳过 |

**仍缺 4 张**（需真实对话交互，成本高）：
`quote-question`（选中文字弹引用条）、`conversation-recommendation`（AI 推荐回复）、
`acp-permission`（工具审批卡片）、`schedule-proposal`（聊天内任务建议卡片）。

### 终端截图必须执行真实命令

**空终端截图没有意义。** 必须让终端跑出可见输出：

```js
// 点 xterm 的 helper textarea 聚焦，再用真实键盘输入
const ta = await p.$('.xterm-helper-textarea');
await ta.click({ force: true });
await p.keyboard.type('ls -la && echo "=== 演示 ===" && git log --oneline -5', { delay: 25 });
await p.keyboard.press('Enter');
await p.waitForTimeout(4000);   // 等输出渲染
```

判据：`.xterm-rows > div` 行数应 > 20（实测 43 行），且能看到命令输出内容。

### 三个实测踩坑

1. **`input[type=text]` 要按 placeholder 筛**。页面里有 `input[type=file]`（上传控件），
   直接设 value 会抛 `InvalidStateError: This input element accepts a filename`。
   正确：`[...document.querySelectorAll('input[type=text]')].find(i => i.placeholder.includes('搜索'))`。

2. **文件搜索必须先开「全局搜索」**。默认 `recursive=false` 只搜当前目录一级，
   搜子目录文件（如 `test/markdown/mermaid-demo.md`）会返回 0 结果。
   按钮 title 是「全局搜索」，点一下让它 `active`。

3. **项目 cookie 会被误切**。`clawbench_project` cookie 决定搜索根目录。
   实测误点「文件管理器」里的目录后 cookie 变成了 `.../clawbench/android`，
   导致所有搜索都在 android 子目录里进行。**发现搜索结果异常时先查这个 cookie**：
   ```js
   const ck = await ctx.cookies();
   ck.find(c => c.name === 'clawbench_project').value   // 应为仓库根，URL 编码
   ```

4. **`.up-overlay` 会挡住点击**。升级提示浮层拦截 pointer events，
   导致 `elementHandle.click` 超时 30s。**必须先写 `clawbench-upgrade-skip`**：
   ```js
   const r = await fetch('/api/upgrade/check', { credentials: 'include' });
   const j = await r.json();
   localStorage.setItem('clawbench-upgrade-skip', j.latest_version);
   ```

### 移动端补充截图（第二批，8 张）

| 文件 | 内容 | 触发方式 |
|---|---|---|
| m-42-session-tag-dialog | 设置标签弹窗 | 会话 `contextmenu` → 点「设置标签」 |
| m-43-session-menu | 会话长按菜单（置顶/重命名/设置标签/归档） | `.session-row` 派发 `contextmenu` |
| m-44-file-menu | 文件长按菜单（9 项） | **命中区必须用 `.file-name`**，否则落空白区菜单（4 项） |
| m-45-file-multiselect | 多选模式（已选 N 项 + 批量栏） | 点 `button[title="多选"]`，再点条目 |
| m-46-slash-commands | 斜杠命令菜单（104 项） | **真实键盘** `page.keyboard.type('/')`，不能用 evaluate 设值 |
| m-47-tool-detail | 工具调用详情抽屉 | 点 `[class*=tool-call]` 卡片 |
| m-48-term-gesture-help | 终端操作帮助（手势说明） | 终端内 `button[title="终端操作帮助"]` |

**拍不到的（需真机/真实环境）**：

| 内容 | 原因 |
|---|---|
| 语音输入录音态 | `useVoiceInput.start()` 需要真实 `getUserMedia`，无头浏览器无音频设备 |
| PWA 安装提示 | `beforeinstallprompt` 在自动化环境不触发 |
| 悬浮窗权限弹窗 | Android 系统级对话框 |
| APK 安装器 | 系统 PackageInstaller |
| 系统分享面板 | Android ShareSheet |
| 音量键 / 返回键 | 硬件按键 |

**本轮新增的两个踩坑**：

1. **斜杠命令菜单必须用真实键盘**。之前用 `evaluate` 设 `textarea.value` + 派发 `input`
   事件始终不触发（`items:0`）；改用 `page.keyboard.type('/', { delay: 120 })` 立即成功
   （104 个候选项）。原因：`parseSlashQuery` 依赖 caret 与 `isTextareaFocused`，
   只有真实键盘事件才会同步更新这两者。

2. **语音输入的长按用的是 `pointerdown`/`pointerup`**（`ChatInputBar.vue:703-723`），
   不是 touch 事件，且条件为 `!hasInputContent.value`（空输入时）。
   但即使触发成功，`start()` 仍会因无麦克风而报错——**这张只能真机拍**。
