# 05 · 预设（profile）：让插件跟着用户的模式调整

## 1. 它是什么

功能编排里有三个预设，它们**同时决定显示、顺序和服务端开关**：

| 值 | 名称 | 隐藏 / 关闭 | 顺序 |
|---|---|---|---|
| `developer` | 开发 | 全开 | 默认顺序 |
| `standard` | 办公 | 隐藏 Forge / 终端 / 端口映射，关闭 Forge / 终端 | 开发相关三项挪到设置之前 |
| `minimal` | 极简 | 只留常驻四项，其余隐藏并关闭 | 常驻三项在前、设置最后，其余按默认顺序 |
| `custom` | 自定义 | 用户手动改过，没有固定语义 | 用户当前顺序 |

完整数据在 `useDockPlugins.ts` 的 `DOCK_PRESET_PLANS`（每项含 `hidden` / `order` / `features`）。改预设只改那一处。

这个值存在 `panelState` 的 `dockPreset` 里，全局生效，不随项目切换。

## 2. 应用预设

```ts
import { applyDockPreset } from '@/composables/useDockPlugins'

// commit 拿到要写的全部服务端开关，UI 层把它压成一次 PATCH
await applyDockPreset('standard', async (changes) => {
  await patchConfig(toConfigChanges(changes))
})
```

规则：

- `applyDockPreset` 是 **async**，`commit` 只调用一次；
- `commit` 先执行、失败直接抛：本地隐藏集合与顺序一格都不改，所以失败即整体回滚；
- 办公 / 极简会停服务、撤销链接，UI 层必须先确认（`PRESETS_NEEDING_CONFIRM`），取消则什么都不做；
- `resetDockPlugins(commit)` 等价于应用 `developer`。

单测里可以省略 `commit`（只验证本地状态），UI 调用必须传。

## 3. 插件怎么读

```ts
import { usePluginProfile } from '@/composables/useDockPlugins'

const { profile, isDeveloper, isStandard, isMinimal, isCustom } = usePluginProfile()
```

返回值都是响应式 ref / computed：

- `profile.value`：`'developer' | 'standard' | 'minimal' | 'custom'`
- `isDeveloper.value` / `isStandard.value` / `isMinimal.value` / `isCustom.value`

在模板里直接写 `isDeveloper`（顶层 ref 自动解包）；在 script 里写 `isDeveloper.value`。

## 4. 怎么用

### 4.1 推荐

- **开发**：显示调试面板、原始 JSON、请求日志、高级开关；
- **办公**：隐藏开发相关的高级区块，保留日常操作；
- **极简 / 自定义**：回到中性默认，不要猜。

例子：

```vue
<template>
  <section v-if="isDeveloper" class="debug-block">
    <pre>{{ rawPayload }}</pre>
  </section>
</template>

<script setup lang="ts">
import { usePluginProfile } from '@/composables/useDockPlugins'
const { isDeveloper } = usePluginProfile()
</script>
```

### 4.2 不要

- **不要拿预设做权限判断**。它不是安全边界，用户可以随时切换。
- **不要因为 `custom` 就隐藏核心功能**。`custom` 只表示“用户手动调过”，不代表他不需要某个功能。
- **不要把预设当成功能开关**。预设会顺带写服务端开关，但插件自己判断可用性时要读 `isFeatureEnabled(key)`，不要读 `profile`。

## 5. 为什么有 `custom`

用户手动隐藏一个页签、移动顺序，或单独拨动一个开关后，当前状态就不再是任何一个预设。继续声称“你是开发模式”是不诚实的，所以变成 `custom`。

插件读到 `custom` 时：

- 不要报错；
- 不要假设用户是开发者；
- 回到默认展示即可。

如果用户希望重新按“开发”调整，他可以回设置里再点一次“开发”预设。

## 6. 预设的持久化

| 键 | 类型 | 作用域 | 默认 |
|---|---|---|---|
| `dockPreset` | string | global | `developer` |
| `dockHidden` | string[] | global | `[]` |
| `dockOrder` | string[] | global | `[]`（默认顺序） |

三者都在 `web/src/utils/panelState.ts` 注册，`followsLayoutMemory: false`：

- 不跟随“布局记忆（全局/按项目）”开关；
- 切换项目不会改变插件配置；
- 所有插件读到的是同一份。

服务端开关不在这三个键里：它们是服务端全局配置，由 `useFeatureFlags` 读取。

## 7. 测试

```ts
import { applyDockPreset, usePluginProfile } from '@/composables/useDockPlugins'

await applyDockPreset('standard')
const profile = usePluginProfile()
expect(profile.isStandard.value).toBe(true)
expect(profile.isDeveloper.value).toBe(false)
```

失败回滚的用例在 `useDockPlugins.test.ts`：传入一个 reject 的 `commit`，断言预设、隐藏集合、顺序全部保持原样。
