<template>
  <div class="mail-setting">
    <div class="mail-head">
      <p class="mail-hint">{{ t('settings.items.mailAccountsDesc') }}</p>
      <button
        v-if="!formOpen"
        class="fbtn fbtn-primary mail-add"
        data-action="mail-add"
        @click="openCreate"
      >
        <Plus :size="14" />
        {{ t('settings.items.mailAdd') }}
      </button>
    </div>

    <template v-if="!formOpen">
      <div v-if="loading" class="mail-empty">{{ t('settings.items.mailLoading') }}</div>
      <div v-else-if="accounts.length === 0" class="mail-empty">
        {{ t('settings.items.mailEmpty') }}
      </div>

      <div v-else class="mail-list">
        <div
          v-for="a in accounts"
          :key="a.id"
          class="mail-row"
          :data-mail-id="a.id"
          @click="openEdit(a)"
        >
          <div class="mail-tile"><Mail :size="18" /></div>
          <div class="mail-row-body">
            <div class="mail-row-head">
              <button type="button" class="mail-name" @click.stop="openEdit(a)">
                {{ a.name || a.email }}
              </button>
              <span v-if="!a.enabled" class="mail-badge">{{ t('settings.items.mailDisabled') }}</span>
            </div>
            <div class="mail-meta">{{ a.email }}</div>
            <div class="mail-sub" :class="{ 'mail-sub--error': !!a.lastError }">
              {{ a.lastError || syncLine(a) }}
            </div>
            <div
              v-if="testResults[a.id]"
              class="mail-test-result"
              :class="testResults[a.id].ok ? 'mail-test-result--ok' : 'mail-test-result--fail'"
            >
              <component :is="testResults[a.id].ok ? CheckCircle2 : AlertCircle" :size="13" />
              <span>{{ testResults[a.id].message }}</span>
            </div>
          </div>
          <div class="mail-row-actions">
            <button
              type="button"
              class="mail-icon-btn"
              :disabled="testingId !== null"
              :title="t('settings.items.mailTest')"
              :aria-label="t('settings.items.mailTest')"
              @click.stop="testAccount(a)"
            >
              <component
                :is="testingId === a.id ? Loader2 : PlugZap"
                :size="16"
                :class="{ 'mail-spin': testingId === a.id }"
              />
            </button>
            <button
              type="button"
              class="mail-icon-btn mail-icon-btn--danger"
              :title="t('settings.items.mailDelete')"
              :aria-label="t('settings.items.mailDelete')"
              @click.stop="removeAccount(a)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
      </div>
    </template>

    <!-- The form replaces the list in place. A modal would put the provider
         picker inside a transformed overlay, where the app's viewport-anchored
         dropdown opens in the wrong place. -->
    <div v-else class="mail-form-wrap">
      <div class="mail-form-head">
        <!-- Only a back link: the settings card above already names this page
             ("邮箱账号"), so repeating the form title here stacked two headers
             that said almost the same thing. -->
        <button type="button" class="mail-back-link" @click="closeForm">
          <ChevronLeft :size="14" />
          <span>{{ t('common.back') }}</span>
        </button>
      </div>
      <MailAccountForm
        :key="editing?.id ?? 'new'"
        :account="editing"
        :presets="presets"
        @saved="onSaved"
        @cancel="closeForm"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Mailbox management — the settings area's list page for accounts.
 *
 * Layout, row shape, empty state and actions are copied from ProviderSetting:
 * both pages are "a list of endpoint + credential entries you can add, test and
 * delete", so they must read as the same page with different nouns.
 */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertCircle,
  CheckCircle2,
  ChevronLeft,
  Loader2,
  Mail,
  PlugZap,
  Plus,
  Trash2,
} from 'lucide-vue-next'
import MailAccountForm from '@/components/mail/MailAccountForm.vue'
import { useDialog } from '@/composables/useDialog'
import { useMailAccounts } from '@/composables/useMailAccounts'
import { refreshMailUnread } from '@/composables/useMailUnread'
import { appLog } from '@/utils/appLog'
import { mailErrorMessage, type MailAccount, type MailPreset } from '@/utils/mailTypes'

const { t, locale } = useI18n()
const { confirm } = useDialog()
const api = useMailAccounts()

const accounts = ref<MailAccount[]>([])
const presets = ref<MailPreset[]>([])
const loading = ref(false)
const formOpen = ref(false)
const editing = ref<MailAccount | null>(null)
const testingId = ref<number | null>(null)
const testResults = ref<Record<number, { ok: boolean; message: string }>>({})

