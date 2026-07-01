<template>
  <ModalDialog
    :open="!!data"
    :title="data ? `${t('chat.table.row')} ${data.currentIndex + 1} / ${data.rows.length}` : ''"
    @close="$emit('close')"
  >
    <div v-if="data" class="table-row-form" aria-live="polite">
      <div v-for="(header, hi) in data.headers" :key="hi" class="table-row-field">
        <div class="table-row-label">{{ header }}</div>
        <div class="table-row-value" v-html="data.rows[data.currentIndex]?.[hi] || ''" @dblclick="handleValueDblClick" @click="handleValueClick"></div>
      </div>
    </div>
    <template #footer>
      <button class="table-row-nav-btn" :disabled="!data || data.currentIndex <= 0" @click="$emit('prev')">{{ t('chat.table.prevRow') }}</button>
      <button class="table-row-nav-btn" :disabled="!data || data.currentIndex >= data.rows.length - 1" @click="$emit('next')">{{ t('chat.table.nextRow') }}</button>
    </template>
  </ModalDialog>
</template>

<script setup>
import { inject } from 'vue'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { copyText } from '@/utils/clipboard.ts'
import { gt } from '@/composables/useLocale'
import { openFilePath } from '@/composables/useFilePathAnnotation.ts'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { store } from '@/stores/app.ts'

defineProps({
  data: Object,  // { headers: string[], rows: string[][], currentIndex: number } | null
})

const emit = defineEmits(['close', 'prev', 'next'])

const { t } = useI18n()
const toast = inject('toast', null)
const switchTab = inject('switchTab', null)
const hotSwitchProject = inject('hotSwitchProject', null)
const dialog = useDialog()
const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()

function handleValueDblClick(event) {
  const el = event.target
  const valueEl = el.closest?.('.table-row-value')
  if (!valueEl) return
  const text = valueEl.textContent?.trim() || ''
  if (!text) return
  copyText(text, () => {
    valueEl.classList.add('copy-flash')
    valueEl.addEventListener('animationend', () => {
      valueEl.classList.remove('copy-flash')
    }, { once: true })
    if (toast) {
      toast.show(gt('common.copied'), { icon: '📋', duration: 1500 })
    }
  })
}

async function handleValueClick(event) {
  const target = event.target

  // 0. Code block copy/wrap button
  if (handleCodeBlockClick(event)) return

  // 0.5. Table block copy/wrap button
  if (handleTableBlockClick(event)) return

  // 1. Localhost URL button
  if (handleLocalhostUrlClick(event)) return

  // 2. Worktree button
  const wtBtn = target.closest('.chat-worktree-btn')
  if (wtBtn) {
    event.preventDefault()
    event.stopPropagation()
    const wtPath = wtBtn.getAttribute('data-worktree-path')
    const filePath = wtBtn.getAttribute('data-file-path')
    if (wtPath) {
      const switchLabel = t('chat.attach.switchWorktree')
      const openLabel = t('chat.attach.openDirectory')
      const result = await dialog.confirm(
        filePath ? `${switchLabel}\n${openLabel}` : switchLabel,
        {
          title: t('chat.attach.openWorktree'),
          confirmText: switchLabel,
          cancelText: filePath ? openLabel : t('common.cancel'),
        }
      )
      if (result) {
        if (hotSwitchProject) {
          await hotSwitchProject(wtPath)
        } else {
          await store.setProject(wtPath)
        }
      } else if (filePath) {
        const ok = await openFilePath(filePath)
        if (ok) switchTab?.('browse')
      }
    }
    emit('close')
    return
  }

  // 3. Commit hash
  const commitEl = target.closest('.chat-commit-hash, .chat-commit-open-btn')
  if (commitEl) {
    event.preventDefault()
    event.stopPropagation()
    const sha = commitEl.getAttribute('data-commit-sha')
    if (sha) {
      window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha } }))
    }
    emit('close')
    return
  }

  // 4. File-open button
  const fileBtn = target.closest('.chat-file-open-btn')
  if (fileBtn) {
    event.preventDefault()
    event.stopPropagation()
    const filePath = fileBtn.getAttribute('data-file-path')
    const lineStart = fileBtn.getAttribute('data-line-start')
    const lineEnd = fileBtn.getAttribute('data-line-end')
    if (filePath) {
      const ok = await openFilePath(
        filePath,
        lineStart ? parseInt(lineStart, 10) : undefined,
        lineEnd ? parseInt(lineEnd, 10) : undefined,
      )
      if (ok) {
        switchTab?.('browse')
        emit('close')
      }
    }
    return
  }
}
</script>
