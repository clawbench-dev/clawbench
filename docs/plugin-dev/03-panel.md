# 03 · 面板：把插件画出来

## 1. 面板放在哪

面板组件放在功能目录，`dock.ts` 的 `panel.load` 指向它：

```ts
// web/src/components/snippets/dock.ts
export const snippetsDock: DockPluginRuntime = {
  panel: {
    load: () => import('./SnippetsPanel.vue'),
  },
}
```

App.vue 不再为每个插件写 `<TabPanel>`。它对除文件管理/文件预览之外的所有左栏页签渲染一个通用的 `DockPanelHost`：

```vue
<TabPanel
  v-for="tab in genericDockTabs"
  :key="tab"
  :tabId="tab"
  :activeTab="leftPanelActive"
  :noHeader="true"
>
  <DockPanelHost :tab-id="tab" :active="panelIsActive(tab)" :host="dockPanelHost" />
</TabPanel>
```

`DockPanelHost` 会：

1. 按 `tabId` 从 `dockPluginRuntime` 取 `panel`；
2. 用 `buildAsyncComponentOptions` 包装 `panel.load`；
3. 把 `active` 传下去；
4. `v-bind` `panel.props(host)`、`v-on` `panel.on(host)`。

## 2. 额外 props 和事件

面板需要宿主的东西时，不要 import App.vue。在 `panel` 里从 `host` 取：

```ts
panel: {
  load: () => import('./ForgePanelContent.vue'),
  props: (host) => ({ projectPath: host.projectPath }),
  on: (host) => ({ 'request-project': host.requestProject }),
}
```

`host` 的字段在 `web/src/composables/dockPluginTypes.ts` 的 `DockPanelHost` 里。当前有：

| 字段 | 用途 |
|---|---|
| `projectPath` | 当前项目根目录 |
| `terminalRequestedCwd` | 终端要打开的目录 |
| `platformUnsupported` | 当前平台是否不支持 PTY |
| `openFile(path)` | 在文件预览里打开文件 |
| `openTaskFile(path)` | 任务打开文件 |
| `quoteMeeting(token)` | 把会议引用到聊天 |
| `openMailSettings()` | 跳到邮件设置 |
| `requestProject()` | 请求选择项目（Forge 用） |
| `openMeetingRelaySettings()` | 跳到会议中继设置 |
| `clearTerminalCwd()` | 终端消费完 cwd 后清空 |

需要新能力时，往这个接口加字段，并在 App.vue 的 `dockPanelHost` 里实现。不要在面板里 import App.vue，也不要让面板直接 import 别的插件。

**例外：引用到聊天 / 拖拽成附件不用加 host 字段。** 这两件事是纯前端状态，直接用共享门面，比给每个插件加一个 host 方法更短：

```ts
const chat = useChatBridge()       // @/composables/useChatBridge
chat.quote(payload)                // 一键进当前会话
chat.comment(payload, { anchorEl })// 开浮条写批注
chat.attach('src/a.ts')            // 附件
```

```html
<div v-chat-drag="{ kind: 'quote', payload }">…</div>
<div v-chat-drag="{ kind: 'attach', path }">…</div>
```

正文要能被划词引用，给容器加 `data-quotable`（身份仍用 `data-quote-*`）。完整说明见 `docs/spec/features/quotes-and-attachments.md` §3.1 / §3.4 / §3.5。

## 3. 懒加载

`DockPanelHost` 统一用共享封装：

```ts
defineAsyncComponent(buildAsyncComponentOptions({ loader: panel.load }))
```

不要自己写裸 `defineAsyncComponent`。共享封装带：

- 网络抖动重试；
- 失败后的可点击错误页；
- 旧页面引用已删除 chunk 时的恢复（`vite:preloadError` + 限频 reload）。

`panel.load` 必须是动态 `import()`，不要 `import XxxPanel from ...` 再赋给 `component`——那会把面板拉回主包。

## 4. `TabPanel` 保活

`TabPanel`（`web/src/components/common/TabPanel.vue`）仍然是外壳：

- `tabId` 与 `activeTab` 相等时显示；
- 第一次打开后 `everOpened` 变 true，之后用 `v-show` 保活；
- 所以 **`onMounted` 只会跑一次**，切走再切回不会重新挂载；
- 面板需要知道“现在是不是我”的时候，用 `active` prop 或监听它。

## 5. 空状态

每个插件都必须能“单独打开”。没有数据时给一个像样的空状态：

- 一句说明现在为什么是空的；
- 一个下一步动作（如果存在）；
- 不要白屏，不要报错，不要把“上游插件被关了”说成“加载失败”。

例子：

| 插件 | 空状态 |
|---|---|
| 文件预览 | 最近文件列表 + “打开文件管理器” |
| 分享 | “还没有分享” + 说明 |
| Forge | “还没有绑定仓库” + 绑定引导 |
| 统计 | “还没有用量数据” |

## 6. `active` 与轮询

`TabPanel` 保活意味着面板切走后仍然存在。需要轮询/订阅的面板必须看 `active`：

```ts
watch(() => props.active, (active) => {
  if (active) startPolling()
  else stopPolling()
}, { immediate: true })
```

不要用 `onMounted` 启动一个永不停的定时器：用户切走后它还在跑。

## 7. 样式

- 先读 [`docs/spec/client/design-guide.md`](../spec/client/design-guide.md)；
- 几何尺寸用 `calc(N * var(--dp))`，不要裸 px；
- 颜色/间距/圆角用设计 token；
- 设置页里的自定义块可以复用 `SettingsCard`；
- 开关复用全局 `.settings-item__switch` 的 markup，不要在自己的 scoped 样式里重写它的宽高（有守卫测试）。

## 8. 面板检查清单

- [ ] `dock.ts` 的 `panel.load` 指向面板；
- [ ] 面板不 import App.vue / 其他插件；
- [ ] 额外 props/events 从 `DockPanelHost` 的 host 取；
- [ ] 空状态有说明和下一步；
- [ ] `active` 变化时启停轮询/订阅；
- [ ] 切走再切回状态还在；
- [ ] 不写裸 px；
- [ ] 有组件测试或至少一个 mount 测试。
