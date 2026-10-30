<template>
  <div class="tls-panel">
    <div class="tls-panel__desc">{{ t('settings.items.tlsPanelDesc') }}</div>

    <div v-if="status === null" class="tls-panel__hint">
      <LoadingIndicator size="sm" inline />
      {{ t('common.loading') }}
    </div>

    <template v-else>
      <div class="tls-panel__row">
        <span class="tls-panel__label">{{ t('settings.items.tlsState') }}</span>
        <span class="tls-panel__value" :class="{ 'tls-panel__value--ok': status.active }">
          {{
            status.active
              ? status.selfSigned
                ? t('settings.items.tlsStateSelfSigned')
                : t('settings.items.tlsStateCA')
              : t('settings.items.tlsStateNone')
          }}
        </span>
      </div>

      <div v-if="hosts.length" class="tls-panel__row">
        <span class="tls-panel__label">{{ t('settings.items.tlsHosts') }}</span>
        <span class="tls-panel__value tls-panel__value--wrap">{{ hosts.join('、') }}</span>
      </div>

      <div v-if="status.notAfter" class="tls-panel__row">
        <span class="tls-panel__label">{{ t('settings.items.tlsNotAfter') }}</span>
        <span class="tls-panel__value">{{ formatDate(status.notAfter) }}</span>
      </div>

      <!-- Addresses that are not currently on a NIC (a fixed office IP, a DNS
           name). The machine's own addresses are always included. -->
      <div class="tls-panel__field">
        <label class="tls-panel__label" for="tls-extra-hosts">
          {{ t('settings.items.tlsExtraHosts') }}
        </label>
        <input
          id="tls-extra-hosts"
          v-model="extraHosts"
          class="tls-panel__input"
          type="text"
          :placeholder="t('settings.items.tlsExtraHostsPlaceholder')"
        />
        <div class="tls-panel__hint">{{ t('settings.items.tlsExtraHostsHint') }}</div>
      </div>

      <div class="tls-panel__actions">
        <button class="fbtn fbtn-primary" type="button" :disabled="busy" @click="generate(false)">
          {{ status.active ? t('settings.items.tlsRegenerate') : t('settings.items.tlsGenerate') }}
        </button>
        <button
          v-if="needsForce"
          class="fbtn fbtn-danger"
          type="button"
          :disabled="busy"
          @click="generate(true)"
        >
          {{ t('settings.items.tlsForceOverwrite') }}
        </button>
      </div>

      <div v-if="error" class="tls-panel__error">{{ error }}</div>

      <!-- The listener is created at startup, so a new certificate only takes
           effect after a restart. Saying so is not enough — offer the button. -->
      <div v-if="restartRequired" class="tls-panel__notice">
        <div>{{ t('settings.items.tlsRestartRequired') }}</div>
        <div class="tls-panel__actions">
          <button class="fbtn fbtn-primary" type="button" :disabled="restarting" @click="restart">
            {{ restarting ? t('settings.items.tlsRestarting') : t('settings.items.tlsRestartNow') }}
          </button>
        </div>
        <div v-if="status.httpsUrl" class="tls-panel__hint">
          {{ t('settings.items.tlsOpenHint') }}
          <a :href="status.httpsUrl" class="tls-panel__link">{{ status.httpsUrl }}</a>
        </div>
      </div>

      <div v-if="status.serving" class="tls-panel__hint">
        {{ t('settings.items.tlsServingHint') }}
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
/**
 * HTTPS certificate panel.
 *
 * Generating a self-signed certificate used to be a CLI-only affair
 * (`clawbench --gen-cert`), which assumed the person who needs HTTPS can open a
 * terminal. The certificate also has to be regenerated whenever the machine's
 * address changes — exactly when nobody is at a shell.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { appLog } from '@/utils/appLog'

const { t } = useI18n()

interface TLSStatus {
  active: boolean
  serving: boolean
  selfSigned: boolean
  certFile?: string
  keyFile?: string
  certDir: string
  hosts?: string[]
  notAfter?: string
  httpsUrl?: string
  restartRequired?: boolean
}

const status = ref<TLSStatus | null>(null)
const extraHosts = ref('')
const busy = ref(false)
const restarting = ref(false)
const error = ref('')
/** Set when the existing certificate came from a CA: overwriting needs force. */
const needsForce = ref(false)

const restartRequired = computed(() => status.value?.restartRequired === true)
const hosts = computed(() => status.value?.hosts ?? [])

function formatDate(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString()
}

function parseExtraHosts(): string[] {
  return extraHosts.value
    .split(/[,，\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

async function load(): Promise<void> {
  try {
    const resp = await fetch('/api/config/tls')
    if (!resp.ok) throw new Error(`status ${resp.status}`)
    status.value = (await resp.json()) as TLSStatus
  } catch (e) {
    appLog.w('TlsPanel', 'load tls status failed', e)
    error.value = t('settings.items.tlsLoadFailed')
  }
}

async function generate(force: boolean): Promise<void> {
  busy.value = true
  error.value = ''
  needsForce.value = false
  try {
    const resp = await fetch('/api/config/tls/self-signed', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ hosts: parseExtraHosts(), force }),
    })
    if (resp.status === 409) {
      // A CA certificate is on disk. Overwriting it cannot be undone from here,
      // so it takes a second, explicit click.
      needsForce.value = true
      error.value = t('settings.items.tlsNotSelfSigned')
      return
    }
    if (!resp.ok) {
      const data = (await resp.json().catch(() => ({}))) as { error?: string }
      throw new Error(data.error || `status ${resp.status}`)
    }
    status.value = (await resp.json()) as TLSStatus
  } catch (e) {
    appLog.w('TlsPanel', 'generate cert failed', e)
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function restart(): Promise<void> {
  restarting.value = true
  error.value = ''
  try {
    const resp = await fetch('/api/config/restart', { method: 'POST' })
    if (!resp.ok) throw new Error(`restart failed: ${resp.status}`)
    // The page is served over the OLD protocol; once the listener comes back as
    // HTTPS this tab can no longer talk to it. Give the server a moment, then
    // move to the https:// address (the browser will show the certificate
    // warning once).
    const target = status.value?.httpsUrl
    if (target) {
      setTimeout(() => {
        window.location.replace(target)
      }, 4000)
    }
  } catch (e) {
    appLog.w('TlsPanel', 'restart failed', e)
    error.value = t('settings.items.tlsRestartFailed')
    restarting.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.tls-panel {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  padding: var(--space-6) var(--space-7);
}

.tls-panel__desc,
.tls-panel__hint {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
}

.tls-panel__row {
  display: flex;
  align-items: baseline;
  gap: var(--space-5);
  font-size: var(--font-size-sm);
}

.tls-panel__label {
  flex: 0 0 auto;
  color: var(--text-muted);
}

.tls-panel__value {
  min-width: 0;
  color: var(--text-primary);
  font-family: var(--font-mono, ui-monospace, monospace);
}

.tls-panel__value--ok {
  color: var(--color-success);
}

.tls-panel__value--wrap {
  overflow-wrap: anywhere;
}

.tls-panel__field {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.tls-panel__input {
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
}

.tls-panel__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-4);
}

.tls-panel__error {
  color: var(--color-red);
  font-size: var(--font-size-sm);
}

.tls-panel__notice {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-5);
  border: 1px solid color-mix(in srgb, var(--color-orange, #d18616) 45%, transparent);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--color-orange, #d18616) 10%, transparent);
  font-size: var(--font-size-sm);
}

.tls-panel__link {
  color: var(--accent-color);
  word-break: break-all;
}
</style>
