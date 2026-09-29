import { ref } from 'vue'

// Singleton toast state shared across the whole app
const visible = ref(false)
const message = ref('')
const icon = ref('')
const type = ref<'success' | 'error' | 'info' | 'loading'>('success')
const onClick = ref<(() => void) | null>(null)
let timer: ReturnType<typeof setTimeout> | null = null

export interface ToastOptions {
    icon?: string
    type?: 'success' | 'error' | 'info' | 'loading'
    duration?: number
    onClick?: () => void
}

/**
 * useToast()
 *
 * Show a toast notification. Calling show() while one is already visible
 * replaces the message and resets the timer.
 *
 * @param msg - Toast message text
 * @param opts - Toast options
 * @param opts.icon - Emoji or text shown before the message
 * @param opts.type - Toast type: 'success' | 'error' | 'info' | 'loading'
 *                    (default: 'success'). 'loading' renders a spinner in
 *                    place of the icon and is meant to be paired with
 *                    duration: 0, so it stays up until the action finishes
 *                    and a real result toast replaces it.
 * @param opts.duration - Auto-dismiss after N ms, 0 = manual only
 * @param opts.onClick - Callback fired when the toast is clicked
 */
function show(msg: string, { icon: ico = '', type: tp = 'success', duration = 4000, onClick: cb }: ToastOptions = {}): void {
    clearTimeout(timer!)
    message.value = msg
    icon.value = ico
    type.value = tp
    onClick.value = cb ?? null
    visible.value = true
    if (duration > 0) {
        timer = setTimeout(() => { visible.value = false }, duration)
    }
}

function dismiss(): void {
    clearTimeout(timer!)
    visible.value = false
}

export function useToast() {
    return {
        visible,
        message,
        icon,
        type,
        onClick,
        show,
        dismiss,
    }
}
