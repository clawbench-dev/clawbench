# 03 · 面板：把插件画出来

## 1. 面板放在哪

当前版本，面板由 `web/src/App.vue` 的 `<TabPanel>` 挂载：

```vue
<TabPanel tabId="snippets" :activeTab="leftPanelActive" :noHeader="true">
  <SnippetsPanel :active="panelIsActive('snippets')" />
</TabPanel>
```

`TabPanel` 是共享外壳（`web/src/components/common/TabPanel.vue`）：

- `tabId` 与 `activeTab` 相等时显示；
- 第一次打开后 `everOpened` 变 true，之后用 `v-show` 保活；
- 所以 **`onMounted` 只会跑一次**，切走再切回不会重新挂载；
- 面板需要知道“现在是不是我”的时候，用 `active` prop 或监听它，不要指望 `onMounted`。

## 2. 静态 import 还是懒加载

| 面板 | 建议 |
|---|---|
| 聊天、文件管理、文件预览 | 静态 import（核心、首屏就可能用到） |
| 其余可选面板 | 懒加载 |

懒加载必须走共享封装，不要裸 `defineAsyncComponent`：

```ts
import { buildAsyncComponentOptions } from '@/composables/useAsyncComponent'

const SnippetsPanel = defineAsyncComponent(
  buildAsyncComponentOptions({
    loader: () => import('./components/snippets/SnippetsPanel.vue'),
  }),
)
```

原因：

- 网络抖动时自动重试；
- 重试失败显示可点击的错误页，不会永久转圈；
- 旧页面引用已删除的 chunk 时，会走统一的陈旧 chunk 恢复（`vite:preloadError` + 限频 reload）。

裸 `defineAsyncComponent` 没有 `errorComponent`，失败时会永远显示 loading —— 统计页签曾经因此永久转圈。

## 3. 面板的 props / events 约定

面板不 import `App.vue`，也不 import 别的插件。它需要宿主能力时，用 props / events：

| 动作 | 约定 |
|---|---|
| 打开文件 | `emit('open-file', path)` |
| 引用到聊天 | `emit('quote', payload)` |
| 跳到另一个页签 | `emit('navigate', tabId)` |
| 打开设置 | `emit('open-settings', categoryId)` |
| 请求选择项目 | `emit('request-project')` |

宿主在 `App.vue` 里接这些事件。不要为了“方便”让面板直接 import `switchTab` 或 `App.vue` 的内部函数。

## 4. 空状态

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

## 5. `active` 与轮询

`TabPanel` 保活意味着面板切走后仍然存在。需要轮询/订阅的面板必须看 `active`：

```ts
watch(() => props.active, (active) => {
  if (active) startPolling()
  else stopPolling()
}, { immediate: true })
```

不要用 `onMounted` 启动一个永不停的定时器：用户切走后它还在跑。

## 6. 卸载与状态

当前行为：

- **用户隐藏**（插件管理器的显示开关）：Dock 入口消失，已经挂载的面板保持挂载，状态不丢；
- **功能关闭 / 能力不可用**：`App.vue` 的 watcher 会把当前页签切走，但面板通常仍挂载；
- **应用刷新**：全部重新挂载。

设计目标（下一阶段）是“功能关闭即卸载面板”，释放定时器和内存。当前新增插件不要依赖“面板一定会被卸载”来做清理，清理逻辑要同时挂在 `active` 变化和 `onUnmounted` 上。

## 7. 样式

- 先读 [`docs/spec/client/design-guide.md`](../spec/client/design-guide.md)；
- 几何尺寸用 `calc(N * var(--dp))`，不要裸 px；
- 颜色/间距/圆角用设计 token；
- 设置页里的自定义块可以复用 `SettingsCard`；
- 开关复用全局 `.settings-item__switch` 的 markup，不要在自己的 scoped 样式里重写它的宽高（有守卫测试）。

## 8. 面板检查清单

- [ ] 静态或 `buildAsyncComponentOptions` 懒加载，二选一；
- [ ] 第一次打开能渲染，切走再切回状态还在；
- [ ] 空状态有说明和下一步；
- [ ] `active` 变化时启停轮询/订阅；
- [ ] 不 import `App.vue` / 其他插件；
- [ ] 不裸 `defineAsyncComponent`；
- [ ] 不写裸 px；
- [ ] 有组件测试或至少一个 mount 测试。
