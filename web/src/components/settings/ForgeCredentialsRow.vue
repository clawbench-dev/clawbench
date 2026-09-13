<template>
  <div class="forge-credentials-row">
    <div class="forge-cred-desc">{{ description }}</div>

    <!-- Hosts with a stored token. The token itself is never returned by the
         server (write-only), so we only know that one exists. -->
    <div v-for="host in hosts" :key="host" class="forge-cred-host">
      <div class="forge-cred-host-head">
        <span class="forge-cred-host-name">{{ host }}</span>
        <span class="forge-cred-badge">
          <Check :size="12" />
          {{ t('settings.items.forgeTokenSet') }}
        </span>
      </div>
      <!-- Verification is independent of saving: this re-checks the credential
           already on the server, without retyping it. -->
      <div class="forge-cred-host-actions">
        <button class="fbtn" :disabled="verifyingHost === host" @click="verifySaved(host)">
          <Loader2 v-if="verifyingHost === host" :size="13" class="forge-spin" />
          <ShieldCheck v-else :size="13" />
          {{ t('settings.items.forgeTokenVerify') }}
        </button>
        <button class="fbtn" @click="clearToken(host)">
          {{ t('settings.items.forgeTokenClear') }}
        </button>
        <span v-if="results[host]" class="forge-cred-result" :class="results[host].ok ? 'ok' : 'bad'">
          <component :is="results[host].ok ? Check : AlertCircle" :size="13" />
          <span>{{ results[host].text }}</span>
        </span>
      </div>
    </div>

    <!-- Add / replace a token for a host. -->
    <div class="forge-cred-add">
      <input
        v-model="newHost"
        class="forge-cred-input"
        :placeholder="t('settings.items.forgeHostPlaceholder')"
      />
      <input
        v-model="newToken"
        class="forge-cred-input"
        type="password"
        autocomplete="off"
        :placeholder="t('settings.items.forgeTokenPlaceholder')"
      />
      <!-- Verify the typed token before saving it; this does not store it. -->
      <button
        class="fbtn"
        :disabled="!newHost || !newToken || verifyingDraft"
        @click="verifyDraft"
      >
        <Loader2 v-if="verifyingDraft" :size="13" class="forge-spin" />
        <ShieldCheck v-else :size="13" />
        {{ t('settings.items.forgeTokenVerify') }}
      </button>
      <button
        class="fbtn fbtn-primary"
        :disabled="!newHost || !newToken || saving"
        @click="saveToken"
      >
        {{ t('settings.items.forgeTokenSave') }}
      </button>
    </div>

    <div v-if="draftResult" class="forge-cred-result" :class="draftResult.ok ? 'ok' : 'bad'">
      <component :is="draftResult.ok ? Check : AlertCircle" :size="13" />
      <span>{{ draftResult.text }}</span>
    </div>

    <div v-if="error" class="forge-cred-error">
      <AlertCircle :size="13" />
      <span>{{ error }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, AlertCircle, ShieldCheck, Loader2 } from 'lucide-vue-next'
import { setForgeToken, deleteForgeToken, verifyForgeToken, ForgeApiError } from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'

defineProps<{ description?: string }>()

const TAG = 'ForgeCredentials'
const { t } = useI18n()

const hosts = ref<string[]>([])
const newHost = ref('')
const newToken = ref('')
const saving = ref(false)
const error = ref('')

// Verification state is separate from saving: checking a token never stores it.
const verifyingDraft = ref(false)
const verifyingHost = ref('')
const draftResult = ref<VerifyResult | null>(null)
const results = ref<Record<string, VerifyResult>>({})

interface VerifyResult {
  ok: boolean
  text: string
}

