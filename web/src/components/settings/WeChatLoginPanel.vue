<template>
  <div class="wechat-login">
    <div class="wechat-login__desc">{{ t('settings.items.wechatLoginDesc') }}</div>

    <!-- Loading: the status fetch has not answered yet. Without this branch an
         already-connected user sees "not logged in" + a scan button until the
         request lands, which reads as "my login was lost". -->
    <div v-if="status === null" class="wechat-login__card">
      <div class="wechat-login__qr wechat-login__qr--empty">
        <LoadingIndicator size="md" />
      </div>
      <div class="wechat-login__hint">{{ t('settings.items.wechatLoading') }}</div>
    </div>

    <!-- Login in flight: QR image + live status. -->
    <div v-else-if="login" class="wechat-login__card">
      <div class="wechat-login__head">
        <span class="wechat-login__name">{{ stateLabel }}</span>
        <span v-if="login.refresh_max > 0" class="wechat-login__meta">
          {{ t('settings.items.wechatQRRefresh') }} {{ login.refresh }}/{{ login.refresh_max }}
        </span>
      </div>

      <div v-if="qrDataUrl" class="wechat-login__qr">
        <img :src="qrDataUrl" :alt="t('settings.items.wechatScanHint')" />
      </div>
      <div v-else class="wechat-login__qr wechat-login__qr--empty">
        <LoadingIndicator size="md" />
      </div>

      <div class="wechat-login__hint">{{ t('settings.items.wechatScanHint') }}</div>
      <div v-if="login.error" class="wechat-login__error">{{ login.error }}</div>
      <div v-else-if="login.message" class="wechat-login__hint">{{ login.message }}</div>

      <div class="wechat-login__actions">
        <button type="button" class="fbtn" :disabled="busy" @click="cancelLogin">
          {{ t('settings.items.wechatCancel') }}
        </button>
        <button type="button" v-if="isTerminal" class="fbtn fbtn-primary" :disabled="busy" @click="startLogin">
          {{ t('settings.items.wechatRetry') }}
        </button>
      </div>
    </div>

    <!-- Session expired on the platform side (-14): the token is still stored
         but nothing is being delivered, so this must NOT look connected. -->
    <div v-else-if="connectionExpired" class="wechat-login__card">
      <div class="wechat-login__head">
        <span class="wechat-login__name">{{ t('settings.items.wechatConnectionExpired') }}</span>
        <span class="wechat-login__badge wechat-login__badge--bad">
          <AlertCircle :size="12" />
          {{ t('settings.items.wechatStateExpired') }}
        </span>
      </div>
      <div v-if="lastError" class="wechat-login__error">{{ lastError }}</div>
      <div class="wechat-login__hint">{{ t('settings.items.wechatConnectionExpiredHint') }}</div>
      <div class="wechat-login__actions">
        <button type="button" class="fbtn fbtn-primary" :disabled="busy" @click="startLogin">
          <LoadingIndicator v-if="busy" size="sm" inline class="wechat-login__spin" />
          <QrCode v-else :size="13" />
          {{ t('settings.items.wechatRelogin') }}
        </button>
        <button type="button" class="fbtn" :disabled="busy" @click="logout">
          {{ t('settings.items.wechatLogout') }}
        </button>
      </div>
    </div>

    <!-- Connected: show who is logged in and offer logout. -->
    <div v-else-if="connected" class="wechat-login__card">
      <div class="wechat-login__head">
        <span class="wechat-login__name">{{ t('settings.items.wechatLoggedIn') }}</span>
        <span class="wechat-login__badge wechat-login__badge--ok">
          <Check :size="12" />
          {{ t('settings.items.wechatStateRunning') }}
        </span>
      </div>
      <div v-if="botId" class="wechat-login__meta">{{ t('settings.items.wechatBotId') }}: {{ botId }}</div>
      <div class="wechat-login__actions">
        <button type="button" class="fbtn" :disabled="busy" @click="logout">
          <LoadingIndicator v-if="busy" size="sm" inline class="wechat-login__spin" />
          <LogOut v-else :size="13" />
          {{ t('settings.items.wechatLogout') }}
        </button>
        <button type="button" class="fbtn" :disabled="busy" @click="startLogin">
          {{ t('settings.items.wechatRelogin') }}
        </button>
      </div>
    </div>

    <!-- Idle: nothing logged in, nothing in flight. -->
    <div v-else class="wechat-login__card">
      <div class="wechat-login__hint">{{ t('settings.items.wechatNotLoggedIn') }}</div>
      <div class="wechat-login__actions">
        <button type="button" class="fbtn fbtn-primary" :disabled="busy" @click="startLogin">
          <LoadingIndicator v-if="busy" size="sm" inline class="wechat-login__spin" />
          <QrCode v-else :size="13" />
          {{ t('settings.items.wechatScanLogin') }}
        </button>
      </div>
    </div>

    <div v-if="error" class="wechat-login__error">{{ error }}</div>
    <div v-else-if="statusFailed" class="wechat-login__error">
      {{ t('settings.items.wechatStatusFailed') }}
    </div>

    <!-- Account risk: this is an unofficial protocol surface. Stated plainly
         once, without a confirmation dialog (it would fire on every visit). -->
    <div class="wechat-login__warning">{{ t('settings.items.wechatRiskWarning') }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, Check, LogOut, QrCode } from 'lucide-vue-next'
