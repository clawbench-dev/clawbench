import { ref } from 'vue'

// Singleton toast state shared across the whole app
const visible = ref(false)
const message = ref('')
const icon = ref<ToastIconKey | ''>('')
const type = ref<ToastType>('success')
const onClick = ref<(() => void) | null>(null)
let timer: ReturnType<typeof setTimeout> | null = null
// Auto-dismiss bookkeeping. `deadline` is the absolute time the toast would
// hide; `remaining` is what is left after a hover pause. `autoDismiss` is false
// for duration: 0 (manual-only) toasts, which must survive a mouseleave.
let autoDismiss = false
let deadline = 0
let remaining = 0

export type ToastType = 'success' | 'error' | 'info' | 'loading'

/**
 * Semantic icon key. Every toast renders a native (lucide) icon: the key picks
 * the glyph, and an absent key falls back to the default for the toast type.
 * The key→glyph table lives in components/common/toastIcons.ts so this
 * composable stays free of any icon-library import.
 */
export type ToastIconKey =
    | 'success'
    | 'error'
    | 'warning'
    | 'info'
    | 'attach'
    | 'link'
    | 'copy'
    | 'blocked'
    | 'star'
    | 'fork'
    | 'shuffle'
    | 'chat'
    | 'cut'
    | 'stop'
    | 'pause'
    | 'pending'
    | 'delete'
    | 'image'
    | 'sound'
    | 'mute'
    | 'sync'
    | 'archive'
    | 'inbox'
    | 'file'
    | 'folder'
    | 'save'
    | 'hand'
    | 'sunrise'
    | 'create'
    | 'rewind'
    | 'eye'

export interface ToastOptions {
    icon?: ToastIconKey | ''
    type?: ToastType
    duration?: number
    onClick?: () => void
}

function hide(): void {
    timer = null
    visible.value = false
}

/** Arm the auto-dismiss timer for `ms` from now. */
function arm(ms: number): void {
    remaining = ms
    deadline = Date.now() + ms
    clearTimeout(timer!)
    timer = setTimeout(hide, ms)
}

/**
 * useToast()
 *
 * Show a toast notification. Calling show() while one is already visible
 * replaces the message and resets the timer.
 *
 * @param msg - Toast message text
 * @param opts - Toast options
 * @param opts.icon - Semantic icon key (see ToastIconKey). Omit to use the
 *                    default glyph for the toast type — every toast has an icon.
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
    timer = null
    message.value = msg
    icon.value = ico
    type.value = tp
    onClick.value = cb ?? null
    visible.value = true
    autoDismiss = duration > 0
    if (autoDismiss) arm(duration)
}

function dismiss(): void {
    clearTimeout(timer!)
    timer = null
    visible.value = false
}

/**
 * Suspend the auto-dismiss countdown (pointer over the toast) so the user has
 * time to read and copy the message. No-op for manual-only (duration: 0) toasts
 * and for a toast that is already paused or hidden.
 */
function pause(): void {
    if (!autoDismiss || !visible.value || timer === null) return
    clearTimeout(timer)
    timer = null
    remaining = Math.max(0, deadline - Date.now())
}

/** Resume a paused countdown with the time that was left. */
function resume(): void {
    if (!autoDismiss || timer !== null || !visible.value) return
    arm(remaining)
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
        pause,
        resume,
    }
}