/** Render a verify response as a human-readable outcome. */
function toResult(res: { ok: boolean; identity?: string; code?: string; error?: string }): VerifyResult {
  if (res.ok) {
    return { ok: true, text: t('settings.items.forgeVerifyOk', { identity: res.identity || '' }) }
  }
  // Distinguish a rejected token from an unreachable host: the latter is not
  // proof the token is bad, so the wording must not claim it is.
  switch (res.code) {
    case 'auth':
      return { ok: false, text: t('settings.items.forgeVerifyAuth') }
    case 'network':
      return { ok: false, text: t('settings.items.forgeVerifyNetwork') }
    case 'rate_limit':
      return { ok: false, text: t('settings.items.forgeVerifyRateLimit') }
    case 'ForgeNoCredential':
      return { ok: false, text: t('settings.items.forgeVerifyNoToken') }
    default:
      return { ok: false, text: res.error || t('settings.items.forgeVerifyFailed') }
  }
}

/** Verify the token currently typed in the form (without saving it). */
async function verifyDraft() {
  if (!newHost.value || !newToken.value) return
  verifyingDraft.value = true
  draftResult.value = null
  error.value = ''
  try {
    const res = await verifyForgeToken({
      host: newHost.value.trim().toLowerCase(),
      token: newToken.value,
    })
    draftResult.value = toResult(res)
  } catch (err) {
    error.value = err instanceof ForgeApiError ? err.message : String(err)
  } finally {
    verifyingDraft.value = false
  }
}

/** Verify the credential already stored for a host (no retyping needed). */
async function verifySaved(host: string) {
  verifyingHost.value = host
  error.value = ''
  try {
    const res = await verifyForgeToken({ host })
    results.value = { ...results.value, [host]: toResult(res) }
  } catch (err) {
    error.value = err instanceof ForgeApiError ? err.message : String(err)
  } finally {
    verifyingHost.value = ''
  }
}

async function loadHosts() {
  try {
    const res = await fetch('/api/config', { headers: { 'X-Locale': String(navigator.language || 'en') } })
    if (!res.ok) return
    const cfg = await res.json()
    const list = cfg?.forge?.credential_hosts
    hosts.value = Array.isArray(list) ? list : []
  } catch (err) {
    appLog.w(TAG, 'load hosts failed', err)
  }
}

async function saveToken() {
  saving.value = true
  error.value = ''
  try {
    await setForgeToken(newHost.value.trim().toLowerCase(), newToken.value)
    newHost.value = ''
    newToken.value = ''
    draftResult.value = null
    await loadHosts()
  } catch (err) {
    error.value = err instanceof ForgeApiError ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

async function clearToken(host: string) {
  error.value = ''
  try {
    await deleteForgeToken(host)
    // Drop any stale verify result for the host we just cleared.
    const next = { ...results.value }
    delete next[host]
    results.value = next
    await loadHosts()
  } catch (err) {
    error.value = err instanceof ForgeApiError ? err.message : String(err)
  }
}

onMounted(loadHosts)
</script>

<style scoped>
.forge-credentials-row {
  padding: 12px 16px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.forge-cred-desc {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: 1.5;
}
.forge-cred-host {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}
.forge-cred-host-head {
  display: flex;
  align-items: center;
  gap: 10px;
}
.forge-cred-host-name {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: var(--font-size-md);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.forge-cred-host-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
/* Verify outcome — green when the token works, red when it does not. */
.forge-cred-result {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--font-size-sm);
  min-width: 0;
}
.forge-cred-result.ok { color: var(--color-success); }
.forge-cred-result.bad { color: var(--color-red); }
.forge-cred-result span {
  overflow: hidden;
  text-overflow: ellipsis;
}
.forge-spin {
  animation: forge-spin 1s linear infinite;
}
@keyframes forge-spin {
  to { transform: rotate(360deg); }
}
@media (prefers-reduced-motion: reduce) {
  .forge-spin { animation: none; }
}
/* "Configured" badge — tinted pill, theme-aware. */
.forge-cred-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 999px;
  color: var(--color-success);
  background: color-mix(in srgb, var(--color-success) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-success) 35%, transparent);
}
.forge-cred-add {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}
.forge-cred-input {
  flex: 1;
  min-width: 130px;
  box-sizing: border-box;
  padding: 7px 12px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
}
.forge-cred-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 2px var(--focus-ring);
}
.forge-cred-error {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--color-red);
  font-size: var(--font-size-md);
}
</style>
