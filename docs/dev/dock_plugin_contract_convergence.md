# 界面端插件化契约收口方案

> 目标：**加一个界面插件，只改一处地方。**
>
> 改造前：加一个插件要动 5 处（前端）+ 1 处（服务端）。
> **本文已按实际实现修订**——落地过程中有三处与最初计划不同，见文末「与计划的偏差」。

---

## 0. 一句话目标

改完之后，加一个插件 = **写 `web/src/plugins/<id>/` 这个目录** + **服务端 `registerPlugins` 加一块**。

前端不再有第二处需要改。

> 服务端那一块能不能也收进同一个目录？不能：服务端和前端是两个编译单元、两种语言，物理上不可能共用一个文件。它已经是「一处」（`registerPlugins` 一个函数里的一个字面量），不再拆散就算达标。

---

## 1. 现状盘点

### 1.1 已经做好的部分（不需要动）

| 能力 | 位置 | 说明 |
|---|---|---|
| 自动发现 | `web/src/composables/dockPluginRuntime.ts:99` | `import.meta.glob('@/plugins/*/plugin.ts')`，新建目录即被发现 |
| 面板懒加载 | 同上 | 插件写 `load: () => import(...)`，不开不加载 |
| 各处派生 | 左侧栏 / 功能编排页 / 设置首页 / 设置分类 / 通知跳转 / 内部事件 | 全部读注册表，**没有**按 tab id 手写的 `if/case` |
| 服务端注册 | `cmd/server/main.go:2095` `registerPlugins` | 一个功能的路由、开关、后台启停、可改配置，全在一个字面量里 |

### 1.2 还差的地方（本方案要解决的）

| # | 要改哪 | 文件 | 卡在哪 |
|---|---|---|---|
| ① | **顺序** | `dockTabs.ts` `DOCK_TAB_ORDER` | 该文件规定「不许 import 任何东西」，推不出来，只能手写 |
| ② | **id 名单与类型** | 同上，`DOCK_TAB_IDS` / `DockTabId` | 同上 |
| ③ | **服务端开关白名单** | `dockTabs.ts` `DOCK_FEATURES` | 手写了一份，服务端 `registerPlugins` 里是另一份 |
| ④ | **设置页字段** | `components/settings/settingsFieldMap.ts`（1346 行） | 插件只说「我是清单页」，字段清单全在全局文件 |
| ⑤ | **文案** | `i18n/locales/en.ts` / `zh.ts`（各约 6100 行） | 标题、描述、不可用原因、设置项文案都在全局文件 |
| ⑥ | 宿主能力（可选） | `App.vue` `dockPanelExtras` + `DockPanelHostExtras` | 仅当插件需要特殊宿主能力时 |

**①② 是同一处**，所以严格计 5 处。⑥ 不是加插件的必经之路，本方案**不动**（理由见 §5）。

### 1.3 一个隐蔽的重复（本次**未**消除，见 §7.2）

`plugins/terminal/plugin.ts` 里写 `id: 'terminal'`，目录名也叫 `terminal`。

计划里是从路径推出 id、插件文件不再写 id。实际没这么做：`chat` 也在同一个 glob 里，而它**没有** `order`（不是左侧栏入口），装配时要靠 `id !== 'chat'` 把它分开——这要求 `id` 在类型上可见，从路径推的话拿不到。
重复因此保留，仍由 `dockPluginManifest.test.ts` 的第一条守卫（目录名 === 声明的 id）钉住。

---

## 2. 三步改造

### 第一步：顺序与 id 名单收进插件

#### 为什么现在推不出来

`dockTabs.ts` 开头那段注释说得很明白：它**故意不 import 任何东西**。原因是 `useWideScreenLayout` 依赖它，而这个 composable 被很多组件用到，那些组件的测试会把 `lucide-vue-next` 整个 mock 成只剩几个图标——这个文件一旦引入插件模块（进而引入图标），那些测试会在**加载时**崩：

```
No "FolderOpen" export is defined on the "lucide-vue-next" mock
```

