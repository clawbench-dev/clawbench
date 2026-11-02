<template>
  <div class="meeting-empty" :class="{ 'is-fill': fill }">
    <component :is="icon" class="meeting-empty__icon empty-state__icon" />
    <div class="meeting-empty__title">{{ title }}</div>
    <div v-if="desc" class="meeting-empty__desc">{{ desc }}</div>
  </div>
</template>

<script setup lang="ts">
/**
 * The empty state shared by every view of the 会议 tab.
 *
 * A bare muted sentence left at the top-left of a pane reads as "the page
 * failed to load". Every other dock tab answers the same question with a
 * centred glyph, a title and — only when there is something factual to add — a
 * line of context; this component is that shape, declared once so the record
 * list, the record's tabs and the resource pane cannot drift apart.
 *
 * `desc` is optional on purpose: a chat pane with nothing in it has no cause
 * worth explaining, and inventing one would be the kind of filler hint this
 * tab deliberately avoids.
 *
 * `fill` (default) claims the remaining height and centres the block, which is
 * what a pane-level empty state wants. Pass `false` for an inline block that
 * sits under a header of its own (the resource tab).
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
.meeting-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-4, 8px);
  padding: calc(32 * var(--dp)) var(--space-8, 20px);
  text-align: center;
}
/* Pane-level: take the free height so the block sits in the middle of the
   surface rather than hugging the header. */
.meeting-empty.is-fill {
  flex: 1;
  justify-content: center;
}
/* The glyph's size, stroke and colour come from the global
   `.empty-state__icon` rule (css/components.css) — the same one every other
   empty state in the app uses. Nothing local to declare here. */
.meeting-empty__title {
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
}
.meeting-empty__desc {
  max-width: calc(260 * var(--dp));
  font-size: var(--font-size-sm);
  line-height: 1.6;
  color: var(--text-muted);
}
</style>
