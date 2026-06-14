<template>
  <PopupMenu :show="show" @update:show="onShowChange" :target-element="targetElement" :max-width="180" :menu-items-count="3" anchor="right">
    <button class="tab-menu-item" @click="handleCopyPath">
      {{ t('terminal.copyPath') }}
    </button>
    <button class="tab-menu-item danger" @click="handleClose">
      {{ t('terminal.close') }}
    </button>
    <button class="tab-menu-item danger" @click="handleCloseAll">
      {{ t('terminal.closeAllTabs') }}
    </button>
  </PopupMenu>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'
import PopupMenu from '@/components/common/PopupMenu.vue'

const props = defineProps<{
  show: boolean
  targetElement: HTMLElement | null
  cwd: string
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  close: []
  copyPath: []
  closeAll: []
}>()

const { t } = useI18n()
const toast = useToast()

function handleClose() {
  emit('update:show', false)
  emit('close')
}

function handleCopyPath() {
  emit('update:show', false)
  navigator.clipboard.writeText(props.cwd).catch(() => {})
  toast.show(t('common.copied'), { icon: '📋', type: 'success', duration: 1500 })
  emit('copyPath')
}

function handleCloseAll() {
  emit('update:show', false)
  emit('closeAll')
}

function onShowChange(val: boolean) {
  emit('update:show', val)
}
</script>

<style>
.tab-menu-item {
  display: block;
  width: 100%;
  padding: 8px 14px;
  border: none;
  background: none;
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  transition: background 0.12s, color 0.12s;
  position: relative;
  overflow: hidden;
}

.tab-menu-item:hover {
  background: var(--accent-color, #0066cc);
  color: #fff;
}

.tab-menu-item.danger {
  color: var(--color-red, #dc3545);
}

.tab-menu-item.danger:hover {
  background: var(--color-red, #dc3545);
  color: #fff;
}

</style>
