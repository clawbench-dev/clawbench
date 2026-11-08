<template>
  <div class="mail-reader">
    <div class="mail-reader__head">
      <button class="mail-icon-btn" :title="t('mail.reader.back')" @click="emit('back')">
        <ArrowLeft />
      </button>
      <div class="mail-reader__title" :title="detail?.subject">{{ detail?.subject || t('mail.list.noSubject') }}</div>
      <button class="mail-icon-btn" :title="detail?.flagged ? t('mail.reader.unstar') : t('mail.reader.star')"
        :disabled="busy" @click="toggleFlagged">
        <Star :class="{ 'is-on': detail?.flagged }" />
      </button>
    </div>

    <!-- One icon row, matching every other quote entry point. ActionBar shows the
         labels when the pane can hold them and lets the row scroll (drag + edge
         fades) when it cannot, so no action is ever unreachable. -->
    <ActionBar
      ref="actionsRef"
      class="mail-reader__actions"
      mode="scroll"
      size="bar"
    >
      <button class="mail-icon-btn" :title="t('mail.reader.reply')" :aria-label="t('mail.reader.reply')"
        :disabled="busy" @click="emit('reply', 'reply')">
        <Reply />
        <span class="action-label">{{ t('mail.reader.reply') }}</span>
      </button>
      <button class="mail-icon-btn" :title="t('mail.reader.replyAll')" :aria-label="t('mail.reader.replyAll')"
        :disabled="busy" @click="emit('reply', 'replyAll')">
        <ReplyAll />
        <span class="action-label">{{ t('mail.reader.replyAll') }}</span>
      </button>
      <button class="mail-icon-btn" :title="t('mail.reader.forward')" :aria-label="t('mail.reader.forward')"
        :disabled="busy" @click="emit('reply', 'forward')">
        <Forward />
        <span class="action-label">{{ t('mail.reader.forward') }}</span>
      </button>
      <!-- Quote this email into a conversation. It goes through the SAME quote
           bar as a commit / task / issue, so it can carry an annotation and be
           sent to another session; the card jumps back here via `mailId`. -->
      <button class="mail-icon-btn" :title="t('mail.reader.quoteToChat')" :aria-label="t('mail.reader.quoteToChat')"
        @click="quoteToChat">
        <MessageSquare />
        <span class="action-label">{{ t('mail.reader.quoteToChat') }}</span>
      </button>
      <span class="mail-reader__spacer" />
      <button class="mail-icon-btn" :title="detail?.seen ? t('mail.reader.markUnread') : t('mail.reader.markRead')"
        :aria-label="detail?.seen ? t('mail.reader.markUnread') : t('mail.reader.markRead')"
        :disabled="busy" @click="toggleSeen">
        <Mail v-if="detail?.seen" />
        <MailOpen v-else />
        <span class="action-label">{{ detail?.seen ? t('mail.reader.markUnread') : t('mail.reader.markRead') }}</span>
      </button>
      <button class="mail-icon-btn is-danger" :title="t('mail.reader.delete')"
        :aria-label="t('mail.reader.delete')" :disabled="busy" @click="remove">
        <Trash2 />
        <span class="action-label">{{ t('mail.reader.delete') }}</span>
      </button>
    </ActionBar>

    <div class="mail-reader__meta">
      <div class="mail-reader__from">
        <span class="mail-reader__name">{{ detail ? mailSenderLabel(detail) : '' }}</span>
        <span class="mail-reader__addr">{{ detail?.fromAddr }}</span>
      </div>
      <div class="mail-reader__time">{{ sentAtLabel }}</div>
      <div v-if="detail?.to?.length" class="mail-reader__recipients">
        <span class="mail-reader__meta-label">{{ t('mail.reader.to') }}</span>
        {{ detail.to.map((a) => a.addr).join(', ') }}
      </div>
      <div v-if="detail?.cc?.length" class="mail-reader__recipients">
        <span class="mail-reader__meta-label">{{ t('mail.reader.cc') }}</span>
        {{ detail.cc.map((a) => a.addr).join(', ') }}
      </div>
    </div>

    <div class="mail-reader__body">
      <div v-if="loading" class="mail-reader__loading">
        <LoadingIndicator size="sm" inline />
      </div>
      <MailEmpty v-else-if="error" :icon="TriangleAlert" :title="t('mail.reader.errorTitle')" :desc="error" />
      <template v-else-if="detail">
        <div v-if="showImagesBar" class="mail-reader__images">
          <span>{{ t('mail.reader.imagesBlocked') }}</span>
          <button class="mail-chip" @click="allowRemoteImages = true">{{ t('mail.reader.showImages') }}</button>
        </div>
        <iframe
          v-if="docSource"
          class="mail-reader__frame"
          sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
          referrerpolicy="no-referrer"
          :srcdoc="docSource"
        />
        <pre v-else-if="detail.bodyText" class="mail-reader__text">{{ detail.bodyText }}</pre>
        <MailEmpty v-else :icon="MailOpen" :title="t('mail.reader.noBody')" />
      </template>
    </div>

    <div v-if="attachments.length > 0" class="mail-reader__attachments">
      <div class="mail-reader__attachments-title">{{ t('mail.reader.attachments', { n: attachments.length }) }}</div>
      <div v-for="a in attachments" :key="a.id" class="mail-attachment-row">
        <a
          class="mail-attachment"
          :href="`/api/mail/messages/${a.messageId}/attachments/${a.id}`"
          :download="a.filename"
        >
          <Paperclip class="mail-attachment__icon" />
          <span class="mail-attachment__name">{{ a.filename }}</span>
          <span class="mail-attachment__size">{{ formatMailSize(a.size) }}</span>
        </a>
        <!-- Add the attachment to the chat input. The bytes are re-uploaded
             into the project's uploads directory: chat attachments are
             validated to live inside the project, and the mail store is not
             inside it. -->
        <button
          class="mail-attachment__chat"
          :title="t('mail.reader.attachToChat')"
          :aria-label="t('mail.reader.attachToChat')"
          :disabled="attachingId === a.id"
          @click="attachToChat(a)"
        >
          <LoadingIndicator v-if="attachingId === a.id" size="sm" inline />
          <MessageSquarePlus v-else :size="14" />
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, Forward, Mail, MailOpen, MessageSquare, MessageSquarePlus, Paperclip, Reply, ReplyAll, Star, Trash2, TriangleAlert } from 'lucide-vue-next'
import MailEmpty from './MailEmpty.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { apiGet, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { buildMailDocument, hasRemoteImages } from '@/utils/mailHtml'
import {
  mailFullTimestamp,
  formatMailSize,
  mailErrorMessage,
  mailSenderLabel,
  type MailAttachment,
  type MailMessage,
  type MailMessageDetail,
} from '@/utils/mailTypes'
import { refreshMailUnread } from '@/composables/useMailUnread'
import ActionBar from '@/components/common/ActionBar.vue'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion'
import { useFileUpload } from '@/composables/useFileUpload'
import { useToast } from '@/composables/useToast'
import { buildMailQuote } from '@/utils/mailQuote'

