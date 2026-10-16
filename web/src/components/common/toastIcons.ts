import {
    Ban,
    CircleCheck,
    CircleStop,
    ClipboardCopy,
    Eye,
    FileText,
    Folder,
    GitFork,
    Hand,
    Hourglass,
    Image,
    Inbox,
    Info,
    Link,
    MessageSquareText,
    Package,
    Paperclip,
    Pause,
    RefreshCw,
    Rewind,
    Save,
    Scissors,
    Shuffle,
    Sparkles,
    Star,
    Sunrise,
    Trash2,
    TriangleAlert,
    Volume2,
    VolumeX,
    CircleX,
} from 'lucide-vue-next'
import type { ToastIconKey, ToastType } from '@/composables/useToast'

/**
 * The single table mapping a semantic toast icon key to its native glyph.
 *
 * The toast icon is part of the component's built-in structure: every toast
 * renders one. A caller passes a semantic key (attach / link / copy / …) when
 * the action has a distinct meaning; when it omits one, `DEFAULT_BY_TYPE`
 * supplies the glyph for the toast type. Callers never pass a raw emoji or a
 * component — the vocabulary lives here.
 */
const TOAST_ICONS: Record<ToastIconKey, unknown> = {
    success: CircleCheck,
    error: CircleX,
    warning: TriangleAlert,
    info: Info,
    attach: Paperclip,
    link: Link,
    copy: ClipboardCopy,
    blocked: Ban,
    star: Star,
    fork: GitFork,
    shuffle: Shuffle,
    chat: MessageSquareText,
    cut: Scissors,
    stop: CircleStop,
    pause: Pause,
    pending: Hourglass,
    delete: Trash2,
    image: Image,
    sound: Volume2,
    mute: VolumeX,
    sync: RefreshCw,
    archive: Package,
    inbox: Inbox,
    file: FileText,
    folder: Folder,
    save: Save,
    hand: Hand,
    sunrise: Sunrise,
    create: Sparkles,
    rewind: Rewind,
    eye: Eye,
}

/**
 * Fallback glyph per toast type, so a toast that names no icon still shows one.
 * 'loading' never reaches this table — the component renders a spinner in its
 * place — but a value is kept for completeness.
 */
const DEFAULT_BY_TYPE: Record<ToastType, ToastIconKey> = {
    success: 'success',
    error: 'error',
    info: 'info',
    loading: 'info',
}

/** Resolve the glyph to render for a toast, honouring the type fallback. */
export function resolveToastIcon(icon: ToastIconKey | '', type: ToastType): unknown {
    return (icon ? TOAST_ICONS[icon] : undefined) ?? TOAST_ICONS[DEFAULT_BY_TYPE[type]] ?? Info
}
