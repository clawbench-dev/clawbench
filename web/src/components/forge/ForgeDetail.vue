<template>
  <div class="forge-detail">
    <!-- Header: breadcrumb-style back + actions -->
    <div class="forge-detail-header">
      <button class="forge-back" @click="emit('back')">
        <ChevronLeft :size="18" />
        <span>{{ t('forge.detail.back') }}</span>
      </button>
      <div class="forge-detail-actions">
        <a
          v-if="detail.item.value"
          class="forge-icon-btn"
          :href="detail.item.value.url"
          target="_blank"
          rel="noopener noreferrer"
          :title="t('forge.detail.openBrowser')"
        >
          <ExternalLink :size="16" />
        </a>
      </div>
    </div>

    <div v-if="detail.loading.value" class="forge-loading">
      <LoadingIndicator size="md" :label="t('forge.loading')" />
    </div>

    <div v-else-if="detail.error.value" class="forge-error-card">
      <div class="forge-error-title">{{ t('forge.error.generic') }}</div>
      <div class="forge-error-body">{{ detail.error.value.message }}</div>
    </div>

    <template v-else-if="detail.item.value">
      <div class="forge-detail-body">
        <!-- Title + meta -->
        <div class="forge-detail-title-row">
          <span class="forge-state-dot" :class="`state-${detail.item.value.state}`"></span>
          <span class="forge-detail-number">#{{ detail.item.value.number }}</span>
          <h2 class="forge-detail-title">{{ detail.item.value.title }}</h2>
        </div>
        <div class="forge-detail-meta">
          <span>{{ detail.item.value.author }}</span>
          <span>{{ formatTime(detail.item.value.createdAt) }}</span>
          <span v-if="detail.item.value.state === 'merged'" class="forge-merged">{{ t('forge.state.merged') }}</span>
          <span v-else-if="detail.item.value.state === 'closed'" class="forge-closed">{{ t('forge.state.closed') }}</span>
          <span v-else class="forge-open">{{ t('forge.state.open') }}</span>
        </div>

        <!-- Body rendered through the shared markdown pipeline -->
        <div
          v-if="detail.item.value.body"
          class="forge-detail-content markdown-body"
          v-html="renderedBody"
        ></div>

        <!-- Comments -->
        <div class="forge-comments">
          <button
            v-if="detail.hasMoreComments.value"
            class="forge-load-more-comments"
            :disabled="detail.loadingComments.value"
            @click="detail.loadOlderComments()"
          >
            {{ detail.loadingComments.value ? t('forge.loading') : t('forge.detail.loadOlder') }}
          </button>
          <div v-for="c in detail.comments.value" :key="c.id" class="forge-comment">
            <div class="forge-comment-meta">
              <span class="forge-comment-author">{{ c.author }}</span>
              <span class="forge-comment-time">{{ formatTime(c.createdAt) }}</span>
            </div>
            <div class="forge-comment-body markdown-body" v-html="renderComment(c.body)"></div>
          </div>
        </div>
      </div>

      <!-- Primary action pinned to the bottom bar -->
      <div class="forge-detail-footer">
        <button class="forge-analyze-btn" @click="onAnalyze">
          <Sparkles :size="16" />
          <span>{{ t('forge.detail.analyze') }}</span>
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ExternalLink, Sparkles } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useForgeDetail } from '@/composables/useForge'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { appLog } from '@/utils/appLog'

const TAG = 'ForgeDetail'
const props = defineProps<{
  type: 'issue' | 'pr'
  number: number
}>()
const emit = defineEmits<{
  (e: 'back'): void
  (e: 'analyze', payload: { item: { type: 'issue' | 'pr'; number: number; title: string; url: string; slug: string; body: string } }): void
}>()

const { t } = useI18n()
const detail = useForgeDetail()

onMounted(() => {
  void detail.open(props.type, props.number)
})

const renderedBody = computed(() => {
  const body = detail.item.value?.body ?? ''
  return body ? renderMarkdownHtml(body) : ''
})

function renderComment(body: string): string {
  try {
    return renderMarkdownHtml(body || '')
  } catch (err) {
    appLog.w(TAG, 'render comment failed', err)
    return ''
  }
}

function onAnalyze() {
  const it = detail.item.value
  if (!it) return
  emit('analyze', {
    item: { type: it.type, number: it.number, title: it.title, url: it.url, slug: it.slug, body: it.body ?? '' },
  })
}

function formatTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString()
}
</script>

<style scoped>
.forge-detail {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.forge-detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
.forge-back {
  display: flex;
  align-items: center;
  gap: 4px;
  background: transparent;
  border: none;
  color: var(--accent-color);
  cursor: pointer;
  font-size: 14px;
}
.forge-icon-btn {
  color: var(--text-secondary);
  display: flex;
  align-items: center;
  padding: 6px;
}
.forge-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}
.forge-error-card {
  margin: 12px;
  padding: 14px;
  border: 1px solid var(--border-color);
  border-radius: 10px;
  background: var(--bg-secondary);
}
.forge-error-title { font-weight: 600; margin-bottom: 6px; }
.forge-error-body { color: var(--text-secondary); font-size: 13px; }
.forge-detail-body {
  flex: 1;
  overflow-y: auto;
  padding: 14px;
}
.forge-detail-title-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.forge-detail-number { color: var(--text-muted); }
.forge-detail-title {
  font-size: 18px;
  margin: 0;
  line-height: 1.35;
}
.forge-detail-meta {
  display: flex;
  gap: 12px;
  margin: 8px 0 14px;
  color: var(--text-muted);
  font-size: 13px;
}
.forge-open { color: #2da44e; }
.forge-closed { color: #cf222e; }
.forge-merged { color: #8250df; }
.forge-detail-content {
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border-color);
}
.forge-comments {
  margin-top: 14px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.forge-load-more-comments {
  align-self: center;
  padding: 6px 14px;
  border: 1px solid var(--border-color);
  border-radius: 999px;
  background: transparent;
  color: var(--accent-color);
  cursor: pointer;
}
.forge-comment {
  border: 1px solid var(--border-color);
  border-radius: 10px;
  overflow: hidden;
}
.forge-comment-meta {
  display: flex;
  gap: 10px;
  padding: 8px 12px;
  background: var(--bg-secondary);
  font-size: 12px;
  color: var(--text-muted);
}
.forge-comment-author { font-weight: 600; color: var(--text-primary); }
.forge-comment-body { padding: 10px 12px; }
.forge-detail-footer {
  flex-shrink: 0;
  padding: 10px 14px;
  border-top: 1px solid var(--border-color);
}
.forge-analyze-btn {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 12px;
  border: none;
  border-radius: 10px;
  background: var(--accent-color);
  color: #fff;
  font-size: 15px;
  cursor: pointer;
}
.forge-state-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  display: inline-block;
}
.forge-state-dot.state-open { background: #2da44e; }
.forge-state-dot.state-closed { background: #cf222e; }
.forge-state-dot.state-merged { background: #8250df; }
</style>
