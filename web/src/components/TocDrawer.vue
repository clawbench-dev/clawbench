<template>
  <BottomSheet :open="open" auto :title="t('toc.title')" @close="$emit('close')">
    <template #header>
      <List :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('toc.title') }}</span>
      <div v-if="file?.path" class="bs-header-description">
        <HeaderMarquee :text="file.path">{{ file.path }}</HeaderMarquee>
      </div>
    </template>

    <TocPanel
      :open="open"
      :file="file"
      :pdf-outline="pdfOutline"
      @jump="handleJump"
      @jump-page="handleJumpPage"
      @activated="emit('close')"
    />
  </BottomSheet>
</template>

<script setup>
import { List } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import HeaderMarquee from '@/components/common/HeaderMarquee.vue'
import TocPanel from '@/components/TocPanel.vue'

const { t } = useI18n()

defineProps({
    file: Object,
    pdfOutline: { type: Array, default: () => [] },
    open: Boolean,
})
const emit = defineEmits(['close', 'jump', 'jumpPage'])

// Bottom-sheet TOC: tapping an item jumps and dismisses the drawer.
function handleJump(line, anchorId) {
  emit('jump', line, anchorId)
  emit('close')
}
function handleJumpPage(pageNum) {
  emit('jumpPage', pageNum)
  emit('close')
}
</script>

<style scoped>
.toc-header-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
}
</style>
