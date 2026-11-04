<template>
  <div class="https-notice" :class="`https-notice--${variant}`" data-role="https-notice">
    <span class="https-notice__icon"><ShieldAlert :size="variant === 'banner' ? 15 : 16" /></span>
    <div class="https-notice__body">
      <div class="https-notice__title">{{ title }}</div>
      <div class="https-notice__text">{{ body }}</div>

      <div class="https-notice__actions">
        <button
          class="fbtn fbtn-primary"
          :class="{ 'fbtn-sm': variant === 'banner' }"
          type="button"
          data-action="https-notice-generate"
          :disabled="busy !== 'idle'"
          @click="generate(false)"
        >
          {{ actionLabel }}
        </button>
        <button
          v-if="needsForce"
          class="fbtn fbtn-danger"
          :class="{ 'fbtn-sm': variant === 'banner' }"
          type="button"
          data-action="https-notice-force"
          :disabled="busy !== 'idle'"
          @click="generate(true)"
        >
          {{ t('liveShare.httpsNotice.forceAction') }}
        </button>
      </div>

      <div
        v-if="message"
        class="https-notice__hint https-notice__hint--error"
        data-role="https-notice-message"
      >
        {{ message }}
      </div>
      <div v-else-if="variant === 'block'" class="https-notice__hint">
        {{ t('liveShare.httpsNotice.hint') }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * "This page needs HTTPS" — one component, two shapes.
 *
 * `block` replaces the create-meeting form when a browser cannot capture at all
 * (an insecure origin has no `navigator.mediaDevices`), and `banner` sits at the
 * top of the meeting list in the desktop shell, where capture works but the link
 * sent to other people does not. Same copy, same button, same flow — only the
 * frame differs, so the two entry points cannot drift apart.
 *
 * The flow is the certificate panel's, compressed to one click: generate a
 * self-signed certificate → restart the server → hand the page over to https.
 * Failures stay in the notice itself; a second dialog for "a CA certificate is
 * already on disk" would lose the context that explains why it matters.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ShieldAlert } from 'lucide-vue-next'
import { getNative } from '@/utils/clawbenchNative'
import { appLog } from '@/utils/appLog'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{ variant: 'block' | 'banner' }>()

const { t } = useI18n()

/**
 * How long the restarted server is given before the page is handed over.
 *
 * The restart replaces the HTTP listener with an HTTPS one, so this page cannot
 * talk to the server the moment it comes back — waiting is the only option, and
 * four seconds is what the certificate panel has used since it shipped.
 */
const RESTART_WAIT_MS = 4000

type Busy = 'idle' | 'generating' | 'restarting'

const busy = ref<Busy>('idle')
/** A CA-issued certificate is on disk; overwriting needs a second click. */
const needsForce = ref(false)
const message = ref('')

const origin = typeof window === 'undefined' ? '' : window.location.origin

const title = computed(() =>
  props.variant === 'block'
    ? t('liveShare.httpsNotice.blockTitle')
    : t('liveShare.httpsNotice.bannerTitle'),
)

const body = computed(() =>
  props.variant === 'block'
    ? t('liveShare.httpsNotice.blockBody', { origin })
    : t('liveShare.httpsNotice.bannerBody'),
)

const actionLabel = computed(() => {
  if (busy.value === 'generating') return t('liveShare.httpsNotice.generating')
  if (busy.value === 'restarting') return t('liveShare.httpsNotice.restarting')
  return t('liveShare.httpsNotice.action')
})

async function generate(force: boolean): Promise<void> {
  if (busy.value !== 'idle') return
  busy.value = 'generating'
  needsForce.value = false
  message.value = ''
  try {
    const resp = await fetch('/api/config/tls/self-signed', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ hosts: [], force }),
    })
    if (resp.status === 409) {
      // A CA certificate is on disk. Replacing it cannot be undone from here, so
      // it takes a second, explicit click.
      needsForce.value = true
      message.value = t('liveShare.httpsNotice.forceBody')
      busy.value = 'idle'
      return
    }
    if (!resp.ok) throw new Error(`generate failed: ${resp.status}`)
    const status = (await resp.json()) as { httpsUrl?: string }
    await restart(status.httpsUrl)
  } catch (e) {
    appLog.w('HttpsNotice', 'generate cert failed', e)
    message.value = t('liveShare.httpsNotice.failed')
    busy.value = 'idle'
  }
}

async function restart(httpsUrl?: string): Promise<void> {
  busy.value = 'restarting'
  try {
    const resp = await fetch('/api/config/restart', { method: 'POST' })
    if (!resp.ok) throw new Error(`restart failed: ${resp.status}`)
  } catch (e) {
    appLog.w('HttpsNotice', 'restart failed', e)
    message.value = t('liveShare.httpsNotice.restartFailed')
    busy.value = 'idle'
    return
  }
  const target = httpsUrl || httpsFallback()
  if (!target) {
    message.value = t('liveShare.httpsNotice.restartFailed')
    busy.value = 'idle'
    return
  }
  setTimeout(() => { void handOver(target) }, RESTART_WAIT_MS)
}

/**
 * Move to the https address.
 *
 * A browser navigates itself. The desktop shell cannot: its `will-navigate` hook
 * treats another origin as external and would open the system browser, leaving
 * the shell on a dead http URL. It gets the address through the native bridge
 * instead, which also rewrites the saved server entry.
 */
async function handOver(target: string): Promise<void> {
  const native = getNative()
  if (native?.isDesktopApp?.() === true) {
    try {
      if (native.switchServerUrl) {
        await native.switchServerUrl(target)
        return
      }
      // Older shells have no dedicated channel. Connecting still lands on the
      // right address; it just leaves the old http entry in the server list.
      if (native.connectToServer) {
        await native.connectToServer(target, '')
        return
      }
    } catch (e) {
      appLog.w('HttpsNotice', 'native handover failed', e)
    }
  }
  window.location.replace(target)
}

/** Same host and port, TLS instead of plain HTTP. */
function httpsFallback(): string {
  try {
    const url = new URL(window.location.href)
    url.protocol = 'https:'
    return url.origin
  } catch {
    return ''
  }
}
</script>

<style scoped>
.https-notice {
  display: flex;
  gap: var(--space-4);
  padding: var(--space-6);
  border: 1px solid color-mix(in srgb, var(--color-orange, #d18616) 45%, transparent);
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--color-orange, #d18616) 10%, transparent);
}

/* The banner is a strip under the panel header: full-bleed, and it must not
   grow when the list is short (the list is the scrolling part). */
.https-notice--banner {
  padding: var(--space-4) var(--space-5);
  border-top: 0;
  border-right: 0;
  border-left: 0;
  border-radius: 0;
  flex: none;
}

.https-notice__icon {
  flex: none;
  margin-top: 1px;
  display: inline-flex;
  color: var(--color-orange, #d18616);
}

.https-notice__body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.https-notice__title {
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--text-primary);
  line-height: 1.45;
}

.https-notice__text {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  line-height: 1.65;
  overflow-wrap: anywhere;
}

.https-notice__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-4);
}

/* The banner is a strip, not a dialog footer: a full-height pill would read as
   the panel's primary action and push the list down. */
.fbtn-sm {
  height: calc(26 * var(--dp));
  padding: 0 var(--space-5);
  font-size: var(--font-size-sm);
}

.https-notice__hint {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  line-height: 1.6;
}

.https-notice__hint--error {
  color: var(--color-red);
}
</style>