顺序手写还有一层产品理由：默认顺序是产品决策，不能让文件夹的字母序决定新插件落哪。

#### 改法

**1）插件自己写顺序**

```ts
// web/src/plugins/terminal/plugin.ts
export default {
  id: 'terminal',
  order: 60,          // ← 新增：默认顺序（按改前的既有顺序换算，留 10 的空档）
  titleKey: 'terminal.title',
  ...
}
```

顺序号留空档（10/20/30），中间插一项不用重排全部。

**2）`dockTabs.ts` 不再列任何 id**

保留的是「机制」，不是「名单」：

```ts
// 不再有 DOCK_TAB_ORDER 字面量
export type DockTabId = string   // 类型退化为 string

let dockTabIds: readonly DockTabId[] = []      // 由注册表填充
export function dockTabIdsInDefaultOrder(): readonly DockTabId[] { return dockTabIds }
export function installDockTabIds(ids: readonly DockTabId[]): void { … }
```

这里的关键是：`dockTabs.ts` 仍然**不 import 任何东西**（那条守卫测试原样保留，包括禁止 `import.meta`），只是它的数据改成由 `dockPluginRuntime.ts` **在模块加载时写入**（`installDockTabIds`）。方向仍然是「runtime 依赖 dockTabs」，不会反过来。

⚠️ 计划里曾想加一个「首次读取时自己去扫目录」的兜底，实测放弃：那需要 `import.meta.glob` 或 `fs`，两者都违反该文件的零导入约束（守卫测试直接红了）。改为**任何只测单模块的测试显式调用 `installDockTabIds`**，并在全局 `test-setup.ts` 里装一份真实清单，绝大多数测试因此不用改。

**3）`dockPluginRuntime.ts` 组装时排序**

```ts
// 按 order 升序；order 相同按 id 兜底，保证稳定
const dockPlugins = (Object.keys(runtime) as TabId[])
  .map((id) => runtime[id])
  .filter((p): p is Extract<DockPlugin, { id: DockTabId }> => !!p && p.id !== 'chat')
  .sort((a, b) => (a.order - b.order) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
installDockTabIds(dockPlugins.map((p) => p.id))
```

**4）id 的来源（与计划不同：仍然写在插件文件里）**

原计划让加载器从目录名推 id、插件文件不再写 `id`。实现时保留了 `id` 字段，理由：

- `DockPlugin` 是个联合类型，`chat` 那一支没有 manifest；判别「这是不是一个 dock 插件」需要一个**字段**，目录名在类型层面看不到。
- 目录名与 `id` 的一致性由 `dockPluginManifest.test.ts` 第一条钉住（"has one plugin.ts per directory, and the directory name is the plugin id"），这条守卫原本就有，成本为零。

所以这一项**没有做**：id 仍写一遍，但目录名写错会被测试拦住，不是新增风险。

**5）校验从测试提到运行期**

现在这些规则只活在测试里，加错插件要跑测试才知道。改成加载时就报：

- id 重复 → 保留第一个，其余丢弃并 `appLog.w`（**已实现**，原有行为）
- `order` 缺失 → 该模块不会被当作 dock 条目（**已实现**，由类型守卫 `typeof order === 'number'` 判定），并由测试 `declares a distinct, positive \`order\` for every dock plugin` 钉住
- `order` 重复 → 按 id 兜底排序（**已实现**），测试 `breaks an order tie by id` 钉住
- 目录名与 `id` 不符 → 由既有测试拦截（**已实现**，非新增）

> ⚠️ **取舍（已确认）**：`DockTabId` 从联合类型退化为 `string`，拼错 id 从**编译报错**变成**运行期警告**。这是唯一实质损失。
>
> 实际暴露的风险比预想小：写 id 的地方仍只有插件文件一处（目录名由测试比对），所以「写错」的机会没有增加，只是「发现时机」延后。

#### 受影响的调用方（都是「读」，不用改逻辑）

