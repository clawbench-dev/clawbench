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

    <div class="mail-reader__actions">
      <button class="mail-chip" :disabled="busy" @click="emit('reply', 'reply')">{{ t('mail.reader.reply') }}</button>
      <button class="mail-chip" :disabled="busy" @click="emit('reply', 'replyAll')">{{ t('mail.reader.replyAll') }}</button>
      <button class="mail-chip" :disabled="busy" @click="emit('reply', 'forward')">{{ t('mail.reader.forward') }}</button>
      <span class="mail-reader__spacer" />
      <button class="mail-chip" :disabled="busy" @click="toggleSeen">
        {{ detail?.seen ? t('mail.reader.markUnread') : t('mail.reader.markRead') }}
      </button>
      <button class="mail-chip mail-chip--danger" :disabled="busy" @click="remove">
        {{ t('mail.reader.delete') }}
      </button>
    </div>

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
      <a
        v-for="a in attachments"
        :key="a.id"
        class="mail-attachment"
        :href="`/api/mail/messages/${a.messageId}/attachments/${a.id}`"
        :download="a.filename"
      >
        <Paperclip class="mail-attachment__icon" />
        <span class="mail-attachment__name">{{ a.filename }}</span>
        <span class="mail-attachment__size">{{ formatMailSize(a.size) }}</span>
      </a>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, MailOpen, Paperclip, Star, TriangleAlert } from 'lucide-vue-next'
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
    // Opening marks it read on the server; refresh the badge so the dock count
    // drops without waiting for the next poll.
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
  display: flex;
  align-items: center;
  gap: var(--space-1, 2px);
  padding: var(--space-1, 2px) var(--space-3, 6px);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  overflow-x: auto;
  scrollbar-width: none;
}
.mail-reader__actions::-webkit-scrollbar {
  display: none;
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
.mail-chip--danger {
  color: var(--color-red);
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
.mail-attachment {
  display: flex;
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
.mail-icon-btn:hover:not(:disabled) {
  background: var(--bg-hover, rgba(128, 128, 128, 0.12));
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
