import { computed, ref } from 'vue'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { readMeetingNick, resolveMeetingAttendeeName } from '@/share/meetingNickname'

/**
 * 参会称呼 — the name shared by both meeting entry points (创建 and 加入).
 *
 * Single source: `meeting.attendee_name`. It is deliberately SEPARATE from
 * `user.nickname` (how agents address you in a chat): editing one here never
 * changes the other, and the group-chat name is not even used as a fallback.
 * The fallback literal counts as "not configured", so the field starts empty
 * and the user must choose a real name — otherwise every unconfigured account
 * would enter as "User" and be indistinguishable in the audience list.
 *
 * Edits stay local until `persist()` runs on submit, so typing does not PATCH
 * the server on every keystroke. The device's remembered guest name is the
 * last fallback: a returning user should not have to retype it.
 */
export function useMeetingNickname() {
  const { serverConfig, setServerValue } = useSettingsConfig()

  const configured = computed(() => resolveMeetingAttendeeName(serverConfig.value?.meeting))
  const edited = ref<string | null>(null)
  const nickname = computed({
    get: () => edited.value ?? (configured.value || readMeetingNick()),
    set: (v: string) => {
      edited.value = v
    },
  })

  /** Save the typed name to meeting.attendee_name. No-op when unchanged or empty. */
  async function persist(): Promise<void> {
    const nick = nickname.value.trim()
    if (!nick || nick === configured.value) return
    await setServerValue('meeting.attendee_name', nick)
    edited.value = nick
  }

  return { nickname, persist }
}