id / 开关清单有 **5 个**读取点：`dockPluginRuntime.ts`、`useDockLifecycle.ts`、`useDockPlugins.ts`、`useWideScreenLayout.ts`、`useFeatureFlags.ts`（最后这个读 `dockFeatures()`，漏掉它会让所有开关都不被当成 master feature——静默失效）。

⚠️ **需要注意（实际踩到了）**：`useWideScreenLayout.ts` 和 `useDockPlugins.ts` 拿到 id 列表的时机。计划阶段判断"只要这几个模块不早于 runtime 就行"，实现时发现还有第二种情况：**测试里把插件运行时整个 mock 掉**，此时 id 从没被填充，循环拿到空数组，表现为「切标签页静默无效」。

解法两条：
1. 所有读取点改成**函数调用**（`dockTabIdsInDefaultOrder()` / `dockFeatures()`），不捕获模块级快照 —— `useWideScreenLayout` 原来用 `export const WIDE_SCREEN_DOCK_TABS = DOCK_TAB_IDS` 在模块加载时快照，首帧拿到空值。
2. 全局 `test-setup.ts` 装一份真实清单，供所有测试共享。

**review 后又补了两处**（第一版仍留着两个模块级快照，只是被导入顺序救了）：

- `useDockPlugins.ts` 的 `DOCK_PRESET_PLANS` 是 `{ developer: expandPreset(...), ... }` 这样的模块级常量，而 `expandPreset` 现在要读 id 和开关。改成 `dockPresetPlan(name)` **按需展开、不缓存**；并加了一条回归测试：先把 id 清空再装回来，断言两次调用结果跟着变（快照会让第二次仍是空）。
- `useWideScreenLayout.ts` 的 `wideScreenDockTabs()` 生产代码零调用点（宽屏校验本来就走 `isDockTabId`），已删除；测试里直接比 `dockTabIdsInDefaultOrder()`。

**这才是第一步的真正风险点**，不是类型退化。

---

### 第二步：功能开关白名单收进插件声明

> **与计划不同（重要）**：计划原本是从 `/api/features` 的接口返回推导。实现时改成了**从插件的 `feature` 字段汇总**。理由见下。

#### 为什么现在是两份

- 服务端：`registerPlugins` 里每个插件声明 `Enabled`，`GET /api/features` 下发 `{id, enabled, available, reason, defaultOn}`
- 前端：`dockTabs.ts` 的 `DOCK_FEATURES` 又手写了一遍 `['forge.enabled', 'mail.enabled', ...]`

#### 改法：从插件声明汇总，而不是从接口

每个插件本来就声明了自己挂哪个开关：

```ts
// plugins/mail/plugin.ts
feature: 'mail.enabled',
```

`dockPluginRuntime.ts` 装配时把这些收成一份清单，推给 `dockTabs.ts`（和 id 列表同一条路）：

```ts
const features = dockPlugins.map((p) => p.feature).filter(Boolean)  // 去重
installDockFeatures(features)
```

`useDockPlugins` 的预设从此读 `dockFeatures()`。

#### 为什么不用 `/api/features`

计划里那样做有两个问题：

1. **预设要在接口回来之前就能算出来。** 预设是「写入」动作（一键把若干开关设为 on/off），需要先知道**要写哪些 key**。接口没回来时不知道，只能给一个加载态；而插件声明是编译期就有的，任何时刻都能算。
2. **两者的语义不同。** `/api/features` 回答的是「这个开关现在开没开」，插件声明回答的是「这个功能归哪个开关管」。预设需要的是后者。

接口那份快照（`serverFeatures`）仍然负责「现在开没开」，由 `isFeatureEnabled()` 读，职责没变。

#### 仍然手写的那一处

`DockPluginSetting.vue` 的 `DESTRUCTIVE_FEATURES`（关掉会造成破坏性后果的开关：Forge 清绑定、分享吊销链接）**保留手写**——这是产品判断，不是从声明里推得出来的。这一处不能收。

#### 类型