const props = defineProps<{ messageId: number }>()
const emit = defineEmits<{
  back: []
  reply: [mode: 'reply' | 'replyAll' | 'forward']
  deleted: []
  updated: [msg: MailMessage]
}>()

const { t } = useI18n()
const detail = ref<MailMessageDetail | null>(null)
const loading = ref(false)
const error = ref('')
const busy = ref(false)
const allowRemoteImages = ref(false)

const attachments = computed(() => (detail.value?.attachments ?? []).filter((a: MailAttachment) => !a.inline))

// ── Action-row labels ─────────────────────────────────────────
// Same rule as the chat action bar (shared composable): labels are always in the
// DOM and shown only when the row proves it can hold them. Falling back to
// icons-only is better than overflowing a strip whose scrollbar is hidden.
const actionsRef = ref<{ measure: () => void } | null>(null)

defineExpose({ measureActionLabels: () => actionsRef.value?.measure() })

// ── Handing this email to the chat ─────────────────────────────
const quoteQuestion = useQuoteQuestion()
const { uploadAndAttach } = useFileUpload()
const toast = useToast()

/** Attachment currently being copied into the chat (0 = none). */
const attachingId = ref(0)

/**
 * Quote this email into a conversation.
 *
 * Same payload shape as every other entry point (label + sourceKind + locator),
 * so the card gets the mail icon and its "jump to source" works. The BODY is
 * inlined because the AI has no way to fetch an email from `mail: <id>` — the
 * id exists for the client's jump, and the label alone tells the model nothing
 * it can read.
 *
 * It opens the shared quote bar (composer mode) rather than staging directly:
 * the user almost always wants to say WHY they are quoting a mail, and the bar
 * is also where a different destination session is chosen.
 */
