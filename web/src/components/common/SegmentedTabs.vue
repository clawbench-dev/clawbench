<template>
  <div class="segmented-tabs" role="tablist">
    <button
      v-for="opt in options"
      :key="opt.value"
      type="button"
      class="segmented-tab"
      :class="{ active: modelValue === opt.value }"
      role="tab"
      :aria-selected="modelValue === opt.value"
      :data-mode="opt.value"
      @click.stop="$emit('update:modelValue', opt.value)"
    >{{ opt.label }}</button>
  </div>
</template>

<script setup lang="ts">
/**
 * 两段式页签（分段控件）：一排紧贴的按钮，选中项用主题色淡底。
 *
 * 与 ModeSwitch 的区别：ModeSwitch 是「开关」，语义是开/关两态；
 * 这里切换的是两个并列的视图，所以用 role="tablist" 而不是 role="switch"。
 *
 * 布局由调用方决定（组件根不带外边距）——放进 header 右侧时由调用方加
 * `margin-left: auto`，因为「靠右」是那个 header 的布局要求，不是分段控件的。
 */
defineProps<{
  modelValue: string
  /** Two or more mutually exclusive choices. `value` must be unique. */
  options: Array<{ value: string; label: string }>
}>()

defineEmits<{ 'update:modelValue': [value: string] }>()
</script>

<style scoped>
.segmented-tabs {
  display: inline-flex;
  align-items: stretch;
  border: 1px solid var(--border-color, #ccc);
  border-radius: var(--radius-sm);
  overflow: hidden;
}

.segmented-tab {
  border: none;
  background: none;
  color: var(--text-secondary, #666);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  line-height: 1;
  padding: calc(4 * var(--dp)) var(--space-4);
  cursor: pointer;
  white-space: nowrap;
  transition: color var(--duration-base), background var(--duration-base);
}

.segmented-tab + .segmented-tab {
  border-left: 1px solid var(--border-color, #ccc);
}

.segmented-tab.active {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}

@media (hover: hover) {
  .segmented-tab:not(.active):hover {
    color: var(--text-primary, #1a1a1a);
    background: color-mix(in srgb, var(--text-primary) 5%, transparent);
  }
}
</style>