`DockFeature` 从字面量联合类型退化为 `string`（因为清单来自运行期）。代价与 id 相同：写错开关名不再编译报错，靠测试 `exposes exactly the switches the manifests declare` 与既有守卫钉住。

---

### 第三步：设置页字段与文案收进插件目录

#### 3.1 设置页字段

**改前**：`settingsFieldMap.ts` 的 `categoryItems` 是一个 1346 行的大对象，12 个插件页的字段清单全在里面；插件文件只说「我是 `kind: 'flat'` / `'group'` / `'custom'`」。

**改后**：每个插件目录新增 `settings.ts`，导出 `CategoryEntry[]`：

```
web/src/plugins/stats/
  plugin.ts
  settings.ts     ← 字段清单
```

全局表里对应的键留成空数组并加一行注释指路：

```ts
// Fields live in `plugins/stats/settings.ts` — see PLUGIN_SETTINGS below.
stats: [],
```

`settingsFieldMap.ts` 自己在模块加载末尾把插件文件合并进 `categoryItems`（`import.meta.glob('@/plugins/*/settings.ts')`），**合并后写回同一个对象**，所以 `SettingsCategory.vue`、`getServerFieldToLabelKey()`、重启对话框、搜索——所有既有读取方一行没改。

**防重复声明**：若同一个分类在全局表和插件文件里都写了字段，合并时直接抛错，不会静默取其一。

**为什么合并而不改读取方**：`categoryItems` 有 4 处读取点（渲染、重启对话框的字段名翻译、搜索、面板查找）。让它们各自「先查插件再查全局」是 4 个可能写错的地方；在加载时合并成一份，读取方根本不知道有这回事。

#### 3.1.1 一个残留：分类名映射表

插件的**目录名不总等于它的设置分类名**：

| 目录 | 设置分类 |
|---|---|
| `browse` | `files` |
| `view` | `fileView` |
| `forge` | `forgeIntegration` |
| `proxy` | `portForward` |
| `live` | `meeting` |

`settingsFieldMap.ts` 里因此留了一张手写的 `PLUGIN_SETTINGS_CATEGORY` 映射表。

**这是本次收口后仍然存在的一处手工维护点**，如实记录，不粉饰。理论上可以从插件的 `settingsCategory` 字段读出来，但那需要 **import 插件模块**（连同它引入的图标、store），而设置字段表会被很多组件与测试直接引入——正是 `dockTabs.ts` 那条零导入约束要避开的坑。宁可在这一处手写 13 行，也不把整张图标图拖进设置表。

后果与代价：**新插件的设置分类名若与目录名不同，必须在这张表里补一行**，否则合并会落到一个不存在的分类（字段静默不显示）。量小、且只影响「分类名≠目录名」这一种情况。

#### 3.2 文案

**改后**：每个插件目录新增 `i18n.ts`，只放**该插件自己的描述与不可用原因**：

```ts
// web/src/plugins/mail/i18n.ts
export default {
  zh: { desc: '收发邮件，支持多个邮箱账号。', unavailable: '服务端没有启用邮件功能。' },
  en: { desc: 'Read and send mail across several accounts.', unavailable: 'Mail is not enabled on this server.' },
}
```

`i18n/pluginMessages.ts` 在 `createI18n` **之前**把这些合并进基础语言包，落到 UI 本来就在用的键上：

- 描述 → `settings.dockPlugins.desc.<id>`
- 不可用原因 → `settings.dockPlugins.unavailable.<id>`

所以 `descriptionKey` / `unavailableReasonKey` 的写法和取值都没变，改动对下游不可见。

**为什么是启动时合并、而不是在语言包里 glob**：`zh.ts` / `en.ts` 是纯字面量对象，几十个测试直接 import 它们；若在那里 glob 插件模块，一段文案的修改就会把 store、图标、composable 拖进语言包，并把「哪个语言生效」交给模块加载顺序。启动时合并一次，语言表保持纯数据，同时仍是「每段文案一个来源」。

**怎么划边界**：

