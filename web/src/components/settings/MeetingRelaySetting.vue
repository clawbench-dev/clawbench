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
      <input
        id="meeting-relay-key"
        v-model="apiKey"
        class="relay-panel__input"
        type="text"
        spellcheck="false"
        autocomplete="off"
        data-role="relay-key"
      />
    </div>

    <div class="relay-panel__field">
      <label class="relay-panel__label" for="meeting-relay-secret">
        {{ t('settings.items.meetingRelaySecret') }}
      </label>
      <input
        id="meeting-relay-secret"
        v-model="apiSecret"
        class="relay-panel__input"
        type="password"
        spellcheck="false"
        autocomplete="new-password"
        data-role="relay-secret"
        :placeholder="
          hasSecret
            ? t('settings.items.meetingRelaySecretStored')
            : t('settings.items.meetingRelaySecretPlaceholder')
        "
      />
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
 * The secret is write-only — the server answers `hasSecret` and never the
 * value — so the field shows a masked placeholder and is sent only when the
 * user typed something.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useLiveSFUConfig } from '@/composables/useLiveSFUConfig'

const { t } = useI18n()
const { state, busy, load, saveAndTest } = useLiveSFUConfig()

const url = ref('')
const apiKey = ref('')
const apiSecret = ref('')
const message = ref('')
const ok = ref(false)

const configured = computed(() => state.value.configured)
const hasSecret = computed(() => state.value.hasSecret)

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
  // The secret is now stored server-side; keeping it in the field would send it
  // again on the next save for no reason.
  if (res.ok) {
    apiSecret.value = ''
    url.value = state.value.url
  }
}

onMounted(async () => {
  // Force a re-read: the create form may have loaded this singleton hours ago,
  // and this panel is where the value is edited.
  await load(true)
  url.value = state.value.url
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
