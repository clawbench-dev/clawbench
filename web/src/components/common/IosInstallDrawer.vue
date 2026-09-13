<template>
  <BottomSheet :open="open" auto @close="close">
    <template #header>
      <Smartphone :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('pwa.iosInstallTitle') }}</span>
    </template>
    <div class="ios-install-steps">
      <div class="ios-step">
        <span class="ios-step-num">1</span>
        <span>{{ t('pwa.iosStep1Part1') }}</span>
        <Share2 :size="16" class="ios-share-icon" />
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
      <button class="fbtn fbtn-primary ios-got-it-btn" @click="close">{{ t('pwa.gotIt') }}</button>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Share2, Smartphone } from 'lucide-vue-next'
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
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-6);
}

.ios-step {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  font-size: var(--font-size-md);
  color: var(--text-primary);
  line-height: var(--line-height-snug);
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
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-bold);
  flex-shrink: 0;
}

.ios-share-icon {
  color: var(--accent-color);
  flex-shrink: 0;
}

/* Layout only — visuals come from the shared .fbtn pills. */
.ios-got-it-btn {
  width: 100%;
}
</style>