import QRCode from 'qrcode'
import { apiDelete, apiGet, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

const TAG = 'WeChatLogin'
const { t } = useI18n()

interface LoginView {
  login_id: string
  state: string
  qr_url: string
  qr_seq: number
  refresh: number
  refresh_max: number
  bot_id: string
  message: string
  error: string
  expires_at: number
}

interface StatusView {
  push_mode: string
  has_token: boolean
  bot_id: string
  user_id: string
  base_url: string
  state: string
  last_error: string
}

const POLL_INTERVAL_MS = 1200
const REQUEST_TIMEOUT_MS = 15000
/** Consecutive transient poll failures tolerated before giving up. */
const MAX_POLL_FAILURES = 5
/** QR is rasterised large and downscaled by CSS, so it stays crisp at any UI scale. */
const QR_PIXEL_SIZE = 512

const status = ref<StatusView | null>(null)
const login = ref<LoginView | null>(null)
const qrDataUrl = ref('')
const busy = ref(false)
const error = ref('')
/** True when the status endpoint itself is unreachable (distinct from a login error). */
const statusFailed = ref(false)

/** Stops every loop and the in-flight request handling once the panel is gone. */
let cancelled = false
/**
 * Generation of the active poll loop. Bumped by cancel/unmount so a loop that is
 * already awaiting can tell it has been superseded — without this, cancelling
 * leaves the loop running, and the next tick re-renders the cancelled session
 * (the backend keeps the snapshot for 10 minutes, so the GET still returns 200).
 */
let loopToken = 0
/** Releases the pending poll sleep immediately on unmount. */
let cancelSleep: (() => void) | null = null

const TERMINAL_STATES = new Set(['connected', 'expired', 'failed', 'cancelled'])

const isTerminal = computed(() => !!login.value && TERMINAL_STATES.has(login.value.state))
const botId = computed(() => status.value?.bot_id ?? '')
const lastError = computed(() => status.value?.last_error ?? '')
const connectionExpired = computed(() => status.value?.state === 'expired')
const connected = computed(
  () => status.value?.has_token === true && status.value.state !== 'expired' && !login.value,
)

const stateLabel = computed(() => {
  const s = login.value?.state ?? ''
  switch (s) {
    case 'pending': return t('settings.items.wechatStatePending')
    case 'scanned': return t('settings.items.wechatStateScanned')
    case 'connected': return t('settings.items.wechatStateConnected')
    case 'expired': return t('settings.items.wechatStateExpired')
    case 'failed': return t('settings.items.wechatStateFailed')
    case 'cancelled': return t('settings.items.wechatStateCancelled')
    default: return t('settings.items.wechatStatePending')
  }
})

/** Last qr_seq successfully rendered, so repeated polls do not redraw the same QR. */
let renderedSeq = -1

async function renderQR(view: LoginView) {
  if (!view.qr_url || view.qr_seq === renderedSeq) return
  try {
    qrDataUrl.value = await QRCode.toDataURL(view.qr_url, {
      width: QR_PIXEL_SIZE,
      margin: 1,
      errorCorrectionLevel: 'M',
    })
    // Only record the seq AFTER a successful render: consuming it up front would
    // leave the plate stuck on the spinner for that QR with no way to retry.
    renderedSeq = view.qr_seq
  } catch (e) {
    appLog.w(TAG, 'QR render failed', e)
    error.value = t('settings.items.wechatQRRenderFailed')
  }
}

function sleep(ms: number) {
  return new Promise<void>(resolve => {
    const timer = setTimeout(() => {
      cancelSleep = null
      resolve()
    }, ms)
    cancelSleep = () => {
      clearTimeout(timer)
      cancelSleep = null
      resolve()
    }
  })
}

function stopLoop() {
  loopToken++
  cancelSleep?.()
}

async function loadStatus(): Promise<boolean> {
  try {
    status.value = await apiGet<StatusView>('/api/push/wechat/status', {
      timeoutMs: REQUEST_TIMEOUT_MS,
    })
    statusFailed.value = false
    return true
  } catch (e) {
    appLog.w(TAG, 'status load failed', e)
    // Silence here would render the "not logged in" card, which reads as
    // "configure the bot" when the truth is "the server did not answer".
    statusFailed.value = true
    return false
  }
}

async function pollLoop(id: string) {
  const mine = ++loopToken
  let failures = 0

  while (!cancelled && mine === loopToken) {
    await sleep(POLL_INTERVAL_MS)
    if (cancelled || mine !== loopToken) return

    let view: LoginView
    try {
      view = await apiGet<LoginView>(
        `/api/push/wechat/login?id=${encodeURIComponent(id)}`,
        { timeoutMs: REQUEST_TIMEOUT_MS },
      )
    } catch (e) {
      if (cancelled || mine !== loopToken) return
      const code = (e as Error & { status?: number }).status
      // 404: the session was reaped or the server restarted — a new login is the
      // only way forward. Other 4xx are permanent too (a malformed id would
      // otherwise be retried forever at 1.2s).
      if (code === 404 || (code !== undefined && code >= 400 && code < 500)) {
        appLog.w(TAG, 'login session gone', { id, code })
        login.value = null
        return
      }
      failures++
      if (failures >= MAX_POLL_FAILURES) {
        appLog.w(TAG, 'giving up polling', { id, failures })
        login.value = null
        error.value = t('settings.items.wechatPollGaveUp')
        return
      }
      continue
    }
    if (cancelled || mine !== loopToken) return

    failures = 0
    login.value = view
    await renderQR(view)

    if (view.state === 'connected') {
      // Adopt the result immediately rather than trusting a follow-up GET: if
      // that request failed, the panel would fall back to "not logged in" right
      // after a successful scan.
      status.value = {
        push_mode: 'wechat',
        has_token: true,
        bot_id: view.bot_id,
        user_id: status.value?.user_id ?? '',
        base_url: status.value?.base_url ?? '',
        state: 'running',
        last_error: '',
      }
      login.value = null
      void loadStatus()
      return
    }
    if (TERMINAL_STATES.has(view.state)) return
  }
}

async function startLogin() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  renderedSeq = -1
  qrDataUrl.value = ''
  try {
    const view = await apiPost<LoginView>('/api/push/wechat/login', {}, {
      timeoutMs: REQUEST_TIMEOUT_MS,
    })
    // The panel may have been torn down while the POST was in flight; `login` is
    // still null at that point, so onBeforeUnmount had nothing to cancel.
    if (cancelled) {
      void apiDelete(`/api/push/wechat/login?id=${encodeURIComponent(view.login_id)}`).catch(() => {})
      return
    }
    login.value = view
    await renderQR(view)
    void pollLoop(view.login_id)
  } catch (e) {
    appLog.w(TAG, 'start login failed', e)
    error.value = t('settings.items.wechatStartFailed')
  } finally {
    busy.value = false
  }
}

