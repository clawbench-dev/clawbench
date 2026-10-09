<template>
  <span
    class="mention-card"
    :class="{ 'is-private': hasNote }"
    :title="cardTitle"
    @click="$emit('click', mention)"
  >
    <!--
      A group member card: the user @-names a member, optionally attaching a
      PRIVATE note (密送) meant for that member alone.

      It deliberately does NOT reuse `.chat-file-attachment` (the file/quote card
      family). Three orthogonal structural signals separate "a person" from "a
      payload", so the two never read alike in any of the 36 themes (colour alone
      is the weakest signal — an accent tint collides across themes):
        - a ROUND agent avatar, vs the file family's square type icon;
        - a PROPORTIONAL-font name, vs the file family's monospace filename;
        - a NEUTRAL surface, vs the file family's accent tint / gradient.
      The lock is the fixed affordance for the private-note feature; whether a
      note is SET is carried by `is-private` (the style), not by the lock's
      presence — hiding it on an empty card would make the feature undiscoverable.
    -->
    <span class="mention-card-avatar">
      <AgentIcon :backend="mention.backend || ''" :name="mention.name" :avatar="avatar" size="sm" />
    </span>
    <span class="mention-card-name">{{ mention.name }}</span>
    <Lock :size="11" class="mention-card-lock" aria-hidden="true" />
    <button
      v-if="removable"
      class="mention-card-close"
      :title="t('common.remove')"
      @click.stop="$emit('remove', mention)"
    >×</button>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Lock } from 'lucide-vue-next'
import AgentIcon from '@/components/common/AgentIcon.vue'
import type { StagedMention } from '@/composables/useChatContext.ts'

const props = withDefaults(defineProps<{
  mention: StagedMention
  /** Show the remove button (input side only — a sent card is not removable). */
  removable?: boolean
  /** The member's custom avatar SVG (resolved from agentId by the caller). */
  avatar?: string
}>(), {
  removable: false,
  avatar: '',
})

defineEmits<{
  click: [mention: StagedMention]
  remove: [mention: StagedMention]
}>()

const { t } = useI18n()

const hasNote = computed(() => !!props.mention.note)

/** Tooltip: the private note when set (it is the more specific content),
 *  otherwise the member name so the card is always identifiable on hover. */
const cardTitle = computed(() => props.mention.note || props.mention.name)
</script>

<style scoped>
.mention-card {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: 40px;
  padding: 0 var(--space-3);
  padding-right: 22px;
  border-radius: var(--radius-full, 999px);
  flex-shrink: 0;
  max-width: 150px;
  position: relative;
  box-sizing: border-box;
  cursor: pointer;
  /* Neutral surface + hairline: the "person" reading. An accent tint is the
     file/quote family's signature and would collapse the distinction. */
  background: var(--bg-tertiary, #f3f4f6);
  border: 1px solid var(--border-color, #d1d5db);
  color: var(--text-primary);
  transition: border-color var(--duration-base), background var(--duration-base);
}

/* A card carrying a private note is marked by an accent BORDER (not a fill), so
   the neutral surface — the person signal — survives in both states. */
.mention-card.is-private {
  border-color: var(--accent-color, #0066cc);
}

@media (hover: hover) {
  .mention-card:hover {
    background: color-mix(in srgb, var(--text-primary) 6%, var(--bg-tertiary, #f3f4f6));
  }
}

/* The avatar is a ROUND box: AgentIcon renders a 20%-radius square, so the
   radius is overridden here (a scoped descendant selector reaches into the
   child's root element). */
.mention-card-avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  border-radius: 50%;
  overflow: hidden;
}

.mention-card-avatar :deep(.agent-icon-img),
.mention-card-avatar :deep(.agent-icon-svg),
.mention-card-avatar :deep(.agent-icon-initial) {
  border-radius: 50%;
}

/* Proportional font: a person's name, not a path. The file family is monospace. */
.mention-card-name {
  font-size: var(--font-size-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.mention-card-lock {
  flex-shrink: 0;
  opacity: 0.55;
}

.mention-card.is-private .mention-card-lock {
  opacity: 1;
  color: var(--accent-color, #0066cc);
}

.mention-card-close {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: none;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  font-size: var(--font-size-2xs);
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background var(--duration-base);
  z-index: 1;
}

@media (hover: hover) {
  .mention-card-close:hover {
    background: var(--color-red);
  }
}
</style>
