# 05 · 预设（profile）：让插件跟着用户的模式调整

## 1. 它是什么

插件管理器里有三个预设：

| 值 | 名称 | 隐藏 |
|---|---|---|
| `developer` | 开发者 | 不隐藏 |
| `standard` | 标准 | 终端、端口映射、Forge |
| `minimal` | 极简 | 所有可关页签 |
| `custom` | 自定义 | 用户手动改过，没有固定语义 |

这个值存在 `panelState` 的 `dockPreset` 里，全局生效，不随项目切换。

## 2. 插件怎么读

```ts
import { usePluginProfile } from '@/composables/useDockPlugins'

const { profile, isDeveloper, isStandard, isMinimal, isCustom } = usePluginProfile()
```

返回值都是响应式 ref / computed：

- `profile.value`：`'developer' | 'standard' | 'minimal' | 'custom'`
- `isDeveloper.value` / `isStandard.value` / `isMinimal.value` / `isCustom.value`

在模板里直接写 `isDeveloper`（顶层 ref 自动解包）；在 script 里写 `isDeveloper.value`。

## 3. 怎么用

### 3.1 推荐

- **开发者**：显示调试面板、原始 JSON、请求日志、高级开关；
- **标准**：隐藏开发相关的高级区块，保留日常操作；
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

### 3.2 不要

- **不要拿预设做权限判断**。它不是安全边界，用户可以随时切换。
- **不要因为 `custom` 就隐藏核心功能**。`custom` 只表示“用户手动调过 Dock”，不代表他不需要某个功能。
- **不要把预设当成功能开关**。功能开关是服务端配置；预设只影响展示。

## 4. 为什么有 `custom`

用户手动隐藏一个页签或移动顺序后，当前状态就不再是任何一个预设。继续声称“你是开发者模式”是不诚实的，所以变成 `custom`。

插件读到 `custom` 时：

- 不要报错；
- 不要假设用户是开发者；
- 回到默认展示即可。

如果用户希望插件重新按“开发者”调整，他可以回设置里再点一次“开发者”预设。

## 5. 预设的持久化

| 键 | 类型 | 作用域 | 默认 |
|---|---|---|---|
| `dockPreset` | string | global | `developer` |
| `dockHidden` | string[] | global | `[]` |
| `dockOrder` | string[] | global | `[]`（默认顺序） |

三者都在 `web/src/utils/panelState.ts` 注册，`followsLayoutMemory: false`：

- 不跟随“布局记忆（全局/按项目）”开关；
- 切换项目不会改变插件配置；
- 所有插件读到的是同一份。

## 6. 测试

```ts
import { applyDockPreset, usePluginProfile } from '@/composables/useDockPlugins'

applyDockPreset('standard')
const profile = usePluginProfile()
expect(profile.isStandard.value).toBe(true)
expect(profile.isDeveloper.value).toBe(false)
```

完整用例见 `web/src/composables/__tests__/useDockPlugins.test.ts`。
