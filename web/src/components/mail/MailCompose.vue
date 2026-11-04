<template>
  <ModalDialog :open="open" :title="title" size="wide" @close="close">
    <div class="mail-compose">
      <label class="mail-compose__row">
        <span class="mail-compose__label">{{ t('mail.compose.to') }}</span>
        <input v-model="form.to" class="mail-compose__input" type="text" placeholder="name@example.com" />
      </label>
      <div v-if="showCc" class="mail-compose__row">
        <span class="mail-compose__label">{{ t('mail.compose.cc') }}</span>
        <input v-model="form.cc" class="mail-compose__input" type="text" placeholder="name@example.com" />
      </div>
      <div v-if="showCc" class="mail-compose__row">
        <span class="mail-compose__label">{{ t('mail.compose.bcc') }}</span>
        <input v-model="form.bcc" class="mail-compose__input" type="text" placeholder="name@example.com" />
      </div>
      <div v-if="!showCc" class="mail-compose__cc-toggle">
        <button class="mail-text-btn" type="button" @click="showCc = true">{{ t('mail.compose.addCc') }}</button>
      </div>

      <label class="mail-compose__row">
        <span class="mail-compose__label">{{ t('mail.compose.subject') }}</span>
        <input v-model="form.subject" class="mail-compose__input" type="text" />
      </label>

      <textarea
        ref="bodyRef"
        v-model="form.text"
        class="mail-compose__body"
        :placeholder="t('mail.compose.bodyPlaceholder')"
      />

      <div v-if="attachments.length > 0" class="mail-compose__attachments">
        <span v-for="(a, i) in attachments" :key="a.path" class="mail-compose__attachment">
          <Paperclip class="mail-compose__attachment-icon" />
          <span class="mail-compose__attachment-name">{{ a.filename }}</span>
          <button class="mail-compose__attachment-remove" type="button" :title="t('mail.compose.removeAttachment')"
            @click="attachments.splice(i, 1)">
            <X />
          </button>
        </span>
      </div>

      <p v-if="error" class="mail-compose__error">{{ error }}</p>
      <input ref="fileInput" type="file" multiple hidden @change="onFilesPicked" />
    </div>

    <template #footer>
      <button class="fbtn fbtn--ghost" :disabled="sending" @click="close">{{ t('mail.compose.cancel') }}</button>
      <button class="fbtn" :disabled="sending" @click="attach">
        {{ t('mail.compose.attach') }}
      </button>
      <button class="fbtn fbtn--primary" :disabled="sending || uploading" @click="send">
        {{ sending ? t('mail.compose.sending') : t('mail.compose.send') }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
/**
 * The composer.
 *
 * A draft is saved locally (debounced) while typing and deleted when the send
 * succeeds. Drafts deliberately stay local: syncing them to the server's Drafts
 * folder would need a second APPEND/EXPUNGE state machine for a feature whose
 * only promise is "do not lose what I typed".
 */
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Paperclip, X } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { apiPatch, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { mailErrorMessage, parseAddressList, type MailDraftFile, type MailReplyContext } from '@/utils/mailTypes'
import { refreshMailUnread } from '@/composables/useMailUnread'

const props = defineProps<{
  open: boolean
  accountId: number
  initial?: Partial<MailReplyContext> & { body?: string; draftId?: number }
}>()
const emit = defineEmits<{ close: []; sent: [] }>()

const { t } = useI18n()

const form = reactive({
  to: '',
  cc: '',
  bcc: '',
  subject: '',
  text: '',
  inReplyTo: '',
  references: '',
})
const attachments = ref<MailDraftFile[]>([])
const draftId = ref(0)
const sending = ref(false)
const uploading = ref(false)
const error = ref('')
const showCc = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const bodyRef = ref<HTMLTextAreaElement | null>(null)

const title = computed(() => t('mail.compose.title'))

let saveTimer: ReturnType<typeof setTimeout> | null = null
let dirty = false

function resetFromInitial(): void {
  const init = props.initial ?? {}
  form.to = init.to ?? ''
  form.cc = init.cc ?? ''
  form.bcc = ''
  form.subject = init.subject ?? ''
  form.text = (init.body ?? '') + (init.quote ?? '')
  form.inReplyTo = init.inReplyTo ?? ''
  form.references = init.references ?? ''
  attachments.value = []
  draftId.value = init.draftId ?? 0
  error.value = ''
  showCc.value = !!(init.cc ?? '')
  dirty = false
}

watch(
  () => props.open,
  (open) => {
    if (open) {
      resetFromInitial()
      // Focus the body on the next tick so the modal's enter animation is not
      // fighting the browser's scroll-into-view.
      setTimeout(() => bodyRef.value?.focus({ preventScroll: true }), 0)
    } else {
      void flushDraft()
    }
  },
  { immediate: true },
)

// Autosave: the timer only runs while the dialog is open and something changed.
watch(
  () => [form.to, form.cc, form.bcc, form.subject, form.text, attachments.value.length],
  () => {
    dirty = true
    if (!props.open) return
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => void flushDraft(), 3000)
  },
)

