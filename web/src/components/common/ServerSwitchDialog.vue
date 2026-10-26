<template>
  <ModalDialog
    :open="open"
    :title="t('serverSwitch.title')"
    :z-index="2500"
    size="compact"
    @close="$emit('close')"
  >
    <div class="server-switch">
      <p class="server-switch__lead">{{ t('serverSwitch.lead') }}</p>

      <div v-if="loading" class="server-switch__hint">{{ t('common.loading') }}</div>
      <div v-else-if="servers.length === 0" class="server-switch__hint">{{ t('serverSwitch.empty') }}</div>
      <div v-else class="server-switch__list">
        <button
          v-for="srv in servers"
          :key="srv.url"
          type="button"
          class="server-switch__row"
          :class="{ 'is-current': srv.url === currentUrl, 'is-pending': srv.url === pendingUrl }"
          :aria-current="srv.url === currentUrl ? 'true' : undefined"
          :title="srv.url"
          :disabled="switching"
          @click="selectServer(srv)"
        >
          <span class="server-switch__icon">
            <Server :size="16" />
          </span>
          <span class="server-switch__body">
            <span class="server-switch__name">{{ getLabel(srv) }}</span>
            <span v-if="srv.name" class="server-switch__url">{{ srv.url }}</span>
          </span>
          <span v-if="srv.url === currentUrl" class="server-switch__current">
            {{ t('serverSwitch.current') }}
          </span>
          <ChevronRight v-else :size="15" class="server-switch__chevron" />
        </button>
      </div>

      <form v-if="pendingUrl" class="server-switch__password" @submit.prevent="confirmPassword">
        <div class="server-switch__password-head">
          {{ t('serverSwitch.passwordFor', { name: pendingLabel }) }}
        </div>
        <div class="server-switch__password-row">
          <input
            ref="passwordRef"
            v-model="password"
            class="server-switch__password-input"
            type="password"
            :placeholder="t('login.passwordPlaceholder')"
            autocomplete="current-password"
            :disabled="switching"
          />
          <button class="fbtn fbtn-primary server-switch__connect" type="submit" :disabled="!password || switching">
            {{ t('serverSwitch.connect') }}
          </button>
        </div>
      </form>

      <div v-if="error" class="server-switch__error">{{ error }}</div>
    </div>

    <template #footer>
      <button class="fbtn fbtn-danger server-switch__logout" type="button" @click="$emit('logout')">
        <LogOut :size="14" />
        <span>{{ t('login.logout') }}</span>
      </button>
      <button class="fbtn" type="button" @click="$emit('close')">{{ t('common.cancel') }}</button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronRight, LogOut, Server } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { useServerList, type ServerEntry } from '@/composables/useServerList'
import { getNative } from '@/utils/clawbenchNative'
import { appLog } from '@/utils/appLog'

const props = defineProps<{
  open: boolean
}>()

defineEmits<{
  close: []
  logout: []
}>()

const { t } = useI18n()
const { servers, load, save, getLabel } = useServerList()

const loading = ref(false)
const switching = ref(false)
const error = ref('')
const pendingUrl = ref('')
const password = ref('')
const passwordRef = ref<HTMLInputElement | null>(null)
const currentUrl = window.location.origin

const pendingLabel = computed(() => {
  const entry = servers.value.find(s => s.url === pendingUrl.value)
  return entry ? getLabel(entry) : pendingUrl.value
})

async function refresh() {
  loading.value = true
  error.value = ''
  try {
    await load()
  } catch (err) {
    appLog.w('ServerSwitchDialog', 'Failed to load server list', err)
    error.value = t('serverSwitch.loadFailed')
  } finally {
    loading.value = false
  }
}

watch(() => props.open, (open) => {
  if (!open) return
  pendingUrl.value = ''
  password.value = ''
  switching.value = false
  void refresh()
}, { immediate: true })

function selectServer(srv: ServerEntry) {
  if (switching.value || srv.url === currentUrl) return
  error.value = ''
  if (srv.password) {
    void connect(srv.url, srv.password)
    return
  }
  pendingUrl.value = srv.url
  password.value = ''
  void nextTick(() => passwordRef.value?.focus())
}

