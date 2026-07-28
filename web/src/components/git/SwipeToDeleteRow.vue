<template>
  <div v-if="deletable" class="swipe-to-delete">
    <!-- Archive button background (revealed on swipe) -->
    <div class="swipe-delete-bg" @click.stop="$emit('delete')">
      <Archive :size="16" />
      <span>{{ t('common.archive') }}</span>
    </div>
    <!-- Content layer (slides on swipe) -->
    <div
      class="swipe-delete-content"
      :style="{ transform: `translateX(${offset}px)` }"
      @touchstart="onTouchStart"
      @touchmove="onTouchMove"
      @touchend="onTouchEnd"
      @click="handleClick"
    >
      <slot />
    </div>
  </div>
  <!-- Non-deletable: just render content directly -->
  <slot v-else />
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Archive } from 'lucide-vue-next'
import { useSwipeDelete } from '@/composables/useSwipeDelete'

const { t } = useI18n()

defineProps({
  deletable: { type: Boolean, default: true },
})

defineEmits(['delete'])

const { offset, onTouchStart, onTouchMove, onTouchEnd, onContentClick } = useSwipeDelete()

function handleClick(e: MouseEvent) {
  if (onContentClick()) {
    e.preventDefault()
    e.stopPropagation()
  }
}
</script>

<style scoped>
.swipe-to-delete {
  position: relative;
  overflow: hidden;
}

.swipe-delete-bg {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  width: 70px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  background: var(--color-warning, #e6a23c);
  color: #fff;
  font-size: 10px;
  font-weight: 600;
  cursor: pointer;
  z-index: 0;
}

.swipe-delete-bg:active {
  opacity: 0.85;
}

.swipe-delete-content {
  position: relative;
  z-index: 1;
  transition: transform 0.2s ease;
  background: var(--bg-primary, #fff);
}
</style>
