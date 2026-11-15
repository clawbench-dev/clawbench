<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <FileText :size="16" class="modal-header-icon" />
      <span class="modal-title">{{ t('kb.drawerTitle') }}</span>
    </template>

    <div class="kb-drawer">
      <div v-if="loading" class="kb-drawer-hint">{{ t('common.loading') }}</div>

      <div v-else-if="error" class="kb-hint kb-hint--error">
        <AlertTriangle :size="16" />
        <span>{{ error }}</span>
        <button class="kb-link" type="button" @click="load">{{ t('common.retry') }}</button>
      </div>

      <template v-else-if="detail">
        <header class="kb-drawer-head">
          <span class="kb-drawer-title" :title="detail.document.title">{{ detail.document.title }}</span>
          <span class="kb-drawer-status" :class="`is-${kindOf(detail.document)}`">
            {{ labelOf(detail.document) }}
          </span>
        </header>

        <div class="kb-drawer-meta">
          <span>{{ formatSize(detail.document.size_bytes) }}</span>
          <template v-if="detail.document.chunk_count > 0">
            <span aria-hidden="true">·</span>
            <span>{{ t('kb.chunks', { count: detail.document.chunk_count }) }}</span>
          </template>
          <template v-if="detail.document.status === 'ready' && vectorOn">
            <span aria-hidden="true">·</span>
            <span :class="{ 'is-muted': !detail.document.vector_ready }">
              {{ detail.document.vector_ready ? t('kb.vectorReady') : t('kb.vectorPending') }}
            </span>
          </template>
        </div>

        <div v-if="usage" class="kb-drawer-row">
          <span class="kb-drawer-label">{{ t('kb.usageTitle') }}</span>
          <span class="kb-drawer-value">{{ usageText }}</span>
        </div>

        <div v-if="detail.document.source_path" class="kb-drawer-row">
          <span class="kb-drawer-label">{{ t('kb.drawerSource') }}</span>
          <span class="kb-drawer-value" :title="detail.document.source_path">{{ detail.document.source_path }}</span>
        </div>

        <div class="kb-drawer-tags">
          <Tag :size="12" class="kb-drawer-tag-icon" aria-hidden="true" />
          <input
            v-model="tagDraft"
            class="kb-drawer-tag-input"
            data-role="kb-tag-input"
            :placeholder="t('kb.tagsPlaceholder')"
            @keydown.enter="onSaveTags"
          />
          <button class="kb-drawer-tag-save" type="button" data-action="kb-save-tags" @click="onSaveTags">
            {{ t('kb.tagsSave') }}
          </button>
        </div>

        <div v-if="detail.document.error" class="kb-drawer-error" role="alert">
          {{ errorText }}
        </div>
        <div v-if="detail.document.warning === 'truncated'" class="kb-drawer-warning">
          {{ t('kb.warningTruncated') }}
        </div>
        <div v-if="detail.document.source_missing" class="kb-drawer-warning">
          {{ t('kb.sourceMissingHint') }}
        </div>
        <div v-else-if="detail.document.source_stale" class="kb-drawer-warning">
          {{ t('kb.sourceStaleHint') }}
        </div>

        <div class="kb-drawer-section">
          <span>{{ t('kb.drawerChunks') }}</span>
          <span class="kb-drawer-count">{{ detail.chunks.length }}</span>
        </div>

        <div v-if="detail.chunks.length === 0" class="kb-drawer-hint">
          {{ t('kb.drawerNoChunksDesc') }}
        </div>

        <article
          v-for="chunk in detail.chunks"
          :key="chunk.id"
          class="kb-drawer-chunk"
          :class="{ 'is-focus': chunk.id === detail.focus_chunk_id }"
          :data-kb-chunk="chunk.id"
        >
          <header class="kb-drawer-chunk-head">
            <span v-if="chunk.location" class="kb-drawer-loc">{{ locationText(chunk.location) }}</span>
            <span class="kb-drawer-index">#{{ chunk.chunk_index + 1 }}</span>
            <span v-if="chunk.id === detail.focus_chunk_id" class="kb-drawer-focus">
              {{ t('kb.drawerFocus') }}
            </span>
            <button
              class="kb-drawer-quote"
              type="button"
              data-action="kb-quote-chunk"
              :title="t('kb.quoteChunk')"
              @click="onQuote(chunk)"
            >
              <MessageSquare :size="12" aria-hidden="true" />
              <span>{{ t('kb.quoteChunk') }}</span>
            </button>
          </header>
          <div class="kb-drawer-text markdown-body" v-html="chunkHtml(chunk)" />
        </article>
      </template>
    </div>

    <template #footer>
      <button class="fbtn" type="button" @click="$emit('close')">{{ t('common.close') }}</button>
      <button
        class="fbtn"
        type="button"
        :disabled="!detail || !detail.document.source_path"
        :title="detail?.document.source_path ? t('kb.refreshFromSourceHint') : t('kb.refreshNoSource')"
        data-action="kb-refresh-doc"
        @click="onRefresh"
      >
        {{ t('kb.refreshFromSource') }}
      </button>
      <button class="fbtn" type="button" :disabled="!detail" @click="onReindex">{{ t('kb.reindex') }}</button>
      <button class="fbtn fbtn-danger" type="button" :disabled="!detail" @click="onDelete">
        {{ t('common.delete') }}
      </button>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, FileText, MessageSquare, Tag } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import {
  fetchDocumentDetail,
  fetchDocumentUsage,
  type KBChunk,
  type KBDocumentDetail,
} from '@/composables/useKnowledgeBase'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer.ts'
import {
  documentErrorText,
  formatLocation,
  formatSize,
  statusKind,
  statusLabel,
  type TFn,
} from '@/utils/kbFormat'