| 放哪 | 内容 |
|---|---|
| 插件目录 `i18n.ts` | 只属于这个插件的：管理器里的描述、这个宿主不支持它的原因 |
| 全局 `en.ts` / `zh.ts` | 框架级共用文案（保存/取消/已启用/「当前环境不支持」兜底），以及**各功能自己的整块界面文案** |

判据：**这段文案只属于一个插件就进插件目录；被两个以上地方用就留全局。**

#### 3.2.1 这次**没**搬的文案

各功能自己的整块文案（`mail`、`meeting`、`kb` 各几百行）**仍在** `i18n/locales/*.ts`。原因：它们与各自的组件强绑定，边界不清晰（大量 `common.*` 被跨功能复用），整体搬迁的改动量与回归风险远超「插件元数据」这一层。

本次只搬了加插件时**必须**补的两段（描述 + 不可用原因），它们才是「新插件绕不过去」的部分。

---

## 3. 改完后的最终形态

加一个插件：

1. 建 `web/src/plugins/<id>/plugin.ts`（含 `order`、`feature`）
2. 建 `web/src/plugins/<id>/settings.ts`（有设置页时）
3. 建 `web/src/plugins/<id>/i18n.ts`（描述 + 不可用原因）
4. 服务端 `registerPlugins` 加一个字面量块

前端 **1 处**（一个新目录），服务端 1 处。**基本达标。**

唯一残留：如果新插件的**设置分类名与目录名不同**（如 `browse` → `files`），还要在 `settingsFieldMap.ts` 的 `PLUGIN_SETTINGS_CATEGORY` 加一行（13 行的小表，理由见 §3.1.1）。分类名直接等于目录名的话，连这一行都不用。

前端测试还需要同步 `testUtils/dockRegistryIds.ts` 里的镜像清单——但那是**测试**清单，漏了会有测试红，不会静默。

---

## 4. 验证方式

每一步都要能单独跑通、单独回滚。

### 4.1 自动化

- `npm test`（Vitest）全绿
- `go test ./...` 全绿
- 重点看这几支测试是否需要改写：
  - `web/src/composables/__tests__/dockPluginManifest.test.ts`（清单守卫，改动最大）
  - `web/src/composables/__tests__/useDockPlugins.test.ts`（预设逻辑）
  - `web/src/components/settings/__tests__/DockPluginSetting.test.ts`
  - `web/src/__tests__/featureTitleKeyAlignment.test.ts`（标题 key 一致性）
  - `web/src/components/settings/__tests__/SettingsIndex.test.ts`

### 4.2 手动核对清单（改完必须逐条走一遍）

| # | 检查项 | 怎么看 |
|---|---|---|
| 1 | 左侧栏顺序与改前**完全一致** | 截图比对，15 个入口位置不能变 |
| 2 | 预设（开发者/标准/极简）行为不变 | 三个预设各点一次，看隐藏与开关结果 |
| 3 | 拖拽排序 + 刷新后保持 | 拖动两个入口，刷新页面 |
| 4 | 老的 `dockOrder` 存储值仍能正确套用 | 用改前的浏览器存储直接打开（`mergeDockOrder` 的槽位规则不能坏） |
| 5 | 功能编排页所有开关仍然可用 | 逐个开关切一次，看服务端是否随之启停 |
| 6 | 关闭一个功能后，入口消失 + 深链也进不去 | 关 Forge，然后点一条 Forge 通知 |
| 7 | 各功能设置页字段完整、能读写 | 8 个设置页逐页翻，改一项保存再刷新 |
| 8 | 中英文切换后，插件文案正确 | 切到英文，翻遍左侧栏 + 功能编排 + 各设置页 |
| 9 | 缺 `order` / id 重复时，控制台有明确报错 | 临时造一个坏插件，看报错是否可读 |
| 10 | `/api/features` 慢或失败时，界面不闪、不误判 | 断网打开，看是否所有功能仍在（开关的**清单**来自插件声明，已不依赖接口；接口只决定「现在开没开」） |
| 11 | 设置页字段全部还在 | 12 个插件的设置页逐页翻，与改前对比字段数量 |
| 12 | 插件文案在中英文下都正确 | 「功能编排」列表里逐行看描述；切到英文再翻一遍 |

