<template>
  <ModalDialog
    :open="!!data"
    :title="data ? `${t('chat.table.row')} ${data.currentIndex + 1} / ${data.rows.length}` : ''"
    @close="$emit('close')"
  >
    <div v-if="data" class="table-row-form" aria-live="polite">
      <div v-for="(header, hi) in data.headers" :key="hi" class="table-row-field">
        <div class="table-row-label">{{ header }}</div>
        <div class="table-row-value" v-html="data.rows[data.currentIndex]?.[hi] || ''" @dblclick="handleValueDblClick"></div>
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

const props = defineProps({
  data: Object,  // { headers: string[], rows: string[][], currentIndex: number } | null
})

defineEmits(['close', 'prev', 'next'])

const { t } = useI18n()
const toast = inject('toast', null)

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
</script>
