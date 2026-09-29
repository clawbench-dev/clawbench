<template>
  <div class="script-result-card" :class="{ 'is-failed': !passed }">
    <div class="src-header">
      <Terminal :size="13" class="src-icon" />
      <span class="src-title">{{ t('task.exec.scriptTitle') }}</span>
      <span class="src-exit" :class="passed ? 'ok' : 'bad'">
        {{ t('task.exec.scriptExitCode') }} {{ script.exitCode }}
      </span>
      <span v-if="durationLabel" class="src-duration">{{ durationLabel }}</span>
    </div>

    <div v-if="script.stdout" class="src-stream">
      <div class="src-stream-label">{{ t('task.exec.scriptStdout') }}</div>
      <pre class="src-stream-body">{{ script.stdout }}</pre>
    </div>

    <div v-if="script.stderr" class="src-stream">
      <div class="src-stream-label">{{ t('task.exec.scriptStderr') }}</div>
      <pre class="src-stream-body stderr">{{ script.stderr }}</pre>
    </div>

    <div v-if="!script.stdout && !script.stderr" class="src-empty">
      {{ t('task.exec.scriptNoOutput') }}
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { Terminal } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { formatDuration } from '@/utils/format.ts'

/**
 * The gating script's result on an execution, styled after the chat's bash
 * tool card: a green terminal accent, an exit-code chip, and the two output
 * streams in monospace blocks.
 *
 * Rendered only when the execution carries a script result, so a task without
 * a gate shows no card.
 */
const props = defineProps({
  script: { type: Object, required: true },
})

const { t } = useI18n()

const passed = computed(() => props.script?.exitCode === 0)

const durationLabel = computed(() => {
  const ms = props.script?.durationMs
  if (!ms || ms <= 0) return ''
  return formatDuration(ms)
})
</script>

<style scoped>
/* Terminal-green accent, matching the chat bash tool card
   (`.chat-tool-call[data-category="bash"]`). */
/* The scroll container (.exec-detail-content) has vertical padding only, so the
   horizontal inset has to come from the card — otherwise it runs edge to edge.
   --space-6 matches the message bubbles' own horizontal padding. */
.script-result-card {
  --src-accent: #10b981;
  margin: 0 var(--space-6) var(--space-5);
  border: 1px solid color-mix(in srgb, var(--src-accent) 25%, var(--border-color));
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--src-accent) 5%, var(--bg-primary));
  overflow: hidden;
}

/* A failed gate is the reason the AI never ran, so it reads as a warning
   rather than a neutral result. */
.script-result-card.is-failed {
  --src-accent: #f59e0b;
}

.src-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  background: color-mix(in srgb, var(--src-accent) 8%, var(--bg-secondary));
  border-bottom: 1px solid color-mix(in srgb, var(--src-accent) 15%, var(--border-color));
  font-size: var(--font-size-xs);
}

.src-icon {
  color: var(--src-accent);
  flex-shrink: 0;
}

.src-title {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.src-exit {
  font-family: var(--font-mono);
  padding: 1px var(--space-3);
  border-radius: var(--radius-xs);
  font-weight: var(--font-weight-semibold);
  font-variant-numeric: tabular-nums;
}

.src-exit.ok {
  background: rgba(16, 185, 129, 0.14);
  color: #059669;
}

.src-exit.bad {
  background: rgba(245, 158, 11, 0.16);
  color: #b45309;
}

.src-duration {
  margin-left: auto;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

.src-stream {
  padding: var(--space-3) var(--space-4);
}

.src-stream + .src-stream {
  border-top: 1px dashed var(--border-color);
}

.src-stream-label {
  font-size: var(--font-size-2xs);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-muted);
  margin-bottom: var(--space-2);
}

.src-stream-body {
  margin: 0;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
  background: var(--bg-tertiary);
  border-radius: var(--radius-xs);
  padding: var(--space-3) var(--space-4);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 260px;
  overflow-y: auto;
  color: var(--text-primary);
}

.src-stream-body.stderr {
  color: color-mix(in srgb, #ef4444 70%, var(--text-primary));
}

.src-empty {
  padding: var(--space-4);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  font-style: italic;
}
</style>