### 4.3 回归重点

第 4 条（老存储值兼容）和第 10 条（接口未就绪）是最容易出问题、又最不容易在测试里发现的两条。测试要专门补。

---

## 5. 明确不做的事

### 5.1 不把 `dockPanelExtras` 改成开放式

`App.vue` 里 `terminal` / `task` / `meeting` 三个键写在 `DockPanelHostExtras` 接口里，是封闭的。

改成开放结构（`Record<string, unknown>`）确实能少改一个文件，但**等于放弃类型检查**：插件取一个不存在的键不会报错，只会在运行时炸。而且它不是加插件的必经之路——只有插件需要特殊宿主能力时才碰。

→ **保持现状。**

### 5.2 不动服务端 `registerPlugins` 的结构

它已经是一个函数里的一个字面量，一处。前端收口后不再需要动它。

### 5.3 不引入构建期代码生成

「用代码生成产出 `DockTabId` 联合类型」能保住编译期检查，但要在构建流程里插一步生成、还要处理生成物与源码的同步。成本高于收益，先不做。

---

## 6. 风险与回滚

| 步骤 | 预估风险 | 实际结果 |
|---|---|---|
| 第一步 | 中。读取时机若早于注册表填充会拿到空数组，表现为界面无入口 | **风险兑现了**：约 10 个测试文件因「只 mock 了运行时、id 从未被填充」而失败。通过「读取点全改成函数调用 + 全局 setup 安装」解决，见 §2 第一步末尾 |
| 第二步 | 低。启动空档期已有既成约定 | 无意外。改成从插件声明汇总后，空档期问题根本不存在了（不依赖接口） |
| 第三步 | 较高。字段搬家与文案合并 | 字段搬家机制（合并写回 `categoryItems` + 重复声明抛错）一次跑通；文案合并也一次通过。风险主要落在测试对旧结构的引用上，已逐条改掉 |

**执行顺序**：三步连续完成（用户指定），中途未暂停手动核对——所以 §4.2 那份清单留在最后统一走。

---

## 7. 落地结果

三步已全部完成。`DockTabId` 接受退化为 `string`；文案按「只属于一个插件就进插件目录」切分。

### 7.1 各步实际做了什么

| 步 | 结果 |
|---|---|
| 一 | 14 个插件各加 `order`（10…140，留空档）；`dockTabs.ts` 的 `DOCK_TAB_ORDER` 字面量删除；`DockTabId` 改为 `string`；`dockPluginRuntime.ts` 按 `order` 排序后调 `installDockTabIds` 推送 |
| 二 | `dockTabs.ts` 的 `DOCK_FEATURES` 字面量删除；改由 `dockPluginRuntime.ts` 从各插件的 `feature` 字段汇总，经 `installDockFeatures` 推送；`DESTRUCTIVE_FEATURES` 按计划保留手写 |
| 三 | 12 个分类的字段清单搬进 `plugins/<id>/settings.ts`；14 个插件的 `desc` / `unavailable` 文案搬进 `plugins/<id>/i18n.ts`，由 `i18n/pluginMessages.ts` 在建实例前合并 |

### 7.2 与计划的偏差（三处）

**① id 仍由插件文件声明，没有从目录名推。**
原因见 §2 第 4 条：`DockPlugin` 是联合类型，判别 dock 插件需要类型可见的字段，目录名在类型层面看不到。目录名一致性由既有测试钉住，不是新增风险。

**② `dockTabs.ts` 没有做运行期「重新拉取兜底」。**
计划里担心「模块加载早于注册表填充会拿到空数组」。实现时**没有**加兜底拉取，理由是那会让 `dockTabs.ts` 依赖构建期的 `import.meta.glob`，直接违反它「不 import 任何东西」的守卫（`dockPluginManifest.test.ts` 的 "has no runtime import" 会失败——实测确实失败了，所以走了注入）。

