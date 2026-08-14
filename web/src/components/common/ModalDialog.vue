<template>
  <Teleport to="body">
    <div
      v-if="everOpened"
      v-show="open || leaving"
      ref="overlayRef"
      class="modal-overlay"
      :class="{ 'modal-leaving': leaving }"
      :style="{ zIndex }"
      tabindex="-1"
      @click.self="handleClose"
      @keydown.escape="handleClose"
    >
      <div class="modal-dialog" :class="{ 'modal-leaving': leaving, 'modal-full-height': fullHeight }" :style="maxWidthStyle" @click.stop>
        <div class="modal-header" @click="handleClose">
          <slot name="header">
            <span class="modal-title">{{ title }}</span>
          </slot>
        </div>
        <div class="modal-body">
          <slot />
        </div>
        <div class="modal-footer" :class="{ 'modal-footer-default': !$slots.footer }">
          <slot name="footer" />
        </div>
        <slot name="after" />
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, watch, nextTick, computed } from 'vue'
import '@/assets/modal-card.css'

const props = defineProps({
  open: Boolean,
  title: { type: String, default: '' },
  zIndex: { type: Number, default: 2100 },
  fullHeight: { type: Boolean, default: false },
  maxWidth: { type: Number, default: 480 },
})

const emit = defineEmits(['close'])

const maxWidthStyle = computed(() => props.maxWidth ? { maxWidth: `${Math.round(props.maxWidth * 1.3)}px` } : {})

const leaving = ref(false)
const everOpened = ref(false)
const overlayRef = ref(null)
let leaveTimer = null

watch(() => props.open, (val) => {
  clearTimeout(leaveTimer)
  if (val) {
    everOpened.value = true
    leaving.value = false
    // Auto-focus overlay so Escape key works immediately
    nextTick(() => {
      overlayRef.value?.focus()
    })
  } else if (leaving.value) {
    // Close triggered externally while animating — cancel animation, hide now
    leaving.value = false
  }
}, { immediate: true })

function handleClose() {
  if (leaving.value) return
  leaving.value = true
  leaveTimer = setTimeout(() => {
    leaving.value = false
    leaveTimer = null
    emit('close')
  }, 250)
}

defineExpose({
  close: handleClose,
})
</script>
