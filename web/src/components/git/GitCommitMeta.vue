<template>
  <div v-if="commit || isWorkingTree" class="diff-meta-panel">
    <template v-if="isWorkingTree">
      <div class="diff-meta-row diff-meta-row-msg">
        <span class="diff-meta-label">{{ t('git.commitMeta.description') }}</span>
        <span class="diff-meta-value">{{ t('git.commitMeta.workingTreeChanges') }}</span>
      </div>
    </template>
    <template v-else-if="commit">
      <div class="diff-meta-row">
        <span class="diff-meta-label">SHA</span>
        <span class="diff-meta-value diff-meta-sha" :class="{ 'sha-copied': shaCopied }" @click="copySHA" :title="t('git.commitMeta.clickToCopy')">{{ commit.sha.substring(0, 8) }}<span v-if="shaCopied" class="sha-copied-text">{{ t('git.commitMeta.copied') }}</span></span>
      </div>
      <div class="diff-meta-row">
        <span class="diff-meta-label">{{ t('git.commitMeta.author') }}</span>
        <span class="diff-meta-value">{{ commit.author }}</span>
      </div>
      <div class="diff-meta-row">
        <span class="diff-meta-label">{{ t('git.commitMeta.time') }}</span>
        <span class="diff-meta-value">{{ formatDate(commit.date) }}</span>
      </div>
      <div class="diff-meta-row diff-meta-row-msg">
        <span class="diff-meta-label">{{ t('git.commitMeta.description') }}</span>
        <span class="diff-meta-value">{{ commit.msg }}</span>
      </div>
    </template>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { copyText } from '@/utils/clipboard.ts'
const { t, locale } = useI18n()

const props = defineProps({
  commit: Object,
  isWorkingTree: Boolean,
})

const shaCopied = ref(false)

function copySHA() {
  if (!props.commit?.sha) return
  copyText(props.commit.sha, () => {
    shaCopied.value = true
    setTimeout(() => { shaCopied.value = false }, 1500)
  })
}

function formatDate(dateStr) {
  if (!dateStr) return ''
  try {
    const d = new Date(dateStr)
    return d.toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  } catch {
    return dateStr
  }
}
</script>

<style scoped>
.diff-meta-panel {
  padding: 12px 14px;
  border-bottom: 1px solid var(--border-color, #dee2e6);
  background: var(--bg-secondary, #f8f9fa);
  display: flex;
  flex-direction: column;
  gap: 5px;
  flex-shrink: 0;
}

.diff-meta-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  font-size: 13px;
}

.diff-meta-label {
  color: var(--text-muted, #999);
  flex-shrink: 0;
  width: 36px;
  padding-top: 1px;
}

.diff-meta-value {
  color: var(--text-primary, #212529);
  word-break: break-all;
}

.diff-meta-sha {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 12px;
  color: var(--accent-color, #4a90d9);
  cursor: pointer;
  border-radius: 3px;
  padding: 1px 4px;
  transition: background 0.15s;
}

@media (hover: hover) {
  .diff-meta-sha:hover {
    background: var(--bg-tertiary, #f0f0f0);
  }
}

.sha-copied-text {
  color: var(--color-green, #16a34a);
  font-size: 11px;
  font-weight: 400;
}

.diff-meta-row-msg .diff-meta-value {
  font-weight: 500;
}
</style>