替代方案：`dockTabs.ts` 只持有被推送进来的数据；**全局测试 setup（`web/src/test-setup.ts`）负责安装**。真实运行时由 `dockPluginRuntime.ts` 在模块加载时推送，早于任何组件渲染。

**③ 新增了一个测试辅助 `web/src/testUtils/dockRegistryIds.ts`。**
因为 `test-setup.ts` 跑在各 spec 的 `vi.mock` 工厂生效之前，直接 glob 插件模块会踩到「Cannot access 'X' before initialization」。所以那两份清单在测试侧是**手写镜像**，由 `dockPluginManifest.test.ts` 的 "test registry helper" 双向钉住（漏更新 = 测试失败）。

### 7.3 最终形态（已验证）

加一个插件：

1. `web/src/plugins/<id>/plugin.ts`（含 `order`）
2. `web/src/plugins/<id>/settings.ts`（有设置页时）
3. `web/src/plugins/<id>/i18n.ts`（描述 / 不可用原因）
4. 服务端 `registerPlugins` 加一个字面量块

前端 1 处（一个新目录），服务端 1 处。**达标。**

### 7.4 验证状态

- `npm run typecheck`（即 `vue-tsc --noEmit -p web/tsconfig.json`）：**EXIT=0**（review 修复前后各跑一次）
- `npx vitest run`：1162 / 1163 个文件通过。唯一失败是 `staticBlockCache.projectRoot.test.ts` 的一条路径相关用例，**单独运行时通过**，且与本改动无交集（工作区另有未提交的 `handler.go` 等 Go 改动）；判定为并发下的既有偶发。
- `npx vitest run web/src/composables`（review 修复后）：**217 / 218 个文件通过，5967 / 5968 条通过**。唯一失败是 `useForgeUnreadItems.test.ts` 的一条，**单独跑 14/14 全过**，同样判为并发偶发（该文件与本改动无 import 关系）。
- 与插件注册表直接相关的 9 个测试文件：全绿（含新增的「预设不冻结安装前快照」回归）。
- `go test ./...`：未运行（本次未改 Go 代码）

### 7.4.1 用运行时断言替代的手动核对

§4.2 里能自动化的几条，用一次性断言实测过（断言脚本跑完即删，结论记录在此）：

| 核对项 | 结果 |
|---|---|
| 默认顺序与改前完全一致 | ✅ 逐项相等：`browse,view,history,forge,tasks,terminal,stats,mail,kb,live,share,proxy,notifications,settings` |
| 老的「只存尾部」`dockOrder` 仍正确套用 | ✅ 常驻项（browse/view/history/settings）位置不动，尾部按存储顺序填入（槽位规则未变） |
| 完整 `dockOrder` 原样复现 | ✅ |
| 未知 id / 重复 id 被丢弃 | ✅ 长度回落为真实 id 数，无重复 |
| 12 个插件分类的字段仍然存在 | ✅ 全部非空（files 9、fileView 7、gitHistory 4、stats 3、tasks 7、mail 7、share 7、forgeIntegration 10、terminal 2、meeting 20、kb 10、portForward 1） |
| 搬字段没有丢字段 | ✅ 全局表 90 + 插件文件 87 = **177**，与改前总数完全相等 |
| 14 个插件的描述/不可用原因中英文都能取到 | ✅ zh/en 各 14 条描述、5 条不可用原因 |

### 7.4.3 独立 review 后的修复

实现完成后做了一轮独立 review，发现的问题与处置：

