<template>
  <div class="meeting-panel">
    <div class="meeting-header">
      <button class="meeting-icon-btn" type="button" :title="t('common.back')" @click="emit('back')">
        <ArrowLeft :size="14" />
      </button>
      <span class="meeting-header__title">{{ t('meeting.join.title') }}</span>
      <span class="meeting-header__spacer" />
    </div>

    <div class="meeting-body">
      <div class="meeting-section">{{ t('meeting.join.nick') }}</div>
      <input
        v-model="nickname"
        class="meeting-input"
        type="text"
        :maxlength="MEETING_NICK_MAX"
        data-role="meeting-nick"
        :placeholder="t('meeting.join.nickPlaceholder')"
      />

      <div class="meeting-section">{{ t('meeting.join.link') }}</div>
      <input
        v-model="url"
        class="meeting-input"
        type="url"
        data-role="meeting-link"
        :placeholder="t('meeting.join.linkPlaceholder')"
        @keydown.enter="join"
      />
      <div v-if="error" class="meeting-error">{{ error }}</div>
    </div>

    <div class="meeting-footer">
      <button class="fbtn" type="button" @click="emit('back')">{{ t('common.cancel') }}</button>
      <button class="fbtn fbtn-primary" type="button" data-action="meeting-join-submit" :disabled="!canJoin" @click="join">
        {{ t('meeting.join.action') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 加入会议: the user's nickname (required) and a share link.
 *
 * The nickname is NOT a meeting-local field: it is the account identity
 * (user.nickname), the same value group chat uses. Reading it from the server
 * config and writing it back through the settings API is what makes "one
 * nickname" true — the page used to keep its own localStorage copy, which
 * drifted from the group-chat name and was lost on another device.
 *
 * The value is still mirrored into localStorage before the link opens, so the
 * share page's gate (which is unauthenticated and cannot read the server
 * config) comes up pre-filled on the same device/origin.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft } from 'lucide-vue-next'
import { getNative } from '@/utils/clawbenchNative'
import { useToast } from '@/composables/useToast'
import { useMeetingNickname } from '@/composables/useMeetingNickname'
import { MEETING_NICK_MAX, saveMeetingNick } from '@/share/meetingNickname'
import '@/assets/modal-footer-btn.css'
// Same section / field / hint primitives as 创建会议 and the running meeting.
import '@/assets/meeting-form.css'

const emit = defineEmits<{ back: [] }>()

const { t } = useI18n()
const toast = useToast()
const { nickname, persist } = useMeetingNickname()
const url = ref('')
const error = ref('')

const canJoin = computed(() => Boolean(nickname.value.trim()) && Boolean(url.value.trim()))

async function join(): Promise<void> {
  const nick = nickname.value.trim()
  const link = url.value.trim()
  if (!nick) {
    error.value = t('meeting.join.nickRequired')
    return
  }
  if (!/^https?:\/\//i.test(link)) {
    error.value = t('meeting.join.invalidLink')
    return
  }
  error.value = ''
  // Editing the name here edits the same setting group chat uses.
  try {
    await persist()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    return
  }
  saveMeetingNick(nick)
  const bridge = getNative()
  if (bridge?.openShareLink) {
    const ok = await bridge.openShareLink(link)
    if (!ok) error.value = t('liveShare.joinInvalid')
    else toast.show(t('meeting.join.opened'), { type: 'success', icon: 'link' })
    return
  }
  // A browser has no second-window bridge: a new tab is the equivalent.
  window.open(link, '_blank', 'noopener')
  toast.show(t('meeting.join.opened'), { type: 'success', icon: 'link' })
}
</script>

<style scoped>
.meeting-panel { display: flex; flex-direction: column; flex: 1; min-height: 0; overflow: hidden; background: var(--bg-primary); }
.meeting-header {
  display: flex; align-items: center; gap: var(--space-3);
  height: var(--header-height); padding: 0 var(--space-5) 0 var(--space-6);
  border-bottom: 1px solid var(--border-color); flex-shrink: 0;
}
.meeting-header__title { font-size: var(--font-size-md); font-weight: 600; color: var(--text-primary); }
.meeting-header__spacer { flex: 1; }
.meeting-icon-btn {
  display: inline-flex; align-items: center; justify-content: center;
  width: calc(26 * var(--dp)); height: calc(26 * var(--dp));
  border: none; border-radius: var(--radius-sm); background: transparent;
  color: var(--text-secondary); cursor: pointer;
}
@media (hover: hover) {
  .meeting-icon-btn:hover { background: color-mix(in srgb, var(--text-primary) 12%, transparent); color: var(--text-primary); }
}

.meeting-body {
  flex: 1; min-height: 0; overflow-y: auto;
  display: flex; flex-direction: column; gap: var(--space-4);
  padding: var(--space-6) var(--space-7);
}
/* .meeting-section / .meeting-input / .meeting-hint live in
   assets/meeting-form.css, shared with 创建会议 and the running meeting. */
.meeting-error {
  padding: var(--space-4) var(--space-5); border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red); font-size: var(--font-size-sm);
}
.meeting-footer {
  display: flex; flex-wrap: nowrap; justify-content: flex-end;
  justify-content: safe flex-end; gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-color); background: var(--bg-primary); flex-shrink: 0;
  overflow-x: auto; overflow-y: hidden;
}
</style>
