<template>
  <Teleport to="body">
    <div
      v-if="everOpened"
      v-show="open || leaving"
      class="bs-overlay"
      :class="{ 'bs-leaving': leaving, 'bs-instant': instant, 'bs-transparent-overlay': transparentOverlay, 'bs-overlay-fullscreen': fullscreen }"
      @click.self="handleClose"
    >
      <div class="bs-panel" :class="{ 'bs-leaving': leaving, 'bs-instant': instant, 'bs-compact': compact, 'bs-auto': auto, 'bs-handle-only': handleOnly }">
        <!-- Header -->
        <div v-if="!noHeader" class="bs-header" :class="{ 'bs-header-handle-only': handleOnly }" @click="handleClose">
          <div class="bs-handle" />
          <slot v-if="!handleOnly" name="header">
            <span class="bs-title">{{ title }}</span>
          </slot>
        </div>
        <!-- Body -->
        <div class="bs-body">
          <slot />
        </div>
        <!-- Footer slot -->
        <footer v-if="$slots.footer" class="bs-footer">
          <slot name="footer" />
        </footer>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, watch } from 'vue'

const props = defineProps({
  open: Boolean,
  title: {
    type: String,
    default: '',
  },
  instant: Boolean,  // 立即关闭，无动画
  compact: Boolean,  // 紧凑模式，高度自适应内容，最大50%，无圆角
  auto: Boolean,     // 自适应模式，高度按内容需要，最大全屏
  noHeader: Boolean, // 隐藏Header（含手柄）
  handleOnly: Boolean, // 仅显示拖拽手柄，无标题栏
  transparentOverlay: Boolean, // 透明遮罩（可点击关闭但可见底层内容）
  fullscreen: Boolean, // 全屏模式，覆盖 app header，用于无 header 的页面（如终端）
})

const emit = defineEmits(['close'])

const leaving = ref(false)
const everOpened = ref(false)
let leaveTimer = null

watch(() => props.open, (val) => {
  clearTimeout(leaveTimer)
  if (val) {
    everOpened.value = true
    leaving.value = false
  } else if (leaving.value) {
    // Close triggered externally while animating — cancel animation, hide now
    leaving.value = false
  }
}, { immediate: true })

function handleClose() {
  if (leaving.value) return
  if (props.instant) {
    emit('close')
    return
  }
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

<style>
/* ── BottomSheet base styles ── */

.bs-overlay {
  position: fixed;
  top: calc(var(--header-height, 44px) + var(--header-safe-area-top, 0px));
  left: 0;
  right: 0;
  bottom: var(--dock-height, 0);
  background: rgba(0, 0, 0, 0.5);
  z-index: 1000;
  display: flex;
  align-items: flex-end;
  overflow: hidden;
  animation: bs-fadeIn 0.2s ease;
}

.bs-overlay.bs-leaving {
  animation: bs-fadeOut 0.25s ease forwards;
}

.bs-panel {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  top: 0;
  background: var(--bg-secondary, #fff);
  border-top: 1px solid var(--border-color, #e0e0e0);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  animation: bs-slideUp 0.25s ease;
}

/* Compact mode - auto height, no border-radius */
.bs-panel.bs-compact {
  top: auto;
  height: auto;
  max-height: 50%;
  border-radius: 0;
  border-top: 1px solid var(--border-color, #e0e0e0);
  box-shadow: 0 -4px 20px rgba(0, 0, 0, 0.15);
}

.bs-panel.bs-compact .bs-header {
  border-radius: 0;
}

.bs-panel.bs-leaving {
  animation: bs-slideDown 0.25s ease forwards;
}

@keyframes bs-slideUp {
  from { transform: translateY(100%); }
  to   { transform: translateY(0); }
}

@keyframes bs-slideDown {
  from { transform: translateY(0); }
  to   { transform: translateY(100%); }
}

@keyframes bs-fadeIn {
  from { opacity: 0; }
  to   { opacity: 1; }
}

@keyframes bs-fadeOut {
  from { opacity: 1; }
  to   { opacity: 0; }
}

/* Instant close (no animation) */
.bs-overlay.bs-instant {
  animation: none;
}

.bs-panel.bs-instant {
  animation: none;
}

.bs-overlay.bs-instant.bs-leaving {
  display: none;
}

.bs-panel.bs-instant.bs-leaving {
  display: none;
}

/* ── Unified Drawer Header ── */
.bs-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 16px;
  height: 36px;
  border-bottom: none;
  box-shadow: 0 1px 0 var(--border-color, #e5e5e5);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
  cursor: pointer;
  position: relative;
}

/* Android-style drag handle */
.bs-handle {
  position: absolute;
  top: 4px;
  left: 50%;
  transform: translateX(-50%);
  width: 32px;
  height: 4px;
  border-radius: 2px;
  background: var(--text-muted, #bbb);
  opacity: 0.5;
}

/* Handle-only header — compact, no box-shadow, centered handle */
.bs-header-handle-only {
  justify-content: center;
  height: 12px;
  padding: 0;
  box-shadow: none;
}

.bs-header-handle-only .bs-handle {
  top: 4px;
}

.bs-header-icon {
  flex-shrink: 0;
  color: var(--text-primary, #1a1a1a);
  display: flex;
  align-items: center;
}

.bs-header-title {
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary, #1a1a1a);
  flex-shrink: 0;
  white-space: nowrap;
}

.bs-header-description {
  flex: 1;
  min-width: 0;
  font-size: 12px;
  color: var(--text-muted, #999);
  white-space: nowrap;
  overflow: hidden;
  display: flex;
  align-items: center;
}

/* ── Body ── */
.bs-body {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

/* Compact mode body - scrollable when options overflow */
.bs-panel.bs-compact .bs-body {
  overflow-y: auto;
}

/* Auto mode - auto height based on content, max full screen, no border-radius */
.bs-panel.bs-auto {
  top: auto;
  height: auto;
  max-height: 100%;
  border-radius: 0;
  border-top: 1px solid var(--border-color, #e0e0e0);
  box-shadow: 0 -4px 20px rgba(0, 0, 0, 0.15);
}

.bs-panel.bs-auto .bs-header {
  border-radius: 0;
}

.bs-panel.bs-auto .bs-body {
  overflow-y: auto;
}

/* ── Footer ── */
.bs-panel > .bs-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: 8px 12px;
  border-top: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
  gap: 8px;
}

/* Compact mode footer — add bottom padding for dock bar clearance */
.bs-panel.bs-compact > .bs-footer {
    padding-bottom: calc(8px + env(safe-area-inset-bottom, 0px));
}

/* Transparent overlay — clickable but see-through */
.bs-overlay.bs-transparent-overlay {
  background: transparent;
}

/* ── Fullscreen mode ── */
/* Overlay covers entire viewport including app header; z-index above app header (1100) */
.bs-overlay.bs-overlay-fullscreen {
  top: 0;
  bottom: 0;
  z-index: 1200;
}
</style>
