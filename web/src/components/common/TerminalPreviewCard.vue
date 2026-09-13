<template>
  <div class="tpc-shell" :class="{ 'tpc-shell--placeholder': !theme && !auto, 'tpc-shell--auto': auto }" :style="shellStyle">
    <div class="tpc-titlebar">
      <span class="tpc-dot tpc-dot--red"></span>
      <span class="tpc-dot tpc-dot--yellow"></span>
      <span class="tpc-dot tpc-dot--green"></span>
    </div>
    <div class="tpc-body">
      <div class="tpc-line">
        <span class="tpc-prompt" :style="promptStyle">$</span>
        <span>ls</span>
      </div>
      <div class="tpc-line">
        <span class="tpc-dir" :style="dirStyle">src</span>
        <span class="tpc-file" :style="fileStyle">main.go</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ITheme } from '@xterm/xterm'
import { darkTheme, lightTheme } from '@/utils/terminalThemes'

const props = defineProps<{
  /** Terminal theme colors. Undefined while lazy-loading → placeholder skeleton. */
  theme?: ITheme
  /** Auto mode: split light/dark gradient to represent "follow app". */
  auto?: boolean
}>()

const shellStyle = computed(() => {
  if (props.auto) {
    return {
      background: `linear-gradient(135deg, ${lightTheme.background} 0%, ${lightTheme.background} 48%, ${darkTheme.background} 52%, ${darkTheme.background} 100%)`,
    }
  }
  if (!props.theme) return {}
  return {
    background: props.theme.background ?? '',
    color: props.theme.foreground ?? '',
  }
})

const promptStyle = computed(() => {
  if (props.auto || !props.theme?.green) return {}
  return { color: props.theme.green }
})

const dirStyle = computed(() => {
  if (props.auto || !props.theme?.blue) return {}
  return { color: props.theme.blue }
})

const fileStyle = computed(() => {
  if (props.auto || !props.theme?.cyan) return {}
  return { color: props.theme.cyan }
})
</script>

<style scoped>
.tpc-shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  border-radius: var(--radius-sm);
  overflow: hidden;
  font-family: var(--font-mono);
  background: var(--bg-tertiary);
  border: 1px solid var(--border-color);
}

.tpc-shell--placeholder {
  background: color-mix(in srgb, var(--text-muted) 15%, var(--bg-tertiary));
}

.tpc-titlebar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding:0 var(--space-3);
  height: 16px;
  flex-shrink: 0;
  background: rgba(0, 0, 0, 0.15);
}

.tpc-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}

.tpc-dot--red { background: #ff5f56; }
.tpc-dot--yellow { background: #ffbd2e; }
.tpc-dot--green { background: #27c93f; }

.tpc-body {
  flex: 1;
  padding: var(--space-3) var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  justify-content: center;
}

.tpc-line {
  font-size: var(--font-size-2xs);
  line-height: var(--line-height-tight);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tpc-prompt {
  font-weight: var(--font-weight-bold);
  margin-right: var(--space-2);
  color: var(--text-secondary);
}

.tpc-dir,
.tpc-file {
  color: var(--text-secondary);
}

.tpc-file {
  margin-left: var(--space-3);
}
</style>
