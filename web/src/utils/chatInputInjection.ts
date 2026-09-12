// Cross-component chat-input injection channel.
//
// Some features (e.g. "analyze this GitHub issue/PR") need to place text into
// the chat input from a different tab, before the input component is mounted or
// while it is off-screen. The chat draft store is keyed by session id, which is
// not always known at the call site, so this module provides a simple
// module-level pending value that ChatInputBar drains when it becomes active.
import { ref } from 'vue'

/** Pending text to append to the chat input. Null when nothing is queued. */
export const pendingChatInput = ref<string | null>(null)

/** Queue text to be appended to the chat input. */
export function injectChatInput(text: string): void {
    if (!text) return
    pendingChatInput.value = text
}

/** Drain and return the pending text, clearing it. */
export function consumePendingChatInput(): string | null {
    const text = pendingChatInput.value
    pendingChatInput.value = null
    return text
}

/** Reset for tests. */
export function _resetChatInputInjectionForTesting(): void {
    pendingChatInput.value = null
}
