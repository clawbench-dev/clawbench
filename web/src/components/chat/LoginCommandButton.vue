<template>
  <button class="warning-login-btn" type="button" :title="command" @click.stop="copy">
    {{ copied ? t('chat.contentBlocks.loginCmdCopied') : t('chat.contentBlocks.copyLoginCmd') }}
  </button>
</template>

<script setup lang="ts">
/**
 * The actionable control for an `auth_required` failure.
 *
 * Why not the shared "reset session" button: that action recycles the agent
 * connection and rebuilds the session, which is the right fix for a STUCK
 * session but can never succeed here — the agent is refusing because nobody is
 * signed in, and no number of retries signs the user in. Offering it made the
 * frontend contradict the backend (which deliberately excludes auth_required
 * from the retryable reasons).
 *
 * The only real fix is running the backend's login command once in a terminal,
 * so the button hands the user exactly that command instead of making them
 * retype it out of localized prose.
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { copyText } from '@/utils/clipboard'

const props = defineProps<{ command: string }>()
const { t } = useI18n()
const copied = ref(false)

function copy() {
  copyText(props.command, () => {
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  })
}
</script>

<style scoped>
/* Mirrors .warning-reset-btn (same slot, same weight) so the banner keeps one
   visual language: the action sits at the right edge of the card. */
.warning-login-btn {
  flex-shrink: 0;
  margin-left: auto;
  padding: var(--space-1) var(--space-5);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: #fff;
  background: #f59e0b;
  border: none;
  border-radius: var(--radius-xs);
  cursor: pointer;
  transition: background var(--duration-slow);
}

@media (hover: hover) {
  .warning-login-btn:hover {
    background: #d97706;
  }
}
</style>
