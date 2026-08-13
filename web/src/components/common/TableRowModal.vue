<template>
  <ModalDialog
    :open="!!data"
    @close="$emit('close')"
  >
    <template #header>
      <Rows3 :size="16" class="modal-header-icon" />
      <span class="modal-title">{{ data ? `${t('chat.table.row')} ${data.currentIndex + 1} / ${data.rows.length}` : '' }}</span>
    </template>
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
import { Rows3 } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { copyText } from '@/utils/clipboard.ts'
import { gt } from '@/composables/useLocale'
import { openFilePath } from '@/composables/useFilePathAnnotation.ts'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { store } from '@/stores/app.ts'
import { extractImageName } from '@/utils/lightbox.ts'

defineProps({
  data: Object,  // { headers: string[], rows: string[][], currentIndex: number } | null
})

const emit = defineEmits(['close', 'prev', 'next'])

const { t } = useI18n()
const toast = inject('toast', null)
const hotSwitchProject = inject('hotSwitchProject', null)
const dialog = useDialog()
const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()
const openLightbox = inject('openLightbox', null)
const openMdImages = inject('openMdImages', null)

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

  // 0. Lightbox (ModalDialog @click.stop prevents bubbling to Lightbox's global listener).
  //    Both the expand icon AND the image body itself open the lightbox, so a click
  //    on the image is enough (the icon can be hidden on non-hover/touch devices).
  const expandIcon = target.closest('.lightbox-expand-icon')
  const isLightboxImgClick = !!target.closest('.lightbox-img')
  if (expandIcon || isLightboxImgClick) {
    if (!openLightbox) return
    event.preventDefault()
    const wrap = expandIcon ? expandIcon.closest('.lightbox-img-wrap') : target.closest('.lightbox-img-wrap')
    const lightboxImg = wrap ? wrap.querySelector('.lightbox-img') : null
    if (!lightboxImg) return
    // Full-size original (data-full-src) preferred; fall back to the inline thumb src.
    const fullSrc = (lightboxImg.dataset && lightboxImg.dataset.fullSrc) || lightboxImg.src
    // Collect sibling images in the modal for navigation
    const modalBody = lightboxImg.closest('.table-row-form')
    if (modalBody && openMdImages) {
      const allImgs = modalBody.querySelectorAll('img.lightbox-img')
      if (allImgs.length > 1) {
        const list = []
        let startIdx = 0
        allImgs.forEach((img) => {
          const src = (img.dataset && img.dataset.fullSrc) || img.src
          if (!src) return
          const name = img.alt || extractImageName(src)
          list.push({ src, name })
          if (img === lightboxImg) startIdx = list.length - 1
        })
        if (list.length > 1) {
          openMdImages(list, startIdx)
          return
        }
      }
    }
    openLightbox(fullSrc)
    return
  }

  // 1. Code block copy/wrap button
  if (handleCodeBlockClick(event)) return

  // 2. Table block copy/wrap button
  if (handleTableBlockClick(event)) return

  // 3. Localhost URL button
  if (handleLocalhostUrlClick(event)) return

  // 4. Worktree button
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
        // openFilePath decides the destination tab (file → view, dir → browse).
        await openFilePath(filePath)
      }
    }
    emit('close')
    return
  }

  // 5. Commit hash
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

  // 6. File-open button
  const fileBtn = target.closest('.chat-file-open-btn')
  if (fileBtn) {
    event.preventDefault()
    event.stopPropagation()
    const filePath = fileBtn.getAttribute('data-file-path')
    const lineStart = fileBtn.getAttribute('data-line-start')
    const lineEnd = fileBtn.getAttribute('data-line-end')
    if (filePath) {
      // openFilePath decides the destination tab (file → view, dir → browse).
      await openFilePath(
        filePath,
        lineStart ? parseInt(lineStart, 10) : undefined,
        lineEnd ? parseInt(lineEnd, 10) : undefined,
      )
      emit('close')
    }
    return
  }
}

</script>

<style>
/* Lightbox expand icon inside the row modal — mirrors .markdown-body rules but
   the modal content lives in .table-row-value (not under .markdown-body). */
.table-row-value .lightbox-img-wrap {
  position: relative;
  display: inline-block;
}

.table-row-value .lightbox-img-wrap .lightbox-expand-icon {
  display: none;
  position: absolute;
  top: 4px;
  right: 4px;
  width: 24px;
  height: 24px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  cursor: pointer;
  z-index: 2;
}

.table-row-value .lightbox-img-wrap .lightbox-expand-icon::after {
  content: '⤢';
  font-size: 14px;
  line-height: 24px;
  text-align: center;
}

@media (hover: hover) {
  .table-row-value .lightbox-img-wrap:hover .lightbox-expand-icon {
    display: flex;
    align-items: center;
    justify-content: center;
  }
}
</style>
