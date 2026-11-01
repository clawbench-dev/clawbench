<template>
  <div class="join-gate">
    <div class="join-gate__card">
      <div class="join-gate__title">{{ title || t('share.live.joinEnter') }}</div>
      <div class="join-gate__sub">{{ t('share.live.joinSubtitle') }}</div>

      <label class="join-gate__label" for="meeting-join-nick">{{ t('share.live.joinNick') }}</label>
      <input
        id="meeting-join-nick"
        v-model="nick"
        class="join-gate__input"
        type="text"
        :maxlength="MEETING_NICK_MAX"
        autocomplete="nickname"
        data-role="join-nick"
        :placeholder="t('share.live.joinNickPlaceholder')"
        @keydown.enter="submit"
      />
      <div class="join-gate__hint">{{ t('share.live.joinNickHint') }}</div>

      <button
        class="join-gate__btn"
        type="button"
        data-action="join-meeting"
        :disabled="!canJoin"
        @click="submit"
      >
        {{ t('share.live.joinEnter') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The viewer's nickname gate.
 *
 * It sits BEFORE the signalling socket: an empty name never opens a connection,
 * so it neither takes an audience slot nor costs the publisher upstream. The
 * name is remembered (same key the desktop "join" page writes), so a returning
 * viewer only has to press one button.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MEETING_NICK_MAX, isValidMeetingNick, readMeetingNick, saveMeetingNick } from './meetingNickname'

defineProps<{ title?: string }>()
const emit = defineEmits<{ join: [name: string] }>()

const { t } = useI18n()
const nick = ref(readMeetingNick())

const canJoin = computed(() => isValidMeetingNick(nick.value))

function submit(): void {
  const name = nick.value.trim()
  if (!isValidMeetingNick(name)) return
  saveMeetingNick(name)
  emit('join', name)
}
</script>

<style scoped>
.join-gate {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  padding: var(--space-8, 20px);
  background: var(--bg-secondary);
}

.join-gate__card {
  display: flex;
  flex-direction: column;
  gap: var(--space-4, 8px);
  width: min(calc(340 * var(--dp)), 100%);
  padding: var(--space-8, 20px);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg, 14px);
  background: var(--bg-elevated, #fff);
  box-shadow: var(--shadow-md);
}

.join-gate__title { font-size: var(--font-size-lg); font-weight: 600; color: var(--text-primary); }
.join-gate__sub { font-size: var(--font-size-xs); line-height: 1.6; color: var(--text-muted); }
.join-gate__label { margin-top: var(--space-2, 4px); font-size: var(--font-size-sm); font-weight: 500; color: var(--text-primary); }
.join-gate__input {
  width: 100%;
  height: calc(32 * var(--dp));
  padding: 0 var(--space-5, 10px);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm, 6px);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  font-family: inherit;
}
.join-gate__input:focus { outline: none; border-color: var(--accent-color); }
.join-gate__hint { font-size: var(--font-size-xs); color: var(--text-muted); }
.join-gate__btn {
  margin-top: var(--space-3, 6px);
  height: calc(30 * var(--dp));
  border: 1px solid var(--accent-color);
  border-radius: var(--radius-sm, 6px);
  background: var(--accent-color);
  color: #fff;
  font-size: var(--font-size-sm);
  cursor: pointer;
}
.join-gate__btn:hover:not(:disabled) { background: var(--accent-hover); }
.join-gate__btn:disabled { opacity: 0.45; cursor: not-allowed; }
</style>