async function connect(url: string, pwd: string) {
  switching.value = true
  error.value = ''
  try {
    const native = getNative()
    if (native?.connectToServer) {
      await native.connectToServer(url, pwd)
      return
    }
    window.location.href = url + '/login'
  } catch (err) {
    appLog.w('ServerSwitchDialog', 'Failed to switch server', { url, err })
    error.value = t('serverSwitch.switchFailed')
    switching.value = false
  }
}

async function confirmPassword() {
  const url = pendingUrl.value
  if (!url || !password.value) return
  const entry = servers.value.find(s => s.url === url)
  try {
    await save(url, password.value, entry?.name)
  } catch (err) {
    appLog.w('ServerSwitchDialog', 'Failed to save server password', { url, err })
    error.value = t('serverSwitch.saveFailed')
    return
  }
  pendingUrl.value = ''
  await connect(url, password.value)
}
</script>

<style scoped>
.server-switch {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  width: 100%;
  padding: var(--space-6) var(--space-7);
  box-sizing: border-box;
}

.server-switch__lead {
  margin: 0;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.5;
}

.server-switch__hint {
  padding: var(--space-8) 0;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  text-align: center;
}

.server-switch__list {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  max-height: min(calc(340 * var(--dp)), 50vh);
  overflow-y: auto;
}

.server-switch__row {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  width: 100%;
  padding: calc(10 * var(--dp)) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
  transition:
    background var(--duration-base) ease,
    border-color var(--duration-base) ease,
    box-shadow var(--duration-base) ease,
    transform var(--duration-base) ease;
}

.server-switch__row.is-current {
  border-color: color-mix(in srgb, var(--accent-color) 50%, transparent);
  background: color-mix(in srgb, var(--accent-color) 7%, var(--bg-tertiary));
  box-shadow: inset calc(2 * var(--dp)) 0 0 var(--accent-color);
  cursor: default;
}

.server-switch__row.is-pending {
  border-color: var(--accent-color);
}

@media (hover: hover) {
  .server-switch__row:not(.is-current):hover {
    border-color: color-mix(in srgb, var(--accent-color) 38%, var(--border-color));
    background: color-mix(in srgb, var(--accent-color) 5%, var(--bg-tertiary));
    box-shadow: 0 calc(2 * var(--dp)) calc(8 * var(--dp)) rgba(0, 0, 0, 0.08);
    transform: translateY(calc(-1 * var(--dp)));
  }

  .server-switch__row:not(.is-current):hover .server-switch__chevron {
    color: var(--accent-color);
    transform: translateX(calc(2 * var(--dp)));
  }
}

.server-switch__row:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

.server-switch__icon {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: calc(32 * var(--dp));
  height: calc(32 * var(--dp));
  border-radius: calc(9 * var(--dp));
  background: color-mix(in srgb, var(--accent-color) 13%, transparent);
  color: var(--accent-color);
}

.server-switch__body {
  display: flex;
  flex-direction: column;
  gap: calc(2 * var(--dp));
  min-width: 0;
  flex: 1;
}

.server-switch__name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.server-switch__url {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.server-switch__current {
  flex-shrink: 0;
  padding: calc(2 * var(--dp)) var(--space-4);
  border-radius: var(--radius-full);
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
  color: var(--accent-color);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
}

.server-switch__chevron {
  flex-shrink: 0;
  color: var(--text-secondary);
  opacity: 0.6;
  transition: color var(--duration-base) ease, transform var(--duration-base) ease;
}

.server-switch__password {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-5);
  border: 1px solid color-mix(in srgb, var(--accent-color) 26%, var(--border-color));
  border-radius: var(--radius-md);
  background: var(--bg-tertiary);
}

.server-switch__password-head {
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}

.server-switch__password-row {
  display: flex;
  gap: var(--space-4);
}

.server-switch__password-input {
  flex: 1;
  min-width: 0;
  height: calc(32 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  box-sizing: border-box;
  transition: border-color var(--duration-base) ease, box-shadow var(--duration-base) ease;
}

.server-switch__password-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 calc(2 * var(--dp)) color-mix(in srgb, var(--accent-color) 18%, transparent);
}

.server-switch__connect {
  flex-shrink: 0;
  height: calc(32 * var(--dp));
}

.server-switch__error {
  padding: var(--space-3) var(--space-5);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--color-red, #ef4444) 10%, transparent);
  color: var(--color-red, #ef4444);
  font-size: var(--font-size-sm);
}

.server-switch__logout {
  margin-right: auto;
}
</style>
