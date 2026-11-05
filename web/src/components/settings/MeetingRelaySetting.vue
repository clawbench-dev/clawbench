<template>
  <div class="relay-panel">
    <div class="relay-panel__desc">{{ t('settings.items.meetingRelayDesc') }}</div>

    <!-- Whether a usable endpoint is stored. The create form's switch is gated
         on this fact, so the panel has to state it rather than leave the user
         to guess from the presence of a URL. -->
    <div class="relay-panel__row">
      <span class="relay-panel__label">{{ t('settings.items.meetingRelayState') }}</span>
      <span
        class="relay-panel__value"
        :class="configured ? 'relay-panel__value--ok' : 'relay-panel__value--idle'"
        data-role="relay-state"
      >
        {{
          configured
            ? t('settings.items.meetingRelayConfigured')
            : t('settings.items.meetingRelayUnconfigured')
        }}
      </span>
    </div>

    <div class="relay-panel__field">
      <label class="relay-panel__label" for="meeting-relay-url">
        {{ t('settings.items.meetingRelayUrl') }}
      </label>
      <input
        id="meeting-relay-url"
        v-model="url"
        class="relay-panel__input"
        type="text"
        spellcheck="false"
        data-role="relay-url"
        :placeholder="t('settings.items.meetingRelayUrlPlaceholder')"
      />
    </div>

    <div class="relay-panel__field">
      <label class="relay-panel__label" for="meeting-relay-key">
        {{ t('settings.items.meetingRelayKey') }}
      </label>
      <div class="relay-panel__secret">
        <input
          id="meeting-relay-key"
          :value="keyValue"
          :type="showKey ? 'text' : 'password'"
          class="relay-panel__input"
          spellcheck="false"
          autocomplete="off"
          data-role="relay-key"
          @input="onKeyInput"
          @focus="onMaskedFocus($event, keyMasked)"
        />
        <button
          type="button"
          class="relay-panel__eye"
          data-action="relay-key-toggle"
          :disabled="revealing"
          :title="showKey ? t('settings.items.meetingRelayHide') : t('settings.items.meetingRelayReveal')"
          :aria-label="showKey ? t('settings.items.meetingRelayHide') : t('settings.items.meetingRelayReveal')"
          @click="toggleReveal('key')"
        >
          <component :is="showKey ? EyeOff : Eye" :size="15" />
        </button>
      </div>
    </div>

    <div class="relay-panel__field">
      <label class="relay-panel__label" for="meeting-relay-secret">
        {{ t('settings.items.meetingRelaySecret') }}
      </label>
      <div class="relay-panel__secret">
        <input
          id="meeting-relay-secret"
          :value="secretValue"
          :type="showSecret ? 'text' : 'password'"
          class="relay-panel__input"
          spellcheck="false"
          autocomplete="new-password"
          data-role="relay-secret"
          :placeholder="
            hasSecret
              ? t('settings.items.meetingRelaySecretStored')
              : t('settings.items.meetingRelaySecretPlaceholder')
          "
          @input="onSecretInput"
          @focus="onMaskedFocus($event, secretMasked)"
        />
        <button
          type="button"
          class="relay-panel__eye"
          data-action="relay-secret-toggle"
          :disabled="revealing"
          :title="showSecret ? t('settings.items.meetingRelayHide') : t('settings.items.meetingRelayReveal')"
          :aria-label="showSecret ? t('settings.items.meetingRelayHide') : t('settings.items.meetingRelayReveal')"
          @click="toggleReveal('secret')"
        >
          <component :is="showSecret ? EyeOff : Eye" :size="15" />
        </button>
      </div>
    </div>

    <div class="relay-panel__actions">
      <button
        class="fbtn fbtn-primary"
        type="button"
        data-action="relay-save-test"
        :disabled="busy"
        @click="save"
      >
        {{ busy ? t('settings.items.meetingRelayTesting') : t('settings.items.meetingRelayTest') }}
      </button>
    </div>

    <div
      v-if="message"
      class="relay-panel__result"
      :class="ok ? 'relay-panel__result--ok' : 'relay-panel__result--bad'"
      data-role="relay-result"
    >
      {{ message }}
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 中继服务 (LiveKit) endpoint + credential, in 设置 → 视频会议.
 *
 * Installation-wide, so it does not belong in the per-meeting create form: the
 * relay is deployed once and every meeting that opts in shares it. The create
 * form keeps only the per-meeting switch.
 *
 * The credential is masked: a stored key/secret shows as dots until the eye
 * asks the server for it (same shape as the provider key field). Nothing is
 * fetched just to render the panel, and an untouched field is never sent — an
 * empty value means "keep what is stored".
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Eye, EyeOff } from 'lucide-vue-next'
import { useLiveSFUConfig } from '@/composables/useLiveSFUConfig'

const { t } = useI18n()
const { state, busy, load, reveal, saveAndTest } = useLiveSFUConfig()

const url = ref('')
const apiKey = ref('')
const apiSecret = ref('')
const message = ref('')
const ok = ref(false)

/**
 * Dots standing in for a stored credential that has not been revealed. Without
 * it the fields look empty and "a credential is saved" is indistinguishable
 * from "none yet". The refs stay empty while the mask is shown: an empty value
 * on save means "keep the stored one", and the mask must never be sent as if it
 * were the credential.
 */
const KEY_MASK_CHAR = '•'
const KEY_MASK = KEY_MASK_CHAR.repeat(12)
const keyMasked = ref(false)
const secretMasked = ref(false)
const showKey = ref(false)
const showSecret = ref(false)
const revealing = ref(false)