function isEmptyDraft(): boolean {
  return !form.to.trim() && !form.cc.trim() && !form.bcc.trim() &&
    !form.subject.trim() && !form.text.trim() && attachments.value.length === 0
}

async function flushDraft(): Promise<void> {
  if (!dirty || !props.open || isEmptyDraft()) return
  dirty = false
  const payload = {
    accountId: props.accountId,
    to: form.to,
    cc: form.cc,
    bcc: form.bcc,
    subject: form.subject,
    bodyText: form.text,
    bodyHtml: '',
    inReplyTo: form.inReplyTo,
    references: form.references,
    attachments: attachments.value,
  }
  try {
    if (draftId.value) {
      await apiPatch(`/api/mail/drafts/${draftId.value}`, payload)
    } else {
      const data = await apiPost<{ draft: { id: number } }>('/api/mail/drafts', payload)
      draftId.value = data.draft?.id ?? 0
    }
  } catch (e) {
    appLog.w('MailCompose', 'save draft failed', e)
  }
}

function attach(): void {
  fileInput.value?.click()
}

async function onFilesPicked(e: Event): Promise<void> {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (files.length === 0) return
  uploading.value = true
  error.value = ''
  try {
    for (const file of files) {
      const fd = new FormData()
      fd.append('file', file)
      const resp = await fetch('/api/upload/file', { method: 'POST', body: fd })
      const data = await resp.json().catch(() => ({}))
      if (!resp.ok || !data?.ok) {
        throw new Error(data?.error || t('mail.compose.uploadFailed'))
      }
      attachments.value.push({ filename: file.name, path: String(data.path), size: file.size })
    }
  } catch (err) {
    appLog.w('MailCompose', 'upload failed', err)
    error.value = (err as Error).message || t('mail.compose.uploadFailed')
  } finally {
    uploading.value = false
  }
}

async function send(): Promise<void> {
  error.value = ''
  const to = parseAddressList(form.to)
  const cc = parseAddressList(form.cc)
  const bcc = parseAddressList(form.bcc)
  if (to.length === 0 && cc.length === 0 && bcc.length === 0) {
    error.value = t('mail.err.noRecipients')
    return
  }
  sending.value = true
  try {
    await apiPost('/api/mail/send', {
      accountId: props.accountId,
      to,
      cc,
      bcc,
      subject: form.subject,
      text: form.text,
      inReplyTo: form.inReplyTo,
      references: form.references,
      attachments: attachments.value,
      draftId: draftId.value,
    }, { timeoutMs: 60_000 })
    dirty = false
    void refreshMailUnread()
    emit('sent')
    emit('close')
  } catch (e) {
    appLog.w('MailCompose', 'send failed', e)
    error.value = mailErrorMessage(e, t)
  } finally {
    sending.value = false
  }
}

function close(): void {
  void flushDraft()
  emit('close')
}
</script>

<style scoped>
.mail-compose {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
  min-height: calc(320 * var(--dp));
}
.mail-compose__row {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  border-bottom: 1px solid var(--border-color);
  padding-bottom: calc(3 * var(--dp));
}
.mail-compose__label {
  flex: none;
  width: calc(48 * var(--dp));
  font-size: var(--font-size-sm, 12px);
  color: var(--text-secondary);
}
.mail-compose__input {
  flex: 1;
  min-width: 0;
  padding: calc(3 * var(--dp)) 0;
  font-size: var(--font-size-sm, 12px);
  color: var(--text-primary);
  background: none;
  border: none;
  outline: none;
}
.mail-compose__cc-toggle {
  display: flex;
  justify-content: flex-end;
}
.mail-compose__body {
  flex: 1;
  min-height: calc(180 * var(--dp));
  padding: var(--space-2, 4px) 0;
  font-family: inherit;
  font-size: var(--font-size-sm, 12px);
  line-height: 1.65;
  color: var(--text-primary);
  background: none;
  border: none;
  outline: none;
  resize: vertical;
}
.mail-compose__attachments {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2, 4px);
}
.mail-compose__attachment {
  display: inline-flex;
  align-items: center;
  gap: calc(4 * var(--dp));
  padding: calc(2 * var(--dp)) calc(6 * var(--dp));
  font-size: var(--font-size-xs, 11px);
  color: var(--text-secondary);
  background: var(--bg-tertiary);
  border-radius: var(--radius-sm, 4px);
  max-width: 100%;
}
.mail-compose__attachment-icon {
  width: calc(11 * var(--dp));
  height: calc(11 * var(--dp));
}
.mail-compose__attachment-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mail-compose__attachment-remove {
  display: inline-flex;
  padding: 0;
  border: none;
  background: none;
  color: var(--text-tertiary);
  cursor: pointer;
}
.mail-compose__attachment-remove :deep(svg) {
  width: calc(11 * var(--dp));
  height: calc(11 * var(--dp));
}
.mail-compose__error {
  margin: 0;
  font-size: var(--font-size-sm, 12px);
  color: var(--color-danger, #d1242f);
}
.mail-text-btn {
  padding: 0;
  border: none;
  background: none;
  font-size: var(--font-size-sm, 12px);
  color: var(--accent-color);
  cursor: pointer;
}
</style>
