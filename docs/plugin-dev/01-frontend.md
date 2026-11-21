# 界面插件（Dock）

界面插件就是 `web/src/plugins/<id>/` 这个目录。**建目录即被发现**，不需要改任何清单文件。

---

## 1. 目录里放什么

```
web/src/plugins/<id>/
├── plugin.ts     必需。身份 + 顺序 + 图标 + 面板 + 运行时资源
├── i18n.ts       必需（chat 除外）。管理器里那句描述 + "这台机器跑不了"的原因
├── settings.ts   有这个功能自己的设置页时才要。字段清单
└── （面板组件不放这里）  放 web/src/components/<area>/XxxPanel.vue
```

三个聚合点，都是 `import.meta.glob`，都在构建期：

| 文件 | glob | 合并成 |
|---|---|---|
| `composables/dockPluginRuntime.ts` | `@/plugins/*/plugin.ts` | 左侧栏清单（按 `order` 排序）、`/api/features` 的消费方、设置页索引、深链注册 |
| `i18n/pluginMessages.ts` | `@/plugins/*/i18n.ts` | `settings.dockPlugins.desc.<id>` / `.unavailable.<id>` |
| `components/settings/settingsFieldMap.ts` | `@/plugins/*/settings.ts` | 该插件的设置分类字段 |

面板组件本身**不进主包**：`plugin.ts` 里写 `load: () => import(...)`，不开这个页签就不加载。

---

## 2. `plugin.ts` 字段

```ts
export default {
  id: 'foo',
  order: 80,
  titleKey: 'nav.foo',
  descriptionKey: 'settings.dockPlugins.desc.foo',
  group: 'project',
  closable: true,
  icon: Rocket,
  panel: { load: () => import('@/components/foo/FooPanel.vue') },
} satisfies DockPlugin
```

### 清单部分（左栏与设置管理器用）

| 字段 | 必填 | 说明 |
|---|---|---|
| `id` | ✅ | 必须等于目录名（守卫会查） |
| `order` | ✅ | 出厂默认顺序，小的在前。**按 10 递增**，方便插到两个之间；重复会让守卫红（不靠 glob 顺序决定） |
| `titleKey` | ✅ | 页签标题的 i18n key |
| `descriptionKey` | ✅ | 管理器里那句描述的 key，固定写 `settings.dockPlugins.desc.<id>`（文案在 `i18n.ts`） |
| `group` | ✅ | 管理器分组：`workspace` / `project` / `personal` / `system`，**不影响左栏顺序** |
| `closable` | ✅ | 用户能不能隐藏。常驻三项（`browse` / `view` / `settings`）填 `false` |
| `icon` | ✅ | 静态图标。图标随状态变才用运行时的 `dynamicIcon` |
| `feature` | 可选 | 依赖的服务端总开关，如 `'mail.enabled'`。填了 ⇒ 管理器里的开关写服务端配置，关掉页签消失 |
| `capability` | 可选 | 依赖的宿主能力：`'terminal'` / `'portForward'` |
| `settingsCategory` | 可选 | 自己的设置页分类 id，默认等于 `id` |
| `settingsPage` | 可选 | 往设置首页贡献一个入口，见 §4 |

### 运行时部分（都是可选的）

| 字段 | 说明 |
|---|---|
| `panel` | 左栏面板。`load()` 动态导入，或 `component` 同步组件（仅核心页签） |
| `badge()` | 角标。`{ count: 0, live: true }` 表示只有小圆点（进行中的会议） |
| `available()` | 这台机器现在能不能用。`false` ⇒ 页签隐藏，管理器里显示 `unavailableReasonKey` |
| `unavailableReasonKey` | 为什么不可用，文案在 `i18n.ts` |
| `leaveWhenUnavailable()` | 变不可用时要不要强制离开这个页签。默认 `true`；终端填 `false`，好让面板自己显示"平台不支持" |
| `dynamicIcon()` / `dynamicTitleKey()` | 图标/名字随状态变（forge 绑了 GitLab 就换成 GitLab 的名字） |
| `onEnter()` / `onLeave()` | 进入/离开页签 |
| `startBackground()` | 页签关着也要跑的后台资源（邮件未读数轮询）。返回清理函数 |
| `appearance` | 按钮外观的**条件**：`unread` / `running` / `justCompleted` / `liveDot`，每个是 `() => boolean`。长什么样在 CSS 里 |
| `onClick(host, extras)` | 点左栏按钮时、切页签**之前**做的事 |
| `deeplinks` | 这个插件要接的 window 事件（通知跳转、引用跳转），见 §6 |

`chat` 是唯一例外：它是右侧对话区，不是左栏页签，所以只写 `id` / `icon` / `titleKey` / `panel`。

---

## 3. `i18n.ts`

```ts
export default {
  zh: { desc: '一句话说明这个功能。', unavailable: '这台机器跑不了的原因。' },
  en: { desc: 'One line about what this does.', unavailable: 'Why this host cannot run it.' },
}
```

- 只放**这个插件自己的**两段文案。`保存` / `取消` 这种共用词留在 `i18n/locales/*.ts`。
- 标题（`nav.foo`）仍在全局语言表里——它是导航词，不是功能描述。
- 缺 `en` 会回退到 `zh`，但守卫要求两边都有值。

---

## 4. `settings.ts` 与设置页

`plugin.ts` 里声明了 `settingsPage` 才需要这个文件：

```ts
settingsPage: { group: 'dockModules', icon: Rocket, kind: 'flat' }
```

