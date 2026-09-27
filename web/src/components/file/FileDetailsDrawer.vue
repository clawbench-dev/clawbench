<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <FileIcon :path="file?.name || ''" :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('file.details.title') }}</span>
    </template>

    <div class="details-body">
      <div class="details-row" v-for="item in detailItems" :key="item.label"
        :class="{ 'details-row-copyable': item.copyable }">
        <span class="details-label">{{ item.label }}</span>
        <div class="details-value-wrap" @click="item.copyable && copyValue(item.value, $event)">
          <span class="details-value" :class="{ 'details-value-copyable': item.copyable }">{{ item.value }}</span>
          <CopyButton
            v-if="item.copyable"
            :text="item.value"
            :size="13"
            class="details-copy-btn"
            @click.stop
          />
        </div>
      </div>
    </div>

  </BottomSheet>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import CopyButton from '@/components/common/CopyButton.vue'
import { copyText } from '@/utils/clipboard'
import FileIcon from '@/components/common/FileIcon.vue'
import { store } from '@/stores/app.ts'
import { getFileType, formatFileSize } from '@/utils/fileType.ts'

const props = defineProps({
  file: Object,
  open: Boolean,
})
defineEmits(['close'])

const { t, locale } = useI18n()

/**
 * Copy from a CLICKABLE TEXT value (the row / the value itself). The copy
 * button beside it owns its own state via CopyButton; this path only flashes
 * the text that was clicked.
 *
 * No toast — the flash is the feedback. `copyText` supplies the
 * non-secure-context fallback that the previous hand-rolled version had.
 */
function copyValue(value, event) {
  const el = event.currentTarget
  if (!el || !value) return
  copyText(value, () => {
    el.classList.add('copied')
    setTimeout(() => el.classList.remove('copied'), 800)
  })
}

const fileType = computed(() => props.file ? getFileType(props.file.name) : null)

const absPath = computed(() => {
    if (!props.file) return ''
    const root = store.state.projectRoot
    return root ? root + '/' + props.file.path : props.file.path
})

const modified = computed(() => {
  if (!props.file) return ''
  const dir = store.state.currentDir
  const entry = store.state.dirEntries?.find(e => {
    const ep = dir ? dir + '/' + e.name : e.name
    return ep === props.file.path
  })
  if (entry?.modified) {
    const d = new Date(entry.modified)
    return d.toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US')
  }
  return ''
})

const lineCount = computed(() => {
  if (!props.file?.content) return ''
  return props.file.content.split('\n').length
})

const charCount = computed(() => {
  if (!props.file?.content) return ''
  return props.file.content.length.toLocaleString()
})

const detailItems = computed(() => {
  if (!props.file) return []
  const items = [
    { label: t('file.details.fileName'), value: props.file.name, copyable: true },
    { label: t('file.details.path'), value: absPath.value, copyable: true },
    { label: t('file.details.type'), value: fileType.value?.label || t('file.details.unknownType') },
  ]
  if (props.file.isSymlink) {
    items.push({ label: t('file.details.linkTarget'), value: props.file.linkTarget || t('file.details.brokenLink'), copyable: true })
  }
  if (props.file.size != null) {
    items.push({ label: t('file.details.size'), value: formatFileSize(props.file.size) })
  }
  if (modified.value) {
    items.push({ label: t('file.details.modifiedTime'), value: modified.value })
  }
  if (props.file.content) {
    items.push({ label: t('file.details.lineCount'), value: lineCount.value })
    items.push({ label: t('file.details.charCount'), value: charCount.value })
  }
  items.push({ label: t('file.details.encoding'), value: 'UTF-8' })
  return items
})
</script>

<style scoped>
.details-title {
  font-weight: var(--font-weight-semibold);
  font-size: var(--font-size-lg);
  color: var(--text-primary, #1a1a1a);
}

.details-body {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-4) 0;
}

.details-row {
  display: flex;
  align-items: center;
  padding: var(--space-5) var(--space-7);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.details-label {
  width: 80px;
  flex-shrink: 0;
  font-size: var(--font-size-md);
  color: var(--text-muted, #999);
}

.details-value-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.details-value {
  flex: 1;
  font-size: var(--font-size-md);
  color: var(--text-primary, #1a1a1a);
  word-break: break-all;
}

.details-value-copyable {
  cursor: pointer;
}

.details-copy-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted, #999);
  padding: var(--space-1);
  border-radius: var(--radius-xs);
  transition: color var(--duration-base), background var(--duration-base);
}
@media (hover: hover) {
  .details-copy-btn:hover {
    color: var(--accent-color, #4a90d9);
    background: var(--bg-tertiary, #f0f0f0);
  }
}
.details-row-copyable {
  user-select: none;
}
@media (hover: hover) {
  .details-row-copyable:hover {
    background: var(--bg-tertiary, #f5f5f5);
  }
  .details-value-copyable:hover {
    color: var(--accent-color, #4a90d9);
  }
}
.details-value-copyable.copied {
  color: #22c55e;
}

</style>
