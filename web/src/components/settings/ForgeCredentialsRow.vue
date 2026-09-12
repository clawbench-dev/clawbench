<template>
  <div class="forge-credentials-row">
    <div class="forge-cred-desc">{{ description }}</div>

    <!-- Hosts with a stored token. The token itself is never returned by the
         server (write-only), so we only know that one exists. -->
    <div v-for="host in hosts" :key="host" class="forge-cred-host">
      <span class="forge-cred-host-name">{{ host }}</span>
      <span class="forge-cred-badge">
        <Check :size="12" />
        {{ t('settings.items.forgeTokenSet') }}
      </span>
      <button class="fbtn" @click="clearToken(host)">
        {{ t('settings.items.forgeTokenClear') }}
      </button>
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
      <button
        class="fbtn fbtn-primary"
        :disabled="!newHost || !newToken || saving"
        @click="saveToken"
      >
        {{ t('settings.items.forgeTokenSave') }}
      </button>
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
import { Check, AlertCircle } from 'lucide-vue-next'
import { setForgeToken, deleteForgeToken, ForgeApiError } from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'

defineProps<{ description?: string }>()

const TAG = 'ForgeCredentials'
const { t } = useI18n()

const hosts = ref<string[]>([])
const newHost = ref('')
const newToken = ref('')
const saving = ref(false)
const error = ref('')

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
  font-size: 13px;
  line-height: 1.5;
}
.forge-cred-host {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}
.forge-cred-host-name {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* "Configured" badge — tinted pill, theme-aware. */
.forge-cred-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  font-size: 11px;
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
  font-size: 13px;
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
  font-size: 13px;
}
</style>