| 问题 | 处置 |
|---|---|
| `DOCK_PRESET_PLANS` 是模块级快照，`expandPreset` 在 import 时就读了 id/开关（今天只被导入顺序救了） | 改成 `dockPresetPlan(name)` 按需展开；加回归测试「清空 id → 调用 → 装回 → 再调用」 |
| `dockTabs.ts` 注释称 `installDockTabIds` 会打印 id 清单，实际没有 | 在 `dockPluginRuntime` 安装后补一条 `appLog.i`（`dockTabs.ts` 零导入，不能自己打） |
| `DOCK_FEATURES` 从 runtime 导出但生产零引用（读取方都走 `dockFeatures()`） | 删除导出 |
| `useWideScreenLayout` 的 `wideScreenDockTabs()` 生产零调用点（校验本来走 `isDockTabId`） | 删除函数；测试直接比 `dockTabIdsInDefaultOrder()` |
| 测试里出现 `expect(accessor()).toEqual(accessor())` 的同义反复（删函数时替换产生） | 删掉该断言，保留「已安装 id == 装配出的 descriptors」这条真断言 |
| 14 个插件 `i18n.ts` 的头部注释里都有空行（缺 ` *`） | 统一修正 |
| 文档写「4 个读取点」，实际 5 个（漏 `useFeatureFlags`） | 已补 |
| 文档写「8 个插件的分类名与目录名不同」，实际 6 个 | 已改为 6，并列出对应关系 |

未采纳的一条：reviewer 建议给「有人重新引入第二份手写 id 列表」加一条源码 grep 守卫。现在 id 只有插件一处来源，`dockPluginManifest.test.ts` 已经钉住「每个目录一个 plugin.ts 且目录名 == 声明 id」，再加一条按字面量匹配的源码断言收益低、误报高，故不做。

### 7.4.2 仍需真机确认的

以下几条依赖真实浏览器与服务端交互，纯断言替代不了：

1. 左侧栏渲染出来的**位置**与改前一致（断言验的是数据顺序，不是渲染）
2. 拖拽排序后刷新仍保持
3. 预设（开发者/标准/极简）点击后的实际效果与开关写入
4. 关闭某功能后深链确实进不去
5. 中英文切换后的界面呈现

> 注：20000 端口上已有另一个 `clawbench` 实例在跑（`AGENTS.md` 明确禁止触碰），所以上述核对未在原地执行；需要在独立数据目录上起一个实例（`--data-dir /tmp/xxx --port 8081`）后走一遍。

### 7.5 新增/改写的守卫

| 测试 | 防的是什么 |
|---|---|
| `declares a distinct, positive \`order\` for every dock plugin` | 两个插件同号、或漏写 `order` |
| `assembles the manifest in the plugin-declared order` | 装配结果必须升序，且图标齐全 |
| `sorts descriptors by the declared order, not by glob order` | 排序真的来自 `order`，不是文件系统 |
| `breaks an order tie by id` | 同号时结果稳定 |
| `exposes exactly the switches the manifests declare` | 开关汇总与服务端声明一致 |
| `keeps every plugin's copy in its own directory` | 文案真的从插件目录到达界面 |
| `test registry helper` | 测试镜像清单与真实插件双向一致 |

---

## 8. 还没做的

1. **§4.2 的手动核对清单（现为 12 条）**——必须启动服务端走一遍，尤其是「老 `dockOrder` 存储值兼容」和「设置页字段是否一个不少」两条。
2. **`PLUGIN_SETTINGS_CATEGORY` 映射表**（§3.1.1）。13 个分类里有 **6 个**与目录名不同（browse→files、view→fileView、forge→forgeIntegration、proxy→portForward、live→meeting、history→gitHistory），因此留了一张手写映射。这是本次收口后**唯一仍需要手工维护的点**。消除它需要 import 插件模块进设置字段表，代价是把图标图拖进来，判断不划算。
3. **各功能整块文案仍留在 `i18n/locales/*.ts`**。本次只搬了插件元数据（描述 + 不可用原因）。`mail`、`meeting` 这类功能自己的几百行文案没动——它们与组件强绑定，边界不清晰，属于后续可选项。
4. **服务端 `registerPlugins` 未改**（不在本次范围）。
5. **`DockTabId` / `DockFeature` 已退化为 `string`**，拼错不再编译报错。若日后觉得这个损失不能接受，补救路径是构建期代码生成（§5.3 里明确没做）。