| `kind` | 长什么样 | 字段从哪来 |
|---|---|---|
| `flat` | 一行行设置项，按 `sectionHeader` 自动分成卡片 | `plugins/<id>/settings.ts` |
| `group` | 分组面板（`SettingsGroupPanel`） | `plugins/<id>/settings.ts` |
| `custom` | 完全自己的组件 | `settingsPage.component: () => import(...)` |

`settings.ts` 导出一个 `CategoryEntry[]`，每个条目是两种之一：

| 条目 | 长什么样 | 保存方式 |
|---|---|---|
| `{ type: 'item', spec }` | 一行（开关 / 输入框 / 下拉） | 改完立即保存 |
| `{ type: 'panel', config }` | 一组字段的卡片 | 批量保存（`SettingsGroupPanel`） |

```ts
export default [
  {
    type: 'item',
    spec: {
      labelKey: 'settings.items.fooEnabled',
      descriptionKey: 'settings.items.fooEnabledDesc',
      key: 'foo.enabled',        // 服务端字段用 source: 'server'
      type: 'switch',
      source: 'server',
    },
  },
  // 或者：{ type: 'panel', config: { panelId: 'fooAdvanced', … } }
] satisfies CategoryEntry[]
```

分类名默认等于目录名。历史上不一致的五个（`browse`→`files`、`live`→`meeting`、`proxy`→`portForward`、`forge`→`forgeIntegration`、`view`→`fileView`）登记在 `settingsFieldMap.ts` 的 `PLUGIN_SETTINGS_CATEGORY` 里。**新插件不要再制造例外。**

`custom` 页面的组件在模块加载时被 `buildAsyncComponentOptions` 包一次——不要在渲染函数里调 `defineAsyncComponent`，那会每次渲染都产生新组件身份、整页重挂。

---

## 5. 面板组件的契约

- 必有一个 prop：`active`（当前是否可见）。
- 其余 props / 事件由 `panel.props(host, extras)` / `panel.on(host, extras)` 给。
- 首屏"空、只等数据"用 `<PanelSkeleton>`，不要居中转圈——转圈会让整块内容在数据到达时跳一下。
- 宿主能力分两层：通用的在 `DockPanelHost`（`projectPath` / `openFile` / `openSettingsCategory` / `openTab` / `isWideScreen`），插件专属的在 `DockPanelHostExtras`（按插件一个 key）。**新能力加到自己那个 key 下，不要往通用接口里塞**——那会让所有面板都看见别人的东西。

---

## 6. 深链（从通知/引用跳到某个页签）

```ts
deeplinks: [
  {
    event: 'clawbench-open-mail',
    handler: (detail, ctx) => {
      if (ctx.tabBlocked('mail')) return          // 关着/不可用/被隐藏：直接放弃
      ctx.recordNotificationOrigin(detail)        // 通知中心来的跳转要能被"返回"回去
      parkPendingMailOpen(detail.messageId)       // 面板还没挂载时先把参数存起来
      ctx.switchTab('mail')
    },
  },
]
```

宿主负责注册/注销和 `ctx`；插件只写 handler。`ctx` 提供五个原语：`switchTab`、`hotSwitchProject`、`tabBlocked`、`recordNotificationOrigin`、`currentProject`。

**不要**把"先判断再跳转再清状态"这一串做成通用模板——顺序是有意义的（比如跨项目跳转要先拒绝、再发布目标、再切换、失败才清）。给原语、让插件自己写顺序。

---

## 7. 三个轴，别混

一个页签显示不显示，取决于三个独立的问题：

| 轴 | 谁回答 | 关掉之后 |
|---|---|---|
| 服务端开关 | `feature` + `/api/features` | 页签消失；服务端接口返回 409（或插件自己的状态码） |
| 宿主能力 | `capability` + `available()` | 页签隐藏；管理器里灰显并给原因 |
| 用户偏好 | `closable` + 管理器的显隐 | 只是不显示，服务端照常工作 |

**`feature` 和 `available()` 不要混用**：前者是"用户关掉了"，后者是"这台机器跑不了"。终端两个都有：`terminal.enabled` 是开关，PTY 起不来是能力。

`startBackground` 的起停条件是这三轴的交集（见 `useDockLifecycle`）：只要有一轴是"不可用"，后台资源就停——所以邮件未读数在功能关掉、页签被隐藏、或页面不可用时都会停止轮询。

---

## 8. 守卫（`dockPluginManifest.test.ts`）

加插件时忘掉某一样，页面不会报错，只会"页签不出现"或"显示成原始 key"。所以每一条都钉住了：

| 不变量 | 违反的后果 |
|---|---|
| 每个目录都有 `plugin.ts`，且声明的 `id` 等于目录名 | 页签 id 与配置键/深链目标对不上 |
| 每个插件目录都注册成页签（`chat` 除外），且没有多余 id | 建了目录但页签不出现 |
| 每个页签 id 都有对应插件模块 | 启动恢复时找不到插件 |
| `order` 互不相同且为正 | 顺序取决于文件系统 |
| 左栏顺序 = 按 `order` 排序 | 顺序与声明不符 |
| 常驻列表（`browse`/`view`/`settings`）与 `closable:false` 的插件一致 | 用户能把设置页藏掉，回不来 |
| 每个 `titleKey` / `descriptionKey` 中英都有非空值 | 界面显示原始 key |
| 每个插件的描述文案在自己的 `i18n.ts`，不在全局表 | 加插件要改全局语言表 |
| 每个 `feature` 都是服务端注册表认识的开关 | 打错一个字母 ⇒ 永远显示（读作"开"） |
| 重复 id 保留第一个并告警；无 default 的模块跳过不抛错 | 一个坏文件让整个左栏消失 |
| `dockTabs.ts` 不 import 任何东西 | 一批组件的测试在模块加载时炸（mock 里没有那个图标） |
