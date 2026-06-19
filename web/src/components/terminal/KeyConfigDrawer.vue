<template>
  <BottomSheet :open="open" :title="t('terminal.keyConfigTitle')" @close="handleClose">
    <template #header>
      <Settings :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('terminal.keyConfigTitle') }}</span>
    </template>

    <!-- Tab bar -->
    <div class="kcd-tabs">
      <button class="kcd-tab" :class="{ active: activeTab === 'key' }" @click="activeTab = 'key'">
        {{ t('terminal.keyConfigTabKeys') }}
      </button>
      <button class="kcd-tab" :class="{ active: activeTab === 'symbol' }" @click="activeTab = 'symbol'">
        {{ t('terminal.keyConfigTabSymbols') }}
      </button>
    </div>

    <!-- Tab content -->
    <div class="kcd-body">
      <KeyConfigTab
        v-show="activeTab === 'key'"
        ref="keyTabRef"
        type="key"
        :selected-ids="selectedKeyIds"
      />
      <KeyConfigTab
        v-show="activeTab === 'symbol'"
        ref="symbolTabRef"
        type="symbol"
        :selected-ids="selectedSymbolIds"
      />
    </div>

    <!-- Footer with save button -->
    <template #footer>
      <div class="kcd-footer">
        <button class="kcd-btn kcd-btn-cancel" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button class="kcd-btn kcd-btn-save" :disabled="saving" @click="handleSave">
          {{ saving ? t('common.loading') : t('common.save') }}
        </button>
      </div>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Settings } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import KeyConfigTab from './KeyConfigTab.vue'
import { useKeyConfig } from '@/composables/useKeyConfig'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const { t } = useI18n()
const { selectedKeyIds, selectedSymbolIds, fetchConfig, saveConfig } = useKeyConfig()

const activeTab = ref<'key' | 'symbol'>('key')
const saving = ref(false)
const keyTabRef = ref<InstanceType<typeof KeyConfigTab> | null>(null)
const symbolTabRef = ref<InstanceType<typeof KeyConfigTab> | null>(null)

watch(() => props.open, (val) => {
  if (val) {
    activeTab.value = 'key'
    fetchConfig()
  }
})

async function handleSave() {
  saving.value = true
  try {
    const keyIds = keyTabRef.value?.getSelectedIds() ?? []
    const symbolIds = symbolTabRef.value?.getSelectedIds() ?? []
    await Promise.all([
      saveConfig('key', keyIds),
      saveConfig('symbol', symbolIds),
    ])
    // Refresh config state after save
    await fetchConfig(true)
    emit('saved')
  } finally {
    saving.value = false
  }
}

function handleClose() {
  emit('close')
}
</script>

<style scoped>
.kcd-tabs {
  display: flex;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

.kcd-tab {
  flex: 1;
  padding: 10px 0;
  border: none;
  background: none;
  font-size: 14px;
  font-weight: 500;
  color: var(--text-muted, #999);
  cursor: pointer;
  position: relative;
  transition: color 0.2s;
  font-family: inherit;
  -webkit-tap-highlight-color: transparent;
}

.kcd-tab.active {
  color: var(--text-primary, #1a1a1a);
}

.kcd-tab.active::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 20%;
  right: 20%;
  height: 2px;
  background: var(--accent, #4f8ef7);
  border-radius: 1px;
}

.kcd-body {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.kcd-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  width: 100%;
}

.kcd-btn {
  padding: 8px 20px;
  border: none;
  border-radius: var(--radius-sm, 6px);
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  -webkit-tap-highlight-color: transparent;
  transition: opacity 0.15s;
}

.kcd-btn:active {
  opacity: 0.7;
}

.kcd-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.kcd-btn-cancel {
  background: var(--bg-tertiary, #eee);
  color: var(--text-primary, #1a1a1a);
}

.kcd-btn-save {
  background: var(--accent, #4f8ef7);
  color: #fff;
}
</style>