const props = defineProps<{
  open: boolean
  documentId: number | null
  /** Passage to centre on; 0 means "start at the top". */
  focusChunkId?: number
  /** Whether a semantic index exists; when not, `vector_ready` is always false
   *  and must not be presented as "indexing". */
  vectorEnabled?: boolean
}>()

const vectorOn = computed(() => props.vectorEnabled !== false)

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'loaded', detail: KBDocumentDetail): void
  (e: 'reindex', id: number): void
  (e: 'refresh', id: number): void
  (e: 'quote', payload: { text: string; title: string; location?: string }): void
  (e: 'save-tags', payload: { id: number; tags: string }): void
  (e: 'delete', payload: { id: number; title: string }): void
}>()

const { t } = useI18n()

const loading = ref(false)
const error = ref('')
const detail = ref<KBDocumentDetail | null>(null)
const tagDraft = ref('')
const usage = ref<{ count: number; lastUsed: string } | null>(null)

const tf: TFn = (key, named) => t(key, named ?? {})

const usageText = computed(() => {
  if (!usage.value) return ''
  const date = new Date(usage.value.lastUsed)
  if (Number.isNaN(date.getTime())) return t('kb.usageCount', { count: usage.value.count })
  return t('kb.usageRecent', {
    count: usage.value.count,
    date: date.toLocaleDateString(),
  })
})

const errorText = computed(() =>
  detail.value ? documentErrorText(detail.value.document.error, tf) : '',
)

function kindOf(doc: KBDocumentDetail['document']): string {
  return statusKind(doc, vectorOn.value)
}

function labelOf(doc: KBDocumentDetail['document']): string {
  return statusLabel(doc, tf, vectorOn.value)
}

function locationText(loc?: string): string {
  return formatLocation(loc, tf)
}

const htmlCache = new Map<number, string>()

function chunkHtml(chunk: KBChunk): string {
  let html = htmlCache.get(chunk.id)
  if (html === undefined) {
    html = renderMarkdownHtml(chunk.chunk_text, { skipEnhancements: true, wrapTables: false })
    htmlCache.set(chunk.id, html)
  }
  return html
}

