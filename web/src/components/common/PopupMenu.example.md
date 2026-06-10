# PopupMenu Component

通用弹出菜单组件，处理视口边界约束、溢出滚动、外部点击关闭。

## Props

| Prop | Type | Default | Description |
|------|------|---------|-------------|
| `show` | `Boolean` | `false` | 菜单显示状态（支持 v-model） |
| `targetElement` | `Object` | - | 锚点 DOM 元素，菜单相对于此元素定位 |
| `anchor` | `String` | `'auto'` | 水平对齐：`'left'` \| `'right'` \| `'auto'`（auto 根据锚点位置自动判断） |
| `maxWidth` | `Number` | `220` | 菜单最大宽度（px） |
| `maxHeight` | `Number` | `320` | 菜单最大高度（px），超出显示滚动条 |
| `edgeMargin` | `Number` | `6` | 与屏幕边缘的最小距离（px） |
| `menuItemsCount` | `Number` | `10` | 菜单项数量，用于高度估算 |

## Events

| Event | Payload | Description |
|-------|---------|-------------|
| `update:show` | `Boolean` | 菜单显示状态变化（外部点击时自动关闭） |

## Features

- **视口边界约束**：自动调整位置避免菜单溢出屏幕
- **溢出滚动**：菜单高度超过 `maxHeight` 时自动显示滚动条
- **外部点击关闭**：点击菜单外部自动关闭
- **平滑动画**：淡入淡出动画
- **左右锚点**：支持左对齐和右对齐两种模式

## Usage

### Basic Example

```vue
<template>
  <div>
    <button ref="buttonRef" @click="showMenu = true">Open Menu</button>

    <PopupMenu v-model:show="showMenu" :target-element="buttonRef">
      <div class="menu-item" @click="handleAction1">Action 1</div>
      <div class="menu-item" @click="handleAction2">Action 2</div>
      <div class="menu-item" @click="handleAction3">Action 3</div>
    </PopupMenu>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import PopupMenu from '@/components/common/PopupMenu.vue'

const buttonRef = ref(null)
const showMenu = ref(false)

function handleAction1() {
  showMenu.value = false
  // handle action...
}
</script>

<style scoped>
.menu-item {
  padding: 8px 12px;
  cursor: pointer;
}
.menu-item:hover {
  background: var(--accent-color, #0066cc);
  color: #fff;
}
</style>
```

### Right-Anchored Menu

```vue
<PopupMenu
  v-model:show="showMenu"
  :target-element="sendBtnRef"
  anchor="right"
  :max-width="260"
  :menu-items-count="Object.keys(quickSend).length"
>
  <div class="menu-title">Quick Actions</div>
  <button
    v-for="(value, key) in quickSend"
    :key="key"
    class="menu-item"
    @click="handleQuickSend(value)"
  >
    {{ key }}
  </button>
</PopupMenu>
```

### Long Menu with Scroll

```vue
<PopupMenu
  v-model:show="showMenu"
  :target-element="modelChipRef"
  :max-width="220"
  :max-height="320"
  :menu-items-count="agentModels.length"
>
  <div class="menu-title">Select Model</div>
  <button
    v-for="m in agentModels"
    :key="m.id"
    class="menu-item"
    :class="{ active: m.id === currentModelId }"
    @click="selectModel(m)"
  >
    <Check v-if="m.id === currentModelId" :size="14" />
    <span>{{ m.name }}</span>
  </button>
</PopupMenu>
```

## Implementation Details

### Positioning Algorithm

1. **水平方向**（anchor='left'）：
   - 默认对齐锚点左侧
   - 右溢时向左收缩
   - 左溢时向右推

2. **水平方向**（anchor='right'）：
   - 默认对齐锚点右侧
   - 右溢时向左收缩
   - 左溢时向右推

3. **垂直方向**：
   - 默认显示在锚点上方
   - 上方空间不足时翻转到下方
   - 仍不足时clamped到视口顶部

### Outside Click Handling

使用 `click` 事件监听器，检测点击是否在以下范围内：
- 锚点元素内部 → 忽略
- 菜单内部 → 忽略
- 其他区域 → 关闭菜单

### Animation

使用 Vue `<Transition>` 组件实现：
- 进入：`opacity: 0` → `1`, `translateY(-4px)` → `0`
- 离开：`opacity: 1` → `0`, `translateY(0)` → `(-4px)`
- 持续时间：150ms

## Related Components

- `ChatInputBar.vue` — 使用 PopupMenu 的实例（附件菜单、快捷发送菜单、模型选择菜单）
