<template>
  <div class="mail-compose">
    <div class="mail-compose__head">
      <button class="mail-icon-btn" type="button" :title="t('mail.compose.back')" @click="close">
        <ArrowLeft />
      </button>
      <div class="mail-compose__title">{{ title }}</div>
    </div>

    <div class="mail-compose__fields">
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
    </div>

    <textarea
      ref="bodyRef"
      v-model="form.text"
      class="mail-compose__body"
      :placeholder="t('mail.compose.bodyPlaceholder')"
    />

    <div v-if="picked.length > 0" class="mail-compose__attachments">
      <span v-for="p in picked" :key="p.source.path" class="mail-compose__attachment"
        :class="{ 'mail-compose__attachment--staging': !p.staged }">
        <LoadingIndicator v-if="!p.staged" size="sm" inline />
        <Paperclip v-else class="mail-compose__attachment-icon" />
        <span class="mail-compose__attachment-name">{{ p.staged?.filename ?? baseName(p.source.path) }}</span>
        <button class="mail-compose__attachment-remove" type="button" :title="t('mail.compose.removeAttachment')"
          @click="removeAttached(p.source)">
          <X />
        </button>
      </span>
    </div>

    <p v-if="error" class="mail-compose__error">{{ error }}</p>

    <div class="mail-compose__actions">
      <button class="fbtn fbtn--ghost" :disabled="sending" @click="close">{{ t('mail.compose.cancel') }}</button>
      <span class="mail-compose__spacer" />
      <button class="fbtn" data-action="mail-attach" :disabled="sending" @click="attachOpen = true">
        {{ t('mail.compose.attach') }}
      </button>
      <button class="fbtn fbtn--primary" :disabled="sending || stagingCount > 0" @click="send">
        {{ sending ? t('mail.compose.sending') : t('mail.compose.send') }}
      </button>
    </div>
  </div>

  <!-- Same picker the chat composer uses (browse + search + upload), with the
       chat-only "recent …" tabs hidden. What it hands back is PATHS: a mail
       attachment has to be readable by the server, so each path is staged
       through /api/mail/attachments, which uses a file in place and packs a
       folder into one zip. -->
  <AttachmentPicker
    :open="attachOpen"
    variant="mail"
    :attached-files="attachFiles"
    @close="attachOpen = false"
    @add-attached="addAttached"
    @remove-attached="removeAttached"
  />
</template>

<script setup lang="ts">
/**
 * The composer, rendered as a drill-down view inside the mail panel.
 *
 * It used to be a modal on top of the list; the dock pane is narrow, and a
 * modal covered the very list the draft was written from. It now takes the
 * panel's full area and returns with the back arrow, exactly like the reader.
 *
 * A draft is saved locally (debounced) while typing and deleted when the send
 * succeeds. Drafts deliberately stay local: syncing them to the server's Drafts
 * folder would need a second APPEND/EXPUNGE state machine for a feature whose
 * only promise is "do not lose what I typed".
 */
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, Paperclip, X } from 'lucide-vue-next'
import { apiPatch, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { baseName } from '@/utils/path'
import { mailErrorMessage, parseAddressList, type MailDraftFile, type MailReplyContext } from '@/utils/mailTypes'
import { stageMailAttachments } from '@/utils/mailAttach'
import { refreshMailUnread } from '@/composables/useMailUnread'
import AttachmentPicker from '@/components/attach/AttachmentPicker.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import type { FileEntry } from '@/utils/fileAttachmentUtils'

const props = defineProps<{
  accountId: number
  initial?: Partial<MailReplyContext> & { body?: string; attachments?: MailDraftFile[]; draftId?: number }
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
/**
 * One attachment, as picked and as staged.
 *
 * `source` is what the picker shows as selected (the path the user chose — for
 * a folder that is the folder itself), `staged` is what the server will send
 * (for a folder, the zip it packed). Keeping both is what lets the picker
 * re-open with the folder still ticked: the staged path is a different file.
 */
interface PickedAttachment {
  source: FileEntry
  staged: MailDraftFile | null
}

const picked = ref<PickedAttachment[]>([])
/** What actually goes out: only entries the server has finished staging. */
const attachments = computed(() => picked.value
  .map(p => p.staged)
  .filter((a): a is MailDraftFile => !!a))
/** The picker's "already attached" view — source paths, not staged ones. */
const attachFiles = computed(() => picked.value.map(p => p.source))
const attachOpen = ref(false)
const draftId = ref(0)
const sending = ref(false)
/** How many picked paths are still being staged (folder zipping is not instant). */
const stagingCount = ref(0)
const error = ref('')
const showCc = ref(false)
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
  // Copied, not referenced: the composer mutates its list (remove attachment)
  // and must not reach back into the caller's request object.
  picked.value = (init.attachments ?? []).map((a) => ({ source: { path: a.path }, staged: { ...a } }))
  draftId.value = init.draftId ?? 0
  error.value = ''
  showCc.value = !!(init.cc ?? '')
  dirty = false
}

onMounted(() => {
  resetFromInitial()
  // The panel swaps views synchronously; focus after paint so the browser does
  // not scroll the freshly mounted view while it is still being laid out.
  setTimeout(() => bodyRef.value?.focus({ preventScroll: true }), 0)
})

onBeforeUnmount(() => {
  if (saveTimer) clearTimeout(saveTimer)
  void flushDraft()
})

// Autosave: the timer only runs while the composer is mounted and something changed.
watch(
  () => [form.to, form.cc, form.bcc, form.subject, form.text, picked.value.length],
  () => {
    dirty = true
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => void flushDraft(), 3000)
  },
)