async function load(): Promise<void> {
  const id = props.documentId
  if (!id) return
  loading.value = true
  error.value = ''
  detail.value = null
  usage.value = null
  htmlCache.clear()
  try {
    const res = await fetchDocumentDetail(id, props.focusChunkId)
    detail.value = res
    tagDraft.value = res.document.tags ?? ''
    emit('loaded', res)
    void loadUsage(res.document.collection_id, res.document.id)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

watch(
  () => [props.open, props.documentId, props.focusChunkId],
  () => {
    if (props.open) void load()
  },
  { immediate: true },
)

function onReindex(): void {
  if (detail.value) emit('reindex', detail.value.document.id)
}

function onRefresh(): void {
  if (detail.value?.document.source_path) emit('refresh', detail.value.document.id)
}

async function loadUsage(collectionId: number, documentId: number): Promise<void> {
  try {
    const rows = await fetchDocumentUsage(collectionId)
    const row = rows.find((r) => r.document_id === documentId)
    usage.value = row ? { count: row.count, lastUsed: row.last_used } : null
  } catch {
    // Usage is decorative; never fail the drawer over it.
    usage.value = null
  }
}

function onSaveTags(): void {
  if (!detail.value) return
  const tags = tagDraft.value.trim()
  emit('save-tags', { id: detail.value.document.id, tags })
  detail.value.document.tags = tags
}

function onQuote(chunk: KBChunk): void {
  if (!detail.value) return
  emit('quote', {
    text: chunk.chunk_text,
    title: detail.value.document.title,
    location: chunk.location,
  })
}

function onDelete(): void {
  if (!detail.value) return
  emit('delete', { id: detail.value.document.id, title: detail.value.document.title })
}

defineExpose({ reload: load })
</script>

<style scoped>
/* BottomSheet's body has no padding of its own. */
.kb-drawer {
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.kb-drawer-hint {
  padding: var(--space-4) 0;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

.kb-drawer-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.kb-drawer-title {
  min-width: 0;
  color: var(--text-primary);
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
  overflow-wrap: anywhere;
}

.kb-drawer-status {
  flex: none;
  padding: 1px var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
}

.kb-drawer-status.is-ready {
  background: color-mix(in srgb, var(--color-green) 12%, transparent);
  color: var(--color-green);
}

.kb-drawer-status.is-fts {
  background: color-mix(in srgb, var(--color-orange) 12%, transparent);
  color: var(--color-orange);
}

.kb-drawer-status.is-failed {
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
  color: var(--color-red);
}

.kb-drawer-status.is-working,
.kb-drawer-status.is-pending {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  color: var(--accent-color);
}

.kb-drawer-meta .is-muted {
  color: var(--text-muted);
}

.kb-drawer-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

.kb-drawer-row {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  min-width: 0;
  font-size: var(--font-size-xs);
}

.kb-drawer-label {
  flex: none;
  color: var(--text-muted);
}

.kb-drawer-value {
  min-width: 0;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-drawer-error {
  padding: var(--space-3);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red);
  font-size: var(--font-size-sm);
  line-height: 1.45;
}

.kb-drawer-warning {
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--color-orange);
  font-size: var(--font-size-xs);
}

.kb-drawer-tags {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.kb-drawer-tag-icon {
  flex: none;
  color: var(--text-muted);
}

.kb-drawer-tag-input {
  flex: 1;
  min-width: 0;
  height: calc(28 * var(--dp));
  padding: 0 var(--space-2);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-xs);
}

.kb-drawer-tag-save {
  flex: none;
  height: calc(28 * var(--dp));
  padding: 0 var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: var(--font-size-2xs);
  cursor: pointer;
}

.kb-drawer-section {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-2);
  padding-bottom: var(--space-2);
  border-bottom: 1px solid var(--border-color);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.kb-drawer-count {
  font-variant-numeric: tabular-nums;
}

.kb-drawer-chunk {
  padding: var(--space-3) var(--space-3) var(--space-3) var(--space-4);
  border: 1px solid transparent;
  border-left: 3px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}

.kb-drawer-chunk.is-focus {
  border-color: color-mix(in srgb, var(--accent-color) 45%, transparent);
  border-left-color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 8%, var(--bg-secondary));
}

.kb-drawer-chunk-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-bottom: var(--space-2);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
}

.kb-drawer-loc {
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.kb-drawer-index {
  font-variant-numeric: tabular-nums;
}

.kb-drawer-focus {
  color: var(--accent-color);
}

.kb-drawer-quote {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  margin-left: auto;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  cursor: pointer;
}

@media (hover: hover) {
  .kb-drawer-quote:hover {
    color: var(--text-secondary);
  }
}

.kb-drawer-text {
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.55;
  overflow-wrap: anywhere;
}

/* Compact reading rhythm: a chunk is a fragment, not a page. */
.kb-drawer-text :deep(h1),
.kb-drawer-text :deep(h2),
.kb-drawer-text :deep(h3),
.kb-drawer-text :deep(h4),
.kb-drawer-text :deep(p),
.kb-drawer-text :deep(ul),
.kb-drawer-text :deep(ol) {
  margin: calc(4 * var(--dp)) 0;
  font-size: inherit;
  line-height: inherit;
  color: inherit;
}

.kb-drawer-text :deep(h1),
.kb-drawer-text :deep(h2),
.kb-drawer-text :deep(h3),
.kb-drawer-text :deep(h4) {
  font-weight: var(--font-weight-semibold);
  border: 0;
  padding: 0;
}
</style>
