<template>
  <div class="forge-credentials-row">
    <div class="forge-cred-desc">{{ description }}</div>

    <!-- Hosts with a stored token. The token itself is never returned by the
         server (write-only), so we only know that one exists. -->
    <div v-for="host in hosts" :key="host" class="forge-cred-host">
      <span class="forge-cred-host-name">{{ host }}</span>
      <span class="forge-cred-badge">{{ t('settings.items.forgeTokenSet') }}</span>
      <button class="forge-cred-btn" @click="clearToken(host)">
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
        class="forge-cred-btn primary"
        :disabled="!newHost || !newToken || saving"
        @click="saveToken"
      >
        {{ t('settings.items.forgeTokenSave') }}
      </button>
    </div>

    <div v-if="error" class="forge-cred-error">{{ error }}</div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
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
}
.forge-cred-host {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
}
.forge-cred-host-name {
  flex: 1;
  font-family: var(--mono-font, monospace);
  font-size: 13px;
}
.forge-cred-badge {
  font-size: 12px;
  color: #2da44e;
}
.forge-cred-add {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.forge-cred-input {
  flex: 1;
  min-width: 140px;
  padding: 8px 10px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-primary);
  color: var(--text-primary);
}
.forge-cred-btn {
  padding: 8px 14px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: transparent;
  color: var(--text-primary);
  cursor: pointer;
}
.forge-cred-btn.primary {
  border-color: var(--accent-color);
  color: var(--accent-color);
}
.forge-cred-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.forge-cred-error {
  color: #cf222e;
  font-size: 13px;
}
</style>