function quoteToChat(): void {
  const d = detail.value
  if (!d) return
  // One shared builder: dropping a mail row on the chat produces the same card.
  const quote = buildMailQuote(d, t('mail.list.noSubject'))
  quoteQuestion.openComposer({ quote, label: quote.filePath })
}

/**
 * Add one attachment to the chat input.
 *
 * The bytes are re-fetched and uploaded into the project's uploads directory
 * instead of referencing the mail store path: chat attachments are validated to
 * live inside the project, and `{data-dir}/mail/attachments/...` is not inside
 * it. Going through `uploadAndAttach` also means the normal size limit, progress
 * chip and dedupe apply, exactly as if the file had been dropped in.
 */
async function attachToChat(a: MailAttachment): Promise<void> {
  if (attachingId.value) return
  attachingId.value = a.id
  try {
    const res = await fetch(`/api/mail/messages/${a.messageId}/attachments/${a.id}`)
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const blob = await res.blob()
    const file = new File([blob], a.filename, { type: a.contentType || blob.type || 'application/octet-stream' })
    await uploadAndAttach([file])
  } catch (e) {
    appLog.w('MailReader', 'attach to chat failed', e)
    toast.show(t('mail.reader.attachFailed'), { type: 'error', icon: 'warning' })
  } finally {
    attachingId.value = 0
  }
}
const sentAtLabel = computed(() =>
  detail.value ? mailFullTimestamp(detail.value.sentAt) : '',
)
const showImagesBar = computed(
  () => !!detail.value?.bodyHtml && hasRemoteImages(detail.value.bodyHtml) && !allowRemoteImages.value,
)
const docSource = computed(() => {
  if (!detail.value?.bodyHtml) return ''
  return buildMailDocument(detail.value.bodyHtml, { allowRemoteImages: allowRemoteImages.value })
})

let generation = 0

async function load(): Promise<void> {
  const gen = ++generation
  loading.value = true
  error.value = ''
  allowRemoteImages.value = false
  try {
    const data = await apiGet<{ message: MailMessageDetail }>(`/api/mail/messages/${props.messageId}`)
    if (gen !== generation) return
    detail.value = data.message
    // Opening marks it read on the server. Tell the list as well: in the wide
    // layout the row stays on screen next to the reader, and patching it is
    // the only thing that drops its unread styling without a full reload.
    emit('updated', data.message)
    // Same for the dock badge and the folder counters.
    void refreshMailUnread()
  } catch (e) {
    if (gen !== generation) return
    appLog.w('MailReader', 'load failed', e)
    error.value = mailErrorMessage(e, t)
  } finally {
    if (gen === generation) loading.value = false
  }
}

watch(() => props.messageId, load, { immediate: true })

async function patchFlags(body: { seen?: boolean; flagged?: boolean }): Promise<void> {
  if (!detail.value) return
  busy.value = true
  try {
    const data = await apiPost<{ message: MailMessage }>(`/api/mail/messages/${detail.value.id}/flags`, body)
    detail.value = { ...detail.value, ...data.message }
    emit('updated', data.message)
    void refreshMailUnread()
  } catch (e) {
    appLog.w('MailReader', 'flag update failed', e)
  } finally {
    busy.value = false
  }
}

function toggleFlagged(): void {
  void patchFlags({ flagged: !detail.value?.flagged })
}

function toggleSeen(): void {
  void patchFlags({ seen: !detail.value?.seen })
}

