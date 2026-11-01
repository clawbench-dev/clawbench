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
        maxlength="24"
        data-role="meeting-nick"
        :placeholder="t('meeting.join.nickPlaceholder')"
        @input="rememberNick"
      />
      <div class="meeting-hint">{{ t('meeting.join.nickHint') }}</div>

      <div class="meeting-section">{{ t('meeting.join.link') }}</div>
      <input
        v-model="url"
        class="meeting-input"
        type="url"
        data-role="meeting-link"
        :placeholder="t('meeting.join.linkPlaceholder')"
        @keydown.enter="join"
      />
      <div class="meeting-hint">{{ t('meeting.join.linkHint') }}</div>
      <div v-if="error" class="meeting-error">{{ error }}</div>
    </div>

    <div class="meeting-footer">
      <button class="meeting-btn" type="button" @click="emit('back')">{{ t('common.cancel') }}</button>
      <button class="meeting-btn meeting-btn--primary" type="button" data-action="meeting-join-submit" :disabled="!canJoin" @click="join">
        {{ t('meeting.join.action') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 加入会议: a nickname (required) and a share link.
 *
 * The nickname is written to the SAME localStorage key the share page's gate
 * reads, so opening the link does not ask for it twice — the gate comes up
 * pre-filled and the user only confirms.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft } from 'lucide-vue-next'
import { getNative } from '@/utils/clawbenchNative'
import { useToast } from '@/composables/useToast'
import { MEETING_NICK_KEY } from '@/share/meetingNickname'

const emit = defineEmits<{ back: [] }>()

const { t } = useI18n()
const toast = useToast()
const nickname = ref(readNick())
const url = ref('')
const error = ref('')

const canJoin = computed(() => Boolean(nickname.value.trim()) && Boolean(url.value.trim()))

function readNick(): string {
  try {
    return localStorage.getItem(MEETING_NICK_KEY) ?? ''
  } catch {
    return ''
  }
}

function rememberNick(): void {
  error.value = ''
  try {
    localStorage.setItem(MEETING_NICK_KEY, nickname.value)
  } catch {
    /* private mode: the name simply is not remembered */
  }
}

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
  rememberNick()
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
  color: var(--text-muted); cursor: pointer;
}
.meeting-icon-btn:hover { background: var(--bg-hover); color: var(--text-primary); }

.meeting-body {
  flex: 1; min-height: 0; overflow-y: auto;
  display: flex; flex-direction: column; gap: var(--space-4);
  padding: var(--space-6) var(--space-7);
}
.meeting-section { font-size: var(--font-size-sm); font-weight: 600; color: var(--text-primary); }
.meeting-section:not(:first-child) {
  margin-top: var(--space-3); padding-top: var(--space-5);
  border-top: 1px solid var(--border-color);
}
.meeting-input {
  width: 100%; height: calc(32 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color); border-radius: var(--radius-sm);
  background: var(--bg-secondary); color: var(--text-primary);
  font-size: var(--font-size-sm); font-family: inherit;
}
.meeting-input:focus { outline: none; border-color: var(--accent-color); }
.meeting-hint { font-size: var(--font-size-xs); line-height: 1.6; color: var(--text-muted); }
.meeting-error {
  padding: var(--space-4) var(--space-5); border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red); font-size: var(--font-size-sm);
}
.meeting-footer {
  display: flex; justify-content: flex-end; gap: var(--space-3);
  padding: var(--space-4) var(--space-6);
  border-top: 1px solid var(--border-color); background: var(--bg-secondary); flex-shrink: 0;
}
.meeting-btn {
  display: inline-flex; align-items: center; gap: var(--space-2);
  height: calc(28 * var(--dp)); padding: 0 var(--space-6);
  border: 1px solid var(--border-color); border-radius: var(--radius-sm);
  background: var(--bg-primary); color: var(--text-primary);
  font-size: var(--font-size-sm); cursor: pointer;
}
.meeting-btn:hover:not(:disabled) { border-color: var(--accent-color); color: var(--accent-color); }
.meeting-btn--primary { border-color: var(--accent-color); background: var(--accent-color); color: #fff; }
.meeting-btn--primary:hover:not(:disabled) { background: var(--accent-hover); border-color: var(--accent-hover); color: #fff; }
.meeting-btn:disabled { opacity: 0.45; cursor: not-allowed; }
</style>
