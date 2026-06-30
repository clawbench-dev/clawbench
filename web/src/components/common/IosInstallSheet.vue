<template>
  <BottomSheet :open="open" :title="t('pwa.iosInstallTitle')" compact @close="close">
    <div class="ios-install-steps">
      <div class="ios-step">
        <span class="ios-step-num">1</span>
        <span>{{ t('pwa.iosStep1Part1') }}</span>
        <Share :size="16" class="ios-share-icon" />
        <span>{{ t('pwa.iosStep1Part2') }}</span>
      </div>
      <div class="ios-step">
        <span class="ios-step-num">2</span>
        <span>{{ t('pwa.iosStep2') }}</span>
      </div>
      <div class="ios-step">
        <span class="ios-step-num">3</span>
        <span>{{ t('pwa.iosStep3') }}</span>
      </div>
    </div>
    <template #footer>
      <button class="ios-got-it-btn" @click="close">{{ t('pwa.gotIt') }}</button>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Share } from 'lucide-vue-next'
import BottomSheet from './BottomSheet.vue'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

function close() {
  emit('close')
}
</script>

<style scoped>
.ios-install-steps {
  padding: 12px 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.ios-step {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-primary);
  line-height: 1.4;
}

.ios-step-num {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--accent-color);
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  flex-shrink: 0;
}

.ios-share-icon {
  color: var(--accent-color);
  flex-shrink: 0;
}

.ios-got-it-btn {
  width: 100%;
  padding: 8px 16px;
  border: none;
  border-radius: 8px;
  background: var(--accent-color);
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
}

.ios-got-it-btn:hover {
  opacity: 0.9;
}
</style>