async function cancelLogin() {
  const id = login.value?.login_id
  // Stop the loop BEFORE the request: otherwise the next tick (≤1.2s) re-renders
  // the cancelled session and the QR card comes back.
  stopLoop()
  login.value = null
  qrDataUrl.value = ''
  renderedSeq = -1
  if (!id) return
  busy.value = true
  try {
    await apiDelete(`/api/push/wechat/login?id=${encodeURIComponent(id)}`, {
      timeoutMs: REQUEST_TIMEOUT_MS,
    })
  } catch (e) {
    appLog.w(TAG, 'cancel failed', e)
  } finally {
    busy.value = false
  }
}

async function logout() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await apiPost('/api/push/wechat/logout', {}, { timeoutMs: REQUEST_TIMEOUT_MS })
    await loadStatus()
  } catch (e) {
    appLog.w(TAG, 'logout failed', e)
    error.value = t('settings.items.wechatLogoutFailed')
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  void loadStatus()
})

onBeforeUnmount(() => {
  cancelled = true
  stopLoop()
  // Tell the server to stop the login goroutine: it would otherwise keep
  // polling for up to 8 minutes with no UI to report to.
  const id = login.value?.login_id
  if (id && !isTerminal.value) {
    void apiDelete(`/api/push/wechat/login?id=${encodeURIComponent(id)}`).catch(() => {})
  }
})
</script>

