<template>
  <Teleport to="body">
    <div ref="overlayRef" tabindex="-1" class="install-overlay" @click.self="handleOverlayClick" @keydown.escape="handleEscape">
      <div class="install-box">
        <div class="install-title">
          <span class="install-title-icon"><PackagePlus :size="16" /></span>
          <span>{{ t('welcomeInfo.install') }} {{ backendName }}</span>
        </div>

        <!-- Command row: always visible. It is the fallback when an automatic
             install cannot work (no permission to the global npm prefix, an
             air-gapped network, a container the user cannot install into), and
             it is also what the server actually runs — same source. -->
        <div class="install-hint">{{ t('welcomeInfo.manualInstallHint') }}</div>
        <div class="install-cmd-row">
          <code class="install-cmd">{{ installCmd }}</code>
          <CopyButton :text="installCmd" class="btn-copy" :duration="2000" />
        </div>

        <!-- Where it runs. A remote server or a container installs there, not on
             the device showing this dialog, and saying so up front avoids a
             "it says installed but my laptop has nothing" report. -->
        <div class="install-note">{{ t('welcomeInfo.installRunsOnServer') }}</div>

        <!-- Progress / outcome. Kept in one block so the dialog height does not
             jump between states. -->
        <div v-if="installing" class="install-status install-status--busy">
          <LoadingIndicator size="sm" inline />
          <span>{{ t('welcomeInfo.installing') }}</span>
        </div>
        <div v-else-if="error" class="install-status install-status--error">
          <div class="install-status-title">{{ t('welcomeInfo.installFailedTitle') }}</div>
          <!-- The server's message already carries the actionable part: the
               missing executable, or the installer's own output tail. -->
          <pre class="install-output">{{ error }}</pre>
        </div>
        <div v-else-if="result" class="install-status install-status--ok">
          <div class="install-status-title">
            {{ result.refreshed ? t('welcomeInfo.installSuccess') : t('welcomeInfo.installSuccessNeedsRescan') }}
          </div>
          <pre v-if="result.output" class="install-output">{{ result.output }}</pre>
        </div>

        <div class="install-actions">
          <button class="fbtn" @click="$emit('close')">{{ t('common.close') }}</button>
          <button
            v-if="!result && installCmd"
            class="fbtn fbtn-primary install-run-btn"
            :disabled="installing"
            @click="startInstall"
          >
            <LoadingIndicator v-if="installing" size="sm" inline />
            <span>{{ installing ? t('welcomeInfo.installing') : t('welcomeInfo.installNow') }}</span>
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { PackagePlus } from 'lucide-vue-next'
import CopyButton from '@/components/common/CopyButton.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import { useAgentInstall, type AgentInstallResponse } from '@/composables/useAgentInstall'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{
  /** Registry id of the backend to install (e.g. "claude"). */
  backendId: string
  backendName: string
  installCmd: string
}>()

const emit = defineEmits<{
  close: []
  /** Emitted after a successful install so the parent can refresh its list. */
  installed: [result: AgentInstallResponse]
}>()

const { t } = useI18n()
const overlayRef = ref<HTMLDivElement | null>(null)
let unregisterBack: (() => void) | null = null

const { installing, error, result, run } = useAgentInstall()

// Escape (PC) closes the dialog, mirroring the Android back handler.
function handleEscape() {
  emit('close')
}

// Closing mid-install is allowed (the server keeps working — it deliberately
// does not follow the request's lifetime), but a stray click on the backdrop is
// the easiest way to lose the output the user was waiting for. So the backdrop
// is inert while an install runs; Escape and the Close button still work.
function handleOverlayClick() {
  if (installing.value) return
  emit('close')
}

async function startInstall() {
  const res = await run(props.backendId)
  if (res) emit('installed', res)
}

onMounted(() => {
  // Focus the overlay so Escape key works immediately (same as BottomSheet/ModalDialog).
  nextTick(() => overlayRef.value?.focus())
  unregisterBack = registerBackHandler({
    id: 'agent-install-dialog',
    canGoBack: () => true,
    goBack: () => emit('close'),
    priority: PRIORITY_OVERLAY,
  })
})

onBeforeUnmount(() => {
  if (unregisterBack) { unregisterBack(); unregisterBack = null }
})
</script>

<style scoped>
.install-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: var(--z-modal);
  padding: 0 var(--space-8);
  animation: overlay-in 0.15s ease;
  outline: none;
}

.install-box {
  background: var(--bg-secondary, #fff);
  border-radius: var(--radius-lg);
  padding: calc(18 * var(--dp)) var(--space-7) calc(14 * var(--dp));
  max-width: var(--modal-width-narrow);
  width: 100%;
  display: flex;
  flex-direction: column;
  box-shadow: 0 calc(8 * var(--dp)) calc(32 * var(--dp)) rgba(0, 0, 0, 0.2);
  animation: dlg-in 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.install-title {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  font-weight: var(--font-weight-semibold);
  font-size: var(--font-size-lg);
  color: var(--text-primary, #1a1a1a);
  margin-bottom: var(--space-5);
}

.install-title-icon {
  flex-shrink: 0;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  border-radius: var(--radius-sm);
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.install-hint {
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #555);
  margin-bottom: var(--space-4);
}

.install-cmd-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  background: var(--bg-tertiary, #f0f0f0);
  border: 1px solid var(--border-color, #ddd);
  border-radius: var(--radius-sm);
  padding: var(--space-4) var(--space-5);
  margin-bottom: var(--space-4);
}

.install-cmd {
  flex: 1;
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  color: var(--text-primary, #1a1a1a);
  word-break: break-all;
  min-width: 0;
}

.btn-copy {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: var(--bg-primary, #fff);
  color: var(--text-secondary, #555);
  cursor: pointer;
  transition: all var(--duration-base);
}

@media (hover: hover) {
  .btn-copy:hover {
    color: var(--accent-color);
  }
}

.install-note {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #888);
  margin-bottom: var(--space-5);
  line-height: var(--line-height-normal);
}

.install-status {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  border-radius: var(--radius-sm);
  padding: var(--space-4) var(--space-5);
  margin-bottom: var(--space-5);
  font-size: var(--font-size-sm);
}

.install-status--busy {
  flex-direction: row;
  align-items: center;
  gap: var(--space-4);
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #555);
}

.install-status--error {
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-red) 35%, transparent);
}

.install-status--ok {
  background: color-mix(in srgb, var(--color-green) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-green) 35%, transparent);
}

.install-status-title {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
}

/* Installer output is machine text: monospace, wrapped (never a horizontal
   scrollbar — the tail can be long) and height-capped so a chatty installer
   cannot push the footer buttons off screen. */
.install-output {
  margin: 0;
  max-height: calc(160 * var(--dp));
  overflow-y: auto;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
  color: var(--text-secondary, #555);
  white-space: pre-wrap;
  word-break: break-word;
}

.install-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-4);
}

/* The Close button uses the shared .fbtn pill language
   (assets/modal-footer-btn.css), the same as every other dialog footer. */
</style>

<style>
@keyframes dlg-in {
  from { opacity: 0; transform: scale(0.9); }
  to { opacity: 1; transform: scale(1); }
}

@keyframes overlay-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
</style>
