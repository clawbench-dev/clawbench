<template>
  <button
    type="button"
    class="mode-switch"
    role="switch"
    :aria-checked="modelValue"
    :aria-label="ariaLabel || undefined"
    @click="$emit('update:modelValue', !modelValue)"
  >
    <span class="mode-switch-label" :class="{ 'is-on': !modelValue }">{{ leftLabel }}</span>
    <span class="mode-switch-track" :class="{ 'is-on': modelValue }"><span class="mode-switch-knob" /></span>
    <span class="mode-switch-label" :class="{ 'is-on': modelValue }">{{ rightLabel }}</span>
  </button>
</template>

<script setup lang="ts">
/**
 * 两档开关：左标签 / 方轨 / 右标签，点整块切换。
 *
 * 表格的「简单表 ⇄ 数据表」和 Excel 预览的「表格 ⇄ 原样」共用这一个实现，
 * 保证两处长得一模一样。刻意不做圆胶囊 —— 轨道和旋钮都是小圆角，跟页签的
 * 硬朗风格配套。
 */
defineProps<{
  modelValue: boolean
  leftLabel: string
  rightLabel: string
  ariaLabel?: string
}>()

defineEmits<{ 'update:modelValue': [value: boolean] }>()
</script>

<style scoped>
.mode-switch {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: 0;
  border: 0;
  background: transparent;
  font-family: inherit;
  cursor: pointer;
}

.mode-switch-label {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  line-height: 1.6;
  white-space: nowrap;
  transition: color var(--duration-base);
}

.mode-switch-label.is-on {
  color: var(--text-primary);
}

.mode-switch-track {
  position: relative;
  flex-shrink: 0;
  width: calc(26 * var(--dp));
  height: calc(13 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: calc(2 * var(--dp));
  background: var(--bg-tertiary);
  transition: border-color var(--duration-base);
}

.mode-switch-knob {
  position: absolute;
  top: calc(1.5 * var(--dp));
  left: calc(1.5 * var(--dp));
  width: calc(8 * var(--dp));
  height: calc(8 * var(--dp));
  border-radius: calc(1 * var(--dp));
  background: var(--text-muted);
  transition: transform var(--duration-base), background var(--duration-base);
}

.mode-switch-track.is-on .mode-switch-knob {
  transform: translateX(calc(13 * var(--dp)));
  background: var(--accent-color);
}

@media (hover: hover) {
  .mode-switch:hover .mode-switch-track {
    border-color: var(--text-muted);
  }

  .mode-switch:hover .mode-switch-label:not(.is-on) {
    color: var(--text-secondary);
  }
}

.mode-switch:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}
</style>
