<template>
  <div v-if="commit || isWorkingTree" class="diff-meta-panel">
    <template v-if="filePath">
      <div class="diff-meta-row">
        <span class="diff-meta-label">{{ t('git.commitMeta.file') }}</span>
        <span
          class="diff-meta-value diff-meta-file-name diff-meta-annotation"
          :title="t('git.commitMeta.openFile')"
          @click="emit('open-file', filePath)"
        >{{ fileName }}</span>
        <button
          class="diff-meta-open-btn"
          type="button"
          :title="t('git.commitMeta.openFile')"
          @click.stop="emit('open-file', filePath)"
          v-html="FILE_OPEN_ICON_SVG"
        ></button>
      </div>
      <div class="diff-meta-row">
        <span class="diff-meta-label">{{ t('git.commitMeta.path') }}</span>
        <span
          class="diff-meta-value diff-meta-file-path diff-meta-annotation"
          :title="t('git.commitMeta.revealInManager')"
          @click="emit('reveal-file', filePath)"
        >{{ filePath }}</span>
        <button
          class="diff-meta-open-btn"
          type="button"
          :title="t('git.commitMeta.revealInManager')"
          @click.stop="emit('reveal-file', filePath)"
          v-html="FILE_OPEN_ICON_SVG"
        ></button>
      </div>
    </template>
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
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { copyText } from '@/utils/clipboard.ts'
import { baseName } from '@/utils/path.ts'
import { FILE_OPEN_ICON_SVG } from '@/composables/useFilePathAnnotation.ts'
const { t, locale } = useI18n()

const props = defineProps({
  commit: Object,
  isWorkingTree: Boolean,
  filePath: String,
})

// Both row actions are delegated to the host. The panel renders inside the
// wide-screen history tab AND inside the mobile file-history bottom sheet, and
// only the host knows which surface the jump originates from (history tab vs.
// the file viewer's own stack) and how to record it as a return target.
// Navigating from inside the panel would strand the destination with no origin,
// so Back would walk up the directory tree instead of returning here. This
// mirrors the breadcrumb's open-file emit, which both hosts already handle.
const emit = defineEmits(['open-file', 'reveal-file'])

const shaCopied = ref(false)

// Show file name only when the path is available — the meta row renders
// whatever per-file info exists and hides the rest.
const fileName = computed(() => {
  return props.filePath ? baseName(props.filePath) : ''
})

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
  padding: var(--space-6) 14px;
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
  gap: var(--space-5);
  font-size: var(--font-size-md);
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
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  color: var(--accent-color, #4a90d9);
  cursor: pointer;
  border-radius: var(--radius-xs);
  padding:1px var(--space-2);
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .diff-meta-sha:hover {
    background: var(--bg-tertiary, #f0f0f0);
  }
}

.sha-copied-text {
  color: var(--color-green, #16a34a);
  font-size: var(--font-size-xs);
  font-weight: 400;
}

.diff-meta-row-msg .diff-meta-value {
  font-weight: var(--font-weight-medium);
}

.diff-meta-file-name {
  font-weight: var(--font-weight-semibold);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Clickable file annotations ──
   The file name opens the file, the path reveals it in the file manager.
   The label is the primary affordance; the trailing button repeats the same
   action for users who expect an explicit control (matching the chat-side
   .chat-file-open-btn convention). */
.diff-meta-annotation {
  cursor: pointer;
  border-radius: var(--radius-xs);
  padding: 1px var(--space-2);
  margin-left: calc(var(--space-2) * -1);
  transition: background var(--duration-base), color var(--duration-base);
  min-width: 0;
}

@media (hover: hover) {
  .diff-meta-annotation:hover {
    background: color-mix(in srgb, var(--text-muted, #999) 15%, transparent);
    color: var(--text-primary, #333);
  }
}

.diff-meta-open-btn {
  background: none;
  border: none;
  padding: var(--space-1);
  cursor: pointer;
  color: var(--text-muted, #999);
  border-radius: var(--radius-xs);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: color var(--duration-base), background var(--duration-base);
  font-size: var(--font-size-sm);
  line-height: 1;
  vertical-align: baseline;
  flex-shrink: 0;
  align-self: center;
}

.diff-meta-open-btn svg {
  display: block;
}

@media (hover: hover) {
  .diff-meta-open-btn:hover {
    color: var(--accent-color, #4a90d9);
    background: var(--bg-tertiary, #f0f0f0);
  }
}

.diff-meta-file-path {
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #555);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

</style>
