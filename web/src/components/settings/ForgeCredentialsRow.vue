<template>
  <div class="forge-credentials-row">
    <div class="forge-cred-desc">{{ description }}</div>

    <!-- Hosts with a stored token. The token itself is never returned by the
         server (write-only), so we only know that one exists. -->
    <div v-for="host in hosts" :key="host" class="forge-cred-host">
      <div class="forge-cred-host-head">
        <span class="forge-cred-host-name">{{ host }}</span>
        <!-- The scheme the host is reached with. Shown because an http-only
             internal instance looks identical to an https one otherwise, and a
             wrong scheme surfaces as an opaque connection error. -->
        <span v-if="schemes[host]" class="forge-cred-scheme">{{ schemes[host] }}</span>
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
/**
 * Resolved API scheme per host, from the config response.
 *
 * Kept beside `hosts` rather than folded into it because the host is the
 * identity (it is what the token and the binding are keyed by) while the scheme
 * is how that host happens to be reached. The server resolves it, so the UI
 * never has to guess.
 */
const schemes = ref<Record<string, string>>({})
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
function toResult(res: { ok: boolean; identity?: string; scheme?: string; code?: string; error?: string }): VerifyResult {
  if (res.ok) {
    // The scheme is part of the outcome: it is what the server actually used,
    // so a check that passed over http says so rather than leaving the user to
    // wonder which scheme the stored host resolved to.
    const identity = res.identity || ''
    const text = res.scheme
      ? t('settings.items.forgeVerifyOkScheme', { identity, scheme: res.scheme })
      : t('settings.items.forgeVerifyOk', { identity })
    return { ok: true, text }
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
      // The host is sent as typed. The server splits the scheme from the host,
      // so a URL pasted from a browser works and also records the scheme.
      host: newHost.value.trim(),
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
    const schemeMap = cfg?.forge?.credential_schemes
    schemes.value = schemeMap && typeof schemeMap === 'object' ? schemeMap : {}
  } catch (err) {
    appLog.w(TAG, 'load hosts failed', err)
  }
}

async function saveToken() {
  saving.value = true
  error.value = ''
  try {
    // Sent as typed: the server normalizes the host and records the scheme a URL
    // names, so "http://gitlab.internal" configures an http instance in one step.
    await setForgeToken(newHost.value.trim(), newToken.value)
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
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}
.forge-cred-desc {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.forge-cred-host {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-5) var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}
.forge-cred-host-head {
  display: flex;
  align-items: center;
  gap: var(--space-5);
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
/* The API scheme this host is reached with. Muted rather than accented: it is
   reference information, not a status. */
.forge-cred-scheme {
  flex-shrink: 0;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  padding: var(--space-1) var(--space-3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
}
.forge-cred-host-actions {
  display: flex;
  align-items: center;
  gap: var(--space-4);
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
  gap: var(--space-2);
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  padding: var(--space-1) var(--space-4);
  border-radius: var(--radius-full);
  color: var(--color-success);
  background: color-mix(in srgb, var(--color-success) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--color-success) 35%, transparent);
}
.forge-cred-add {
  display: flex;
  gap: var(--space-4);
  flex-wrap: wrap;
  align-items: center;
}
.forge-cred-input {
  flex: 1;
  min-width: 130px;
  box-sizing: border-box;
  padding:7px var(--space-6);
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
  gap: var(--space-3);
  color: var(--color-red);
  font-size: var(--font-size-md);
}
</style>
