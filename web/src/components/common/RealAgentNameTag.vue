<template>
  <!-- Real-agent disclosure chip: shown when a member's DISPLAY name was
       auto-derived (a pool name / "<base> <n>") so the reader knows which
       underlying agent it belongs to. Hidden when the display name IS the
       agent name (no redundant "Claude · Claude"). This is the SINGLE source
       of the disclosure rule — the message speaker header, the group settings
       roster and the @ completion list all render THIS component, so the
       visibility rule and the chip styling can never drift apart. -->
  <span
    v-if="agentName && agentName !== displayName"
    class="real-agent-name-tag"
    :title="t('group.realAgent')"
  >{{ agentName }}</span>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  /** The underlying REAL agent name. */
  agentName?: string
  /** The member's display name (the nickname shown to the user). */
  displayName?: string
}>()

const { t } = useI18n()
</script>

<style scoped>
/* Muted, faint pill. Mix --text-primary into --bg-secondary (design-guide
   redline 4): a fixed step like --bg-tertiary is invisible on ~15 of 36 themes. */
.real-agent-name-tag {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  padding: 0 var(--space-1);
  border-radius: var(--radius-xs);
  background: color-mix(in srgb, var(--text-primary) 8%, var(--bg-secondary));
  white-space: nowrap;
}
</style>