const keyValue = computed(() => (keyMasked.value ? KEY_MASK : apiKey.value))
const secretValue = computed(() => (secretMasked.value ? KEY_MASK : apiSecret.value))

const configured = computed(() => state.value.configured)
const hasSecret = computed(() => state.value.hasSecret)

function maskFields(): void {
  keyMasked.value = hasSecret.value
  secretMasked.value = hasSecret.value
  apiKey.value = ''
  apiSecret.value = ''
  showKey.value = false
  showSecret.value = false
}

/**
 * Show / hide one credential. The first reveal fetches both from the server —
 * one request, and the pair is always edited together.
 */
async function toggleReveal(which: 'key' | 'secret'): Promise<void> {
  const shown = which === 'key' ? showKey : showSecret
  if (shown.value) {
    shown.value = false
    return
  }
  if (keyMasked.value || secretMasked.value) {
    revealing.value = true
    message.value = ''
    try {
      const stored = await reveal()
      // An empty answer while the read says a credential exists means the
      // server did not understand `reveal` (older binary). Say so instead of
      // rendering empty fields that look like the credential was lost.
      if (!stored.apiKey && !stored.apiSecret && hasSecret.value) {
        throw new Error(t('settings.items.meetingRelayRevealFailed'))
      }
      apiKey.value = stored.apiKey
      apiSecret.value = stored.apiSecret
      keyMasked.value = false
      secretMasked.value = false
    } catch (err) {
      ok.value = false
      message.value = err instanceof Error ? err.message : String(err)
      return
    } finally {
      revealing.value = false
    }
  }
  shown.value = true
}

/** Select the mask on focus so the first keystroke replaces it instead of
 *  landing in the middle of a run of bullets. */
function onMaskedFocus(event: FocusEvent, masked: boolean): void {
  if (!masked) return
  const el = event.target as HTMLInputElement | null
  el?.select()
}

function stripMask(event: Event, masked: boolean): string {
  const el = event.target as HTMLInputElement
  let next = el.value
  if (masked) {
    // Clicking into the mask without selecting it leaves bullets in the box;
    // strip them instead of storing them as part of the credential.
    next = next.split(KEY_MASK_CHAR).join('')
  }
  if (el.value !== next) el.value = next
  return next
}

function onKeyInput(event: Event): void {
  apiKey.value = stripMask(event, keyMasked.value)
  keyMasked.value = false
}

function onSecretInput(event: Event): void {
  apiSecret.value = stripMask(event, secretMasked.value)
  secretMasked.value = false
}

/**
 * Probe the draft values, and store them only if the probe passes.
 *
 * Test-then-save rather than save-then-test: a stored endpoint that cannot be
 * reached makes every later relay meeting fail at start, and the user has no
 * way to tell which of the three fields is wrong.
 */
async function save(): Promise<void> {
  if (busy.value) return
  if (!url.value.trim() || (!apiKey.value.trim() && !hasSecret.value)) {
    ok.value = false
    message.value = t('settings.items.meetingRelayMissing')
    return
  }
  message.value = ''
  const res = await saveAndTest({
    url: url.value,
    apiKey: apiKey.value,
    apiSecret: apiSecret.value,
  })
  ok.value = res.ok
  message.value = res.message
  // The credential is now stored server-side; keeping it in the fields would
  // send it again on the next save for no reason, and the dots say it is there.
  if (res.ok) {
    url.value = state.value.url
    maskFields()
  }
}

onMounted(async () => {
  // Force a re-read: the create form may have loaded this singleton hours ago,
  // and this panel is where the value is edited.
  await load(true)
  url.value = state.value.url
  maskFields()
})
</script>

<style scoped>
.relay-panel {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  padding: var(--space-6) var(--space-7);
}

.relay-panel__desc {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}

.relay-panel__row {
  display: flex;
  align-items: baseline;
  gap: var(--space-5);
  font-size: var(--font-size-sm);
}

.relay-panel__label {
  flex: 0 0 auto;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

.relay-panel__value {
  min-width: 0;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
}
.relay-panel__value--ok { color: var(--color-success); }
.relay-panel__value--idle { color: var(--text-secondary); }

.relay-panel__field {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

/* Input + eye: the button sits inside the field's right edge, like the
   provider key field, so the row keeps one control height. */
.relay-panel__secret {
  position: relative;
  display: flex;
  height: calc(30 * var(--dp));
}
.relay-panel__secret > .relay-panel__input {
  flex: 1;
  min-width: 0;
  /* Leave room for the button so a long credential never runs underneath it. */
  padding-right: calc(38 * var(--dp));
}
.relay-panel__eye {
  position: absolute;
  top: 50%;
  right: calc(4 * var(--dp));
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 0;
}
.relay-panel__eye:hover:not(:disabled) {
  color: var(--text-primary);
  background: color-mix(in srgb, var(--text-secondary) 12%, transparent);
}
.relay-panel__eye:disabled {
  cursor: default;
  opacity: var(--opacity-disabled);
}

.relay-panel__input {
  box-sizing: border-box;
  height: calc(30 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  font-family: var(--font-mono);
}
.relay-panel__input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}

.relay-panel__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-4);
}

.relay-panel__result {
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  overflow-wrap: anywhere;
}
.relay-panel__result--ok { color: var(--color-success); }
.relay-panel__result--bad { color: var(--color-red); }
</style>
