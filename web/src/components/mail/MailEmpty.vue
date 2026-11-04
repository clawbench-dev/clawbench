<template>
  <div class="mail-empty" :class="{ 'is-fill': fill }">
    <component :is="icon" class="mail-empty__icon empty-state__icon" />
    <div class="mail-empty__title">{{ title }}</div>
    <div v-if="desc" class="mail-empty__desc">{{ desc }}</div>
    <div v-if="$slots.action" class="mail-empty__action">
      <slot name="action" />
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The empty state shared by every view of the 邮件 tab.
 *
 * Same shape as the meeting panel's: a centred glyph, a title, and an optional
 * line of context. A bare muted sentence at the top-left of a pane reads as
 * "the page failed to load".
 */
import type { Component } from 'vue'

withDefaults(
  defineProps<{
    icon: Component
    title: string
    desc?: string
    fill?: boolean
  }>(),
  { desc: '', fill: true },
)
</script>

<style scoped>
.mail-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-4, 8px);
  padding: calc(32 * var(--dp)) var(--space-8, 20px);
  text-align: center;
}
.mail-empty.is-fill {
  flex: 1;
  justify-content: center;
}
.mail-empty__title {
  font-size: var(--font-size-md, 14px);
  color: var(--text-primary);
}
.mail-empty__desc {
  max-width: calc(280 * var(--dp));
  font-size: var(--font-size-sm, 12px);
  line-height: 1.6;
  color: var(--text-secondary);
}
.mail-empty__action {
  margin-top: var(--space-2, 4px);
}
</style>
