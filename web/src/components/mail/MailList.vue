<template>
  <div class="mail-list">
    <div v-if="loading && messages.length === 0" class="mail-list__skeleton">
      <PanelSkeleton variant="list" />
    </div>

    <MailEmpty
      v-else-if="error"
      :icon="TriangleAlert"
      :title="t('mail.list.errorTitle')"
      :desc="error"
    >
      <template #action>
        <button class="fbtn" @click="emit('retry')">{{ t('mail.list.retry') }}</button>
      </template>
    </MailEmpty>

    <MailEmpty
      v-else-if="messages.length === 0"
      :icon="query ? SearchX : Inbox"
      :title="query ? t('mail.list.noResults') : t('mail.list.emptyTitle')"
      :desc="query ? t('mail.list.noResultsDesc', { q: query }) : t('mail.list.emptyDesc')"
    />

    <div v-else ref="bodyRef" class="mail-list__body">
      <MailListItem
        v-for="m in messages"
        :key="m.id"
        :msg="m"
        :active="m.id === activeId"
        @open="(id) => emit('open', id)"
      />
      <div v-if="loadingMore" class="mail-list__more">
        <LoadingIndicator size="sm" inline />
      </div>
      <div ref="sentinel" class="mail-list__sentinel" />
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The message list.
 *
 * Infinite scroll uses an IntersectionObserver sentinel rather than a "load
 * more" button: scrolling back through mail is the normal gesture, and a button
 * every 50 rows turns it into a chore. The observer is disconnected on unmount
 * and whenever the list empties, so a stale observer cannot fire into a
 * component that no longer has rows.
 */
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Inbox, SearchX, TriangleAlert } from 'lucide-vue-next'
import MailEmpty from './MailEmpty.vue'
import MailListItem from './MailListItem.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import type { MailMessage } from '@/utils/mailTypes'

const props = defineProps<{
  messages: MailMessage[]
  loading: boolean
  loadingMore: boolean
  error: string
  hasMore: boolean
  query: string
  /** Message currently open in the reading pane (0 = none). */
  activeId?: number
}>()

const emit = defineEmits<{ open: [id: number]; 'load-more': []; retry: [] }>()

const { t } = useI18n()
const bodyRef = ref<HTMLElement | null>(null)
const sentinel = ref<HTMLElement | null>(null)
let observer: IntersectionObserver | null = null

function disconnect(): void {
  if (observer) {
    observer.disconnect()
    observer = null
  }
}

function connect(): void {
  disconnect()
  if (!sentinel.value) return
  observer = new IntersectionObserver(
    (entries) => {
      if (!entries[0]?.isIntersecting) return
      if (!props.hasMore || props.loadingMore || props.loading) return
      emit('load-more')
    },
    { threshold: 0.1, rootMargin: '120px', root: bodyRef.value ?? undefined },
  )
  observer.observe(sentinel.value)
}

watch(
  () => [props.messages.length, props.hasMore] as const,
  async () => {
    await nextTick()
    if (props.messages.length > 0) connect()
    else disconnect()
  },
  { immediate: true },
)

onBeforeUnmount(disconnect)
</script>

<style scoped>
.mail-list {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}
.mail-list__skeleton {
  padding: var(--space-2, 4px);
}
.mail-list__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}
.mail-list__more {
  display: flex;
  justify-content: center;
  padding: var(--space-3, 6px);
}
.mail-list__sentinel {
  height: 1px;
}
</style>
