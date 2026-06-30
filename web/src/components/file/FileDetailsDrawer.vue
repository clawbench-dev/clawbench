<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <FileText :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('file.details.title') }}</span>
    </template>

    <div class="details-body">
      <div class="details-row" v-for="item in detailItems" :key="item.label"
        :class="{ 'details-row-copyable': item.copyable }">
        <span class="details-label">{{ item.label }}</span>
        <div class="details-value-wrap" @click="item.copyable && copyValue(item.value, $event)">
          <span class="details-value" :class="{ 'details-value-copyable': item.copyable }">{{ item.value }}</span>
          <button v-if="item.copyable" class="details-copy-btn" @click.stop="copyValue(item.value, $event)" :title="t('common.copy')">
            <Copy :size="13" />
          </button>
        </div>
      </div>
    </div>

  </BottomSheet>
</template>

<script setup>
import { computed, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileText, Copy } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { store } from '@/stores/app.ts'
import { getFileType, formatFileSize } from '@/utils/fileType.ts'

const props = defineProps({
  file: Object,
  open: Boolean,
})
defineEmits(['close'])

const { t, locale } = useI18n()
const toast = inject('toast', null)

function copyValue(value, event) {
  const wrap = event.currentTarget.closest('.details-value-wrap')
  const btn = wrap.querySelector('.details-copy-btn')
  const txt = wrap.querySelector('.details-value')
  const doCopy = () => {
    if (btn) { btn.classList.add('copied'); setTimeout(() => btn.classList.remove('copied'), 800) }
    if (txt) { txt.classList.add('copied'); setTimeout(() => txt.classList.remove('copied'), 800) }
    if (toast) toast.show(t('common.copied'), { icon: '📋', type: 'success', duration: 1500 })
  }
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(value).then(doCopy).catch(() => fallbackCopy(value, doCopy))
  } else {
    fallbackCopy(value, doCopy)
  }
}

function fallbackCopy(value, cb) {
  const ta = document.createElement('textarea')
  ta.value = value
  ta.style.cssText = 'position:fixed;opacity:0;top:0;left:0'
  document.body.appendChild(ta)
  ta.select()
  document.execCommand('copy')
  document.body.removeChild(ta)
  cb()
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
  const name = props.file.name
  const fullPath = dir ? dir + '/' + name : name
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
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary, #1a1a1a);
}

.details-body {
  flex: 1;
  overflow-y: auto;
  padding: 8px 0;
}

.details-row {
  display: flex;
  align-items: center;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.details-label {
  width: 80px;
  flex-shrink: 0;
  font-size: 13px;
  color: var(--text-muted, #999);
}

.details-value-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.details-value {
  flex: 1;
  font-size: 13px;
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
  padding: 2px;
  border-radius: 3px;
  transition: color 0.15s, background 0.15s;
}
.details-copy-btn:hover {
  color: var(--accent-color, #4a90d9);
  background: var(--bg-tertiary, #f0f0f0);
}
.details-copy-btn.copied {
  color: #22c55e;
}

.details-row-copyable {
  user-select: none;
}
.details-row-copyable:hover {
  background: var(--bg-tertiary, #f5f5f5);
}
.details-value-copyable:hover {
  color: var(--accent-color, #4a90d9);
}
.details-value-copyable.copied {
  color: #22c55e;
}

</style>