async function remove(): Promise<void> {
  if (!detail.value) return
  busy.value = true
  try {
    await apiPost(`/api/mail/messages/${detail.value.id}/delete`, {})
    void refreshMailUnread()
    emit('deleted')
  } catch (e) {
    appLog.w('MailReader', 'delete failed', e)
    error.value = mailErrorMessage(e, t)
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.mail-reader {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}
.mail-reader__head {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px) var(--space-3, 6px);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
.mail-reader__title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-md, 14px);
  font-weight: 600;
  color: var(--text-primary);
}
.mail-reader__actions {
  padding: var(--space-1, 2px) var(--space-3, 6px);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
.mail-reader__spacer {
  flex: 1;
}
.mail-chip {
  flex: none;
  padding: calc(3 * var(--dp)) calc(8 * var(--dp));
  font-size: var(--font-size-xs, 11px);
  color: var(--text-secondary);
  background: var(--bg-tertiary);
  border: none;
  border-radius: var(--radius-sm, 4px);
  cursor: pointer;
  white-space: nowrap;
}
.mail-chip:hover:not(:disabled) {
  background: var(--bg-hover, rgba(128, 128, 128, 0.12));
}
.mail-chip:disabled {
  opacity: 0.5;
  cursor: default;
}
.mail-reader__meta {
  display: flex;
  flex-direction: column;
  gap: calc(1 * var(--dp));
  padding: var(--space-2, 4px) var(--space-3, 6px);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
.mail-reader__from {
  display: flex;
  align-items: baseline;
  gap: var(--space-2, 4px);
  min-width: 0;
}
.mail-reader__name {
  font-size: var(--font-size-sm, 12px);
  color: var(--text-primary);
  font-weight: 600;
}
.mail-reader__addr {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-hint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mail-reader__time {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-hint);
}
.mail-reader__recipients {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mail-reader__meta-label {
  color: var(--text-hint);
}
.mail-reader__body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.mail-reader__loading {
  display: flex;
  justify-content: center;
  padding: var(--space-6, 16px);
}
.mail-reader__images {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px) var(--space-3, 6px);
  font-size: var(--font-size-xs, 11px);
  color: var(--text-secondary);
  background: var(--bg-tertiary);
  flex-shrink: 0;
}
.mail-reader__frame {
  flex: 1;
  width: 100%;
  min-height: 0;
  border: none;
  background: #fff;
}
.mail-reader__text {
  flex: 1;
  margin: 0;
  padding: var(--space-3, 6px);
  overflow: auto;
  font-family: inherit;
  font-size: var(--font-size-sm, 12px);
  line-height: 1.65;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  color: var(--text-primary);
}
.mail-reader__attachments {
  display: flex;
  flex-direction: column;
  gap: var(--space-1, 2px);
  padding: var(--space-2, 4px) var(--space-3, 6px);
  border-top: 1px solid var(--border-color);
  max-height: calc(140 * var(--dp));
  overflow-y: auto;
  flex-shrink: 0;
}
.mail-reader__attachments-title {
  font-size: var(--font-size-xs, 11px);
  color: var(--text-hint);
}
.mail-attachment-row {
  display: flex;
  align-items: center;
  gap: var(--space-1, 2px);
  min-width: 0;
}
.mail-attachment {
  display: flex;
  flex: 1;
  min-width: 0;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: calc(3 * var(--dp)) calc(6 * var(--dp));
  font-size: var(--font-size-sm, 12px);
  color: var(--text-primary);
  text-decoration: none;
  border-radius: var(--radius-sm, 4px);
}
.mail-attachment:hover {
  background: var(--bg-hover, rgba(128, 128, 128, 0.12));
}
.mail-attachment__icon {
  width: calc(12 * var(--dp));
  height: calc(12 * var(--dp));
  color: var(--text-hint);
  flex: none;
}
.mail-attachment__name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mail-attachment__size {
  flex: none;
  font-size: var(--font-size-xs, 11px);
  color: var(--text-hint);
}
/* "Add to chat" sits at the row's right edge, outside the download link (a
   button inside an <a> is invalid HTML and would also trigger the download). */
.mail-attachment__chat {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  padding: 0;
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: transparent;
  color: var(--text-hint);
  cursor: pointer;
  transition: color var(--duration-base), background var(--duration-base);
}
.mail-attachment__chat:hover {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}
.mail-attachment__chat:disabled {
  cursor: default;
  opacity: var(--opacity-muted, 0.6);
}
/* Size, radius, colour and hover come from the shared ActionBar row rules
   (size="bar"); this block keeps only what is specific to the mail reader. */
.mail-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  /* Needed by the back/star buttons in the head row, which are NOT inside an
     ActionBar and so do not get the shared transparent background. */
  background: none;
  cursor: pointer;
  flex: none;
}
.mail-icon-btn:disabled {
  opacity: 0.5;
  cursor: default;
}


.mail-icon-btn :deep(svg) {
  width: calc(15 * var(--dp));
  height: calc(15 * var(--dp));
}
.mail-icon-btn :deep(svg.is-on) {
  color: var(--color-orange);
  fill: currentColor;
}
</style>
