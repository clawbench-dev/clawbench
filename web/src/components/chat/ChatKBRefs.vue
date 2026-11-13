<template>
  <div v-if="refs.length" class="chat-kb-refs">
    <!-- Quiet citation line under the reply. The chip states how many passages
         were prepended to the prompt; expanding it shows the exact snippets so
         the reader can audit a claim without leaving the conversation. -->
    <button
      type="button"
      class="chat-kb-refs-toggle"
      data-action="toggle-kb-refs"
      :aria-expanded="open"
      :aria-label="open ? t('chat.message.kbRefs.collapse') : t('chat.message.kbRefs.expand')"
      @click="open = !open"
    >
      <BookOpen :size="12" aria-hidden="true" />
      <span>{{ label }}</span>
      <ChevronDown :size="12" class="chat-kb-refs-chevron" :class="{ 'is-open': open }" aria-hidden="true" />
    </button>

    <div v-if="open" class="chat-kb-refs-list">
      <article v-for="(ref, i) in refs" :key="`${ref.documentId}-${i}`" class="chat-kb-ref">
        <header class="chat-kb-ref-head">
          <span class="chat-kb-ref-title">{{ ref.documentTitle || t('chat.message.kbRefs.untitled') }}</span>
          <span v-if="ref.location" class="chat-kb-ref-location">{{ locationText(ref.location) }}</span>
        </header>
        <div v-if="ref.collectionName" class="chat-kb-ref-collection">
          {{ t('chat.message.kbRefs.from', { name: ref.collectionName }) }}
        </div>
        <p class="chat-kb-ref-text">{{ ref.text }}</p>
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { BookOpen, ChevronDown } from 'lucide-vue-next'
import { formatLocation, type TFn } from '@/utils/kbFormat'
import type { ChatKBRef } from '@/utils/kbRefs'

const props = defineProps<{ refs: ChatKBRef[] }>()

const { t } = useI18n()
const open = ref(false)

const tf: TFn = (key, named) => t(key, named ?? {})

const label = computed(() =>
  t(props.refs.length === 1 ? 'chat.message.kbRefs.summaryOne' : 'chat.message.kbRefs.summary', {
    count: props.refs.length,
  }),
)

function locationText(loc: string): string {
  return formatLocation(loc, tf)
}
</script>

<style scoped>
.chat-kb-refs {
  margin-top: var(--space-2);
  padding: 0 var(--space-6);
}

.chat-kb-refs-toggle {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-2);
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted, #999);
  font-size: var(--font-size-xs);
  font-family: inherit;
  cursor: pointer;
  user-select: none;
}

@media (hover: hover) {
  .chat-kb-refs-toggle:hover {
    color: var(--text-secondary);
    background: color-mix(in srgb, var(--text-secondary) 8%, transparent);
  }
}

.chat-kb-refs-toggle:focus-visible {
  outline: 1px solid var(--accent-color, #0066cc);
  outline-offset: 1px;
}

.chat-kb-refs-chevron {
  transition: transform var(--duration-fast, 0.15s) ease;
}

.chat-kb-refs-chevron.is-open {
  transform: rotate(180deg);
}

.chat-kb-refs-list {
  margin-top: var(--space-2);
  max-height: calc(360 * var(--dp));
  overflow-y: auto;
  overscroll-behavior: contain;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
}

.chat-kb-ref {
  padding: var(--space-4) var(--space-5);
}

.chat-kb-ref + .chat-kb-ref {
  border-top: 1px solid var(--border-color);
}

.chat-kb-ref-head {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-3);
}

.chat-kb-ref-title {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  overflow-wrap: anywhere;
}

.chat-kb-ref-location {
  flex: none;
  padding: 0 var(--space-2);
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-2xs);
}

.chat-kb-ref-collection {
  margin-top: var(--space-1);
  color: var(--text-muted, #999);
  font-size: var(--font-size-2xs);
}

.chat-kb-ref-text {
  margin: var(--space-2) 0 0;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