function isEmptyDraft(): boolean {
  return !form.to.trim() && !form.cc.trim() && !form.bcc.trim() &&
    !form.subject.trim() && !form.text.trim() && picked.value.length === 0
}

async function flushDraft(): Promise<void> {
  if (!dirty || isEmptyDraft()) return
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

/**
 * A path came back from the picker: stage it, then show it.
 *
 * Staging is what makes a path sendable — a file is referenced in place, a
 * folder is packed into one zip. It happens per path (not once per confirm)
 * because the picker reports one entry at a time; each request is independent
 * and the row appears with a spinner until its own request lands.
 */
async function addAttached(path: string, isDir?: boolean): Promise<void> {
  if (picked.value.some(p => p.source.path === path)) return
  picked.value = [...picked.value, { source: { path, isDir: isDir === true }, staged: null }]
  stagingCount.value++
  try {
    const staged = await stageMailAttachments([path])
    if (staged.length === 0) throw new Error(t('mail.compose.uploadFailed'))
    picked.value = picked.value.map(p => (p.source.path === path ? { ...p, staged: staged[0] } : p))
  } catch (err) {
    appLog.w('MailCompose', 'stage attachment failed', path, err)
    // Drop the row: leaving an attachment that can never be sent would only
    // fail later, at send time, with a less useful message.
    picked.value = picked.value.filter(p => p.source.path !== path)
    error.value = (err as Error)?.message || t('mail.compose.uploadFailed')
  } finally {
    stagingCount.value--
  }
}

function removeAttached(entry: FileEntry): void {
  picked.value = picked.value.filter(p => p.source.path !== entry.path)
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
  flex: 1;
  min-height: 0;
}
.mail-compose__head {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px) var(--space-3, 6px);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
.mail-compose__title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-md, 14px);
  font-weight: 600;
  color: var(--text-primary);
}
.mail-compose__fields {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px) var(--space-3, 6px) 0;
  flex-shrink: 0;
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
  min-height: calc(120 * var(--dp));
  margin: 0 var(--space-3, 6px);
  padding: var(--space-2, 4px) 0;
  font-family: inherit;
  font-size: var(--font-size-sm, 12px);
  line-height: 1.65;
  color: var(--text-primary);
  background: none;
  border: none;
  outline: none;
  resize: none;
  overflow-y: auto;
}
.mail-compose__attachments {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2, 4px);
  padding: 0 var(--space-3, 6px);
  flex-shrink: 0;
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
/* Still being staged (a folder is zipped server-side): the row exists so the
   user sees the pick landed, but it is not part of the message yet. */
.mail-compose__attachment--staging {
  color: var(--text-hint);
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
  color: var(--text-hint);
  cursor: pointer;
}
.mail-compose__attachment-remove :deep(svg) {
  width: calc(11 * var(--dp));
  height: calc(11 * var(--dp));
}
.mail-compose__error {
  margin: 0;
  padding: 0 var(--space-3, 6px);
  font-size: var(--font-size-sm, 12px);
  color: var(--color-red);
  flex-shrink: 0;
}
.mail-compose__actions {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px) var(--space-3, 6px);
  border-top: 1px solid var(--border-color);
  flex-shrink: 0;
}
.mail-compose__spacer {
  flex: 1;
}
.mail-text-btn {
  padding: 0;
  border: none;
  background: none;
  font-size: var(--font-size-sm, 12px);
  color: var(--accent-color);
  cursor: pointer;
}
.mail-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  padding: 0;
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: none;
  color: var(--text-secondary);
  cursor: pointer;
  flex: none;
}
.mail-icon-btn:hover {
  background: var(--bg-hover, rgba(128, 128, 128, 0.12));
}
.mail-icon-btn :deep(svg) {
  width: calc(15 * var(--dp));
  height: calc(15 * var(--dp));
}
</style>
