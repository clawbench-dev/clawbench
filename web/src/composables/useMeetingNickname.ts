import { computed, ref } from 'vue'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { readMeetingNick } from '@/share/meetingNickname'
import { resolveUserNickname } from '@/utils/userNickname'

/**
 * The nickname shared by both meeting entry points (创建 and 加入).
 *
 * Single source: the account's `user.nickname`. The fallback literal counts as
 * "not configured", so the field starts empty and the user must choose a real
 * name — otherwise every unconfigured account would enter as "User" and be
 * indistinguishable in the publisher's audience list.
 *
 * Edits stay local until `persist()` runs on submit, so typing does not PATCH
 * the account on every keystroke. The device's remembered guest name is the
 * last fallback: a returning user should not have to retype it.
 */
export function useMeetingNickname() {
  const { serverConfig, setServerValue } = useSettingsConfig()

  const configured = computed(() => resolveUserNickname(serverConfig.value?.user))
  const edited = ref<string | null>(null)
  const nickname = computed({
    get: () => edited.value ?? (configured.value || readMeetingNick()),
    set: (v: string) => {
      edited.value = v
    },
  })

  /** Save the typed name to the account. No-op when unchanged or empty. */
  async function persist(): Promise<void> {
    const nick = nickname.value.trim()
    if (!nick || nick === configured.value) return
    await setServerValue('user.nickname', nick)
    edited.value = nick
  }

  return { nickname, persist }
}
