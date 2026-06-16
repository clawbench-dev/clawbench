<template>
  <div v-if="loading" class="git-diff-loading">
    <div class="spinner" style="width:24px;height:24px;border-width:2px;margin:0 auto;" />
  </div>
  <div v-else-if="empty" class="git-diff-empty">{{ t('git.diffView.noChanges') }}</div>
  <div v-else :class="['git-diff-scroll', { 'no-wrap': noWrap }]" v-html="html" />
</template>

<script setup>
import { useI18n } from 'vue-i18n'
const { t } = useI18n()

defineProps({
  loading: Boolean,
  empty: Boolean,
  html: { type: String, default: '' },
  noWrap: Boolean,
})
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
  gap: 0;
}

.git-diff-scroll :deep(.diff-hunk) {
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 0;
  margin-bottom: 0;
}

.git-diff-scroll :deep(.diff-hunk-header) {
  font-size: 11px;
  font-family: 'SF Mono', 'Fira Code', Menlo, monospace;
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f0f0f0);
  padding: 2px 8px;
  user-select: none;
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
