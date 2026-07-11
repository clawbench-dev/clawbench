<template>
  <div v-if="loading" class="git-diff-loading">
    <div class="spinner" style="width:24px;height:24px;border-width:2px;margin:0 auto;" />
  </div>
  <div v-else-if="empty" class="git-diff-empty">{{ t('git.diffView.noChanges') }}</div>
  <div v-else :class="['git-diff-scroll', { 'no-wrap': noWrap }]" v-html="html" @click="onDiffClick" />
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()

defineProps({
  loading: Boolean,
  empty: Boolean,
  html: { type: String, default: '' },
  noWrap: Boolean,
})

function onDiffClick(event: MouseEvent) {
  const target = event.target as HTMLElement
  const btn = target.closest('.diff-hunk-wrap-btn, .diff-hunk-linum-btn')
  if (!btn) return

  event.preventDefault()
  event.stopPropagation()

  const hunk = btn.closest('.diff-hunk')
  if (!hunk) return

  const action = btn.getAttribute('data-action')

  if (action === 'wrap') {
    hunk.classList.toggle('diff-hunk-wrap')
    btn.classList.toggle('is-wrapped')
    const isWrapped = hunk.classList.contains('diff-hunk-wrap')
    btn.setAttribute('title', isWrapped ? t('diffBlock.wrapOn') : t('diffBlock.wrapOff'))
  } else if (action === 'linum') {
    hunk.classList.toggle('diff-hunk-no-linum')
    btn.classList.toggle('is-on')
    const isOn = !hunk.classList.contains('diff-hunk-no-linum')
    btn.setAttribute('title', isOn ? t('diffBlock.lineNumOn') : t('diffBlock.lineNumOff'))
  }
}
</script>

<style scoped>
.git-diff-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.git-diff-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted, #999);
  font-size: 14px;
}

.git-diff-scroll {
  padding: 0;
  -webkit-overflow-scrolling: touch;
}

/* Unified diff layout */
.git-diff-scroll :deep(.diff-unified-view) {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.git-diff-scroll :deep(.diff-hunk) {
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-sm, 4px);
  overflow: hidden;
}

/* ─── Header bar (flex: func name + actions) ─── */
.git-diff-scroll :deep(.diff-hunk-header) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-family: 'SF Mono', 'Fira Code', Menlo, monospace;
  background: var(--bg-tertiary, #f0f0f0);
  padding: 3px 8px;
  user-select: none;
  min-height: 24px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.git-diff-scroll :deep(.diff-hunk-func) {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary, #555);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ─── Header actions ─── */
.git-diff-scroll :deep(.diff-hunk-actions) {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}

.git-diff-scroll :deep(.diff-hunk-wrap-btn),
.git-diff-scroll :deep(.diff-hunk-linum-btn) {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  border-radius: 3px;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  padding: 0;
  opacity: 0.5;
  transition: opacity 0.15s, color 0.15s, background 0.15s;
  outline: none;
  box-shadow: none;
}

.git-diff-scroll :deep(.diff-hunk-wrap-btn:hover),
.git-diff-scroll :deep(.diff-hunk-linum-btn:hover) {
  opacity: 1;
  color: var(--text-secondary, #555);
  background: var(--bg-secondary, #e9ecef);
}

.git-diff-scroll :deep(.diff-hunk-wrap-btn:active),
.git-diff-scroll :deep(.diff-hunk-linum-btn:active) {
  background: var(--border-color, #dee2e6);
}

.git-diff-scroll :deep(.diff-hunk-wrap-btn.is-wrapped) {
  opacity: 0.8;
  color: var(--accent-color, #4a90d9);
}

.git-diff-scroll :deep(.diff-hunk-wrap-btn.is-wrapped:hover) {
  opacity: 1;
}

.git-diff-scroll :deep(.diff-hunk-linum-btn.is-on) {
  opacity: 0.8;
  color: var(--accent-color, #4a90d9);
}

.git-diff-scroll :deep(.diff-hunk-linum-btn.is-on:hover) {
  opacity: 1;
}

.git-diff-scroll :deep(.diff-hunk-body) {
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}

.git-diff-scroll :deep(.diff-table) {
  width: max-content;
  min-width: 100%;
  border-collapse: collapse;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.5;
}

.git-diff-scroll :deep(.diff-linum) {
  width: 1%;
  min-width: 30px;
  padding: 0 4px;
  text-align: right;
  color: var(--text-muted, #999);
  font-size: 11px;
  user-select: none;
  white-space: nowrap;
  background: var(--bg-tertiary, #f8f8f8);
  border-right: 1px solid var(--border-color, #e5e5e5);
}

.git-diff-scroll :deep(.diff-prefix) {
  width: 1%;
  padding: 0 2px;
  text-align: center;
  font-weight: 700;
  user-select: none;
  white-space: nowrap;
}

.git-diff-scroll :deep(.diff-content) {
  padding: 0 6px;
  white-space: pre;
  min-width: 0;
}

/* ─── Word-wrap toggle: wrap mode ─── */
.git-diff-scroll :deep(.diff-hunk.diff-hunk-wrap .diff-content) {
  white-space: pre-wrap;
  word-break: break-all;
  overflow-wrap: break-word;
}

/* ─── Line number toggle: hide ─── */
.git-diff-scroll :deep(.diff-hunk.diff-hunk-no-linum .diff-linum) {
  display: none;
}

/* Line type colors */
.git-diff-scroll :deep(.diff-line-del) {
  background: rgba(239, 68, 68, 0.08);
}
.git-diff-scroll :deep(.diff-line-del .diff-prefix) {
  color: #dc2626;
}
.git-diff-scroll :deep(.diff-line-del .diff-linum) {
  color: #dc2626;
  opacity: 0.6;
}

.git-diff-scroll :deep(.diff-line-add) {
  background: rgba(34, 197, 94, 0.08);
}
.git-diff-scroll :deep(.diff-line-add .diff-prefix) {
  color: #16a34a;
}
.git-diff-scroll :deep(.diff-line-add .diff-linum) {
  color: #16a34a;
  opacity: 0.6;
}

.git-diff-scroll :deep(.diff-line-ctx .diff-content) {
  color: var(--text-primary, #212529);
}

/* Fallback raw diff */
.git-diff-scroll :deep(.diff-raw) {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--text-primary, #212529);
  margin: 0;
}
</style>