<style scoped>
.wechat-login {
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}
.wechat-login__desc {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.wechat-login__card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-5);
  padding: var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}
.wechat-login__head {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  width: 100%;
}
.wechat-login__name {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
}
.wechat-login__meta {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  font-family: var(--font-mono);
}
.wechat-login__badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: calc(2 * var(--dp)) var(--space-4);
  border-radius: var(--radius-full);
  font-size: var(--font-size-sm);
}
.wechat-login__badge--ok {
  color: var(--color-success);
  background: color-mix(in srgb, var(--color-success) 12%, transparent);
}
.wechat-login__badge--bad {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
}
.wechat-login__qr {
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(200 * var(--dp));
  height: calc(200 * var(--dp));
  padding: var(--space-4);
  border-radius: var(--radius-sm);
  /* A QR code must stay high-contrast regardless of theme: a dark-on-dark
     render is unscannable, so the plate is pinned to white. */
  background: #fff;
}
.wechat-login__qr img {
  width: 100%;
  height: 100%;
  display: block;
}
.wechat-login__qr--empty {
  background: var(--bg-tertiary);
}
.wechat-login__hint {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  text-align: center;
  line-height: var(--line-height-normal);
}
.wechat-login__actions {
  display: flex;
  gap: var(--space-4);
  flex-wrap: wrap;
  justify-content: center;
}
.wechat-login__error {
  color: var(--color-red);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
  /* overflow-wrap (not the deprecated word-break: break-word) so a long error
     string — an iLink API message, for instance — wraps instead of overflowing
     the card. */
  overflow-wrap: anywhere;
}
/* The spinner sits inside a neutral pill; without this its arc takes the accent
   colour instead of the pill's text colour (same reason as the other settings
   components that put a LoadingIndicator in a .fbtn). */
.wechat-login__spin {
  --li-color: currentColor;
}
.wechat-login__warning {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border-color);
}
</style>
