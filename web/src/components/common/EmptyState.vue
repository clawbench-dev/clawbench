<template>
  <div class="empty-state" :class="{ 'is-fill': fill }">
    <!-- The glyph is either a component passed through `icon`, or anything the
         caller drops into `#icon` (an avatar, a file-type icon that needs its
         own props). Both end at the same global `.empty-state__icon` rule. -->
    <slot name="icon">
      <component :is="icon" v-if="icon" class="empty-state__icon" />
    </slot>
    <div class="empty-state__title">{{ title }}</div>
    <div v-if="desc || $slots.desc" class="empty-state__desc">
      <slot name="desc">{{ desc }}</slot>
    </div>
    <div v-if="$slots.action" class="empty-state__action">
      <slot name="action" />
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The one empty state every tab and sub-tab draws.
 *
 * "Nothing here yet" used to be written ~20 times with its own class name,
 * spacing, type scale and vertical alignment: seven of them hugged the top of
 * the pane (the block had padding but no height to centre in), two sized their
 * own glyph (40px, 30px), and the copy ranged from one muted line to a title +
 * description + button. Same fact, ten shapes.
 *
 * This component is that shape, declared once: a centred glyph, a title, an
 * optional line of context and an optional action. The glyph's size, stroke and
 * colour still come from the global `.empty-state__icon` rule in
 * `css/components.css` — call sites never size it.
 *
 * `fill` (default) claims the remaining height and centres the block in the
 * pane. Pass `false` for a block that sits inside a section of its own (a
 * collapsible list section, a detail page that already has content above), where
 * there is no height to centre in and the block should only centre itself
 * horizontally.
 *
 * `desc` is optional on purpose: inventing a hint for a pane with nothing to
 * explain is filler. A slot exists for the few places that need more than one
 * line (the chat welcome cards keep their agent/group detail there).
 */
import type { Component } from 'vue'

withDefaults(
  defineProps<{
    icon?: Component
    title: string
    desc?: string
    fill?: boolean
  }>(),
  { desc: '', fill: true },
)
</script>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-5);
  padding: calc(32 * var(--dp)) var(--space-8);
  text-align: center;
  color: var(--text-muted);
}

/* Pane-level: take the free height so the block sits in the middle of the
   surface rather than hugging the header. The parent must be a flex column —
   every tab panel is (`TabPanel` → `.bs-body`), and the few scroll regions that
   hold an empty state were made one for this. */
.empty-state.is-fill {
  flex: 1;
  min-height: 0;
}

.empty-state__title {
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
}

.empty-state__desc {
  max-width: calc(280 * var(--dp));
  font-size: var(--font-size-sm);
  line-height: 1.6;
  color: var(--text-muted);
}

.empty-state__action {
  margin-top: var(--space-2);
}
</style>