function formatTime(unix: number): string {
  if (!unix) return ''
  return new Date(unix * 1000).toLocaleString(String(locale.value), {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function syncLine(a: MailAccount): string {
  return a.lastSyncAt
    ? t('settings.items.mailLastSync', { time: formatTime(a.lastSyncAt) })
    : t('settings.items.mailNeverSynced')
}

async function load(): Promise<void> {
  loading.value = true
  try {
    await api.load()
    accounts.value = api.accounts.value
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await Promise.all([load(), api.loadPresets()])
  presets.value = api.presets.value
})

function openCreate(): void {
  editing.value = null
  formOpen.value = true
}

function openEdit(a: MailAccount): void {
  editing.value = a
  formOpen.value = true
}

function closeForm(): void {
  formOpen.value = false
  editing.value = null
}

async function onSaved(): Promise<void> {
  formOpen.value = false
  editing.value = null
  await load()
  void refreshMailUnread()
}

async function testAccount(a: MailAccount): Promise<void> {
  testingId.value = a.id
  delete testResults.value[a.id]
  try {
    await api.test(a.id)
    testResults.value = {
      ...testResults.value,
      [a.id]: { ok: true, message: t('settings.items.mailTestOk', { name: a.name || a.email }) },
    }
  } catch (e) {
    appLog.w('MailSetting', 'test failed', e)
    testResults.value = {
      ...testResults.value,
      [a.id]: { ok: false, message: mailErrorMessage(e, t) },
    }
  } finally {
    testingId.value = null
  }
}

async function removeAccount(a: MailAccount): Promise<void> {
  const ok = await confirm(t('settings.items.mailDeleteConfirm', { name: a.name || a.email }), {
    dangerous: true,
  })
  if (!ok) return
  try {
    await api.remove(a.id)
    delete testResults.value[a.id]
    await load()
    void refreshMailUnread()
  } catch (e) {
    appLog.w('MailSetting', 'delete failed', e)
    testResults.value = {
      ...testResults.value,
      [a.id]: { ok: false, message: mailErrorMessage(e, t) },
    }
  }
}
</script>

<style scoped>
/* Mirrors ProviderSetting: same paddings, row surface, icon-button column. */
.mail-setting {
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.mail-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
}
.mail-hint {
  flex: 1;
  min-width: 0;
  margin: 0;
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.mail-add {
  flex-shrink: 0;
}
.mail-empty {
  color: var(--text-muted);
  font-size: var(--font-size-md);
}
.mail-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
.mail-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  cursor: pointer;
  min-width: 0;
  transition: border-color var(--duration-base), background var(--duration-base);
}
.mail-row:hover,
.mail-row:focus-visible {
  border-color: color-mix(in srgb, var(--accent-color) 45%, var(--border-color));
  outline: none;
}
.mail-tile {
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(34 * var(--dp));
  height: calc(34 * var(--dp));
  flex-shrink: 0;
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  color: var(--accent-color);
}
.mail-row-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  flex: 1;
  min-width: 0;
}
.mail-row-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}
.mail-name {
  border: none;
  background: transparent;
  padding: 0;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
  cursor: pointer;
  text-align: left;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mail-name:hover {
  text-decoration: underline;
}
.mail-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  color: var(--color-orange);
  white-space: nowrap;
}
.mail-badge::before {
  content: '';
  width: calc(5 * var(--dp));
  height: calc(5 * var(--dp));
  border-radius: var(--radius-full);
  background: currentColor;
  flex-shrink: 0;
}
.mail-meta {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.mail-sub {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  overflow-wrap: anywhere;
}
.mail-sub--error {
  color: var(--color-red);
}
.mail-test-result {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
}
.mail-test-result--ok {
  color: var(--color-green);
}
.mail-test-result--fail {
  color: var(--color-red);
}
.mail-row-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
}
.mail-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(32 * var(--dp));
  height: calc(32 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 0;
}
.mail-icon-btn:hover:not(:disabled) {
  color: var(--text-primary);
  background: color-mix(in srgb, var(--text-secondary) 12%, transparent);
}
.mail-icon-btn--danger:hover:not(:disabled) {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
}
.mail-icon-btn:disabled {
  cursor: default;
  opacity: var(--opacity-disabled);
}
.mail-spin {
  animation: mail-spin 1s linear infinite;
}
@keyframes mail-spin {
  to { transform: rotate(360deg); }
}
.mail-form-wrap {
  display: flex;
  flex-direction: column;
}
.mail-form-head {
  display: flex;
  align-items: center;
  padding: var(--space-3) var(--space-6) 0;
}
.mail-back {
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
.mail-back:hover {
  color: var(--text-primary);
  background: color-mix(in srgb, var(--text-secondary) 12%, transparent);
}
.mail-back-link {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  border: none;
  background: transparent;
  color: var(--accent-color);
  font-size: var(--font-size-sm);
  cursor: pointer;
  padding: 0;
}
</style>
