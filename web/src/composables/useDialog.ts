import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

const TAG = 'useDialog'

// Singleton dialog state — shared across the whole app

interface DialogState {
  visible: boolean
  type: 'confirm' | 'prompt' | 'alert'
  title: string
  message: string
  value: string
  placeholder: string
  confirmText: string
  cancelText: string
  dangerous: boolean
  extraText: string
  extraPrimedText: string
  onExtraAction: (() => void) | null
  // Auto-generate affordance for prompt dialogs: the button label, and the
  // async producer that fills the input. Absent when the caller has nothing to
  // generate (e.g. the summary model is not configured).
  generateText: string
  onGenerate: (() => Promise<string | null>) | null
  /**
   * Render the prompt as a one-line text field instead of the default 3-row
   * textarea. For values that are never multi-line (a line number, a name),
   * the textarea adds a resize-free box the height of three lines and invites
   * a newline the caller would have to strip.
   */
  singleLine: boolean
  /**
   * Let Enter insert a newline in a prompt's textarea instead of confirming.
   *
   * Off by default because the prompt is used for one-line-ish values (a name,
   * a line number, a short note) where Enter-to-submit is the expected
   * behaviour. On for values that are genuinely multi-line — editing a queued
   * message's text — where the default made a newline impossible to type.
   * Ctrl/Cmd+Enter still confirms.
   */
  enterInsertsNewline: boolean
  resolve: ((v: string | boolean | null) => void) | null
}

const state = ref<DialogState>({
  visible: false,
  type: 'confirm',
  title: '',
  message: '',
  value: '',
  placeholder: '',
  confirmText: '',
  cancelText: '',
  dangerous: false,
  extraText: '',
  extraPrimedText: '',
  onExtraAction: null,
  generateText: '',
  onGenerate: null,
  singleLine: false,
  enterInsertsNewline: false,
  resolve: null,
})

function open(type: DialogState['type'], message: string, opts: {
  title?: string
  value?: string
  placeholder?: string
  confirmText?: string
  cancelText?: string
  dangerous?: boolean
  extraText?: string
  extraPrimedText?: string
  onExtraAction?: () => void
  generateText?: string
  onGenerate?: () => Promise<string | null>
  singleLine?: boolean
  enterInsertsNewline?: boolean
} = {}): Promise<string | boolean | null> {
  return new Promise(resolve => {
    // Resolve the previous dialog as cancelled before replacing it,
    // so its awaiter doesn't hang forever.
    const prev = state.value
    if (prev.visible && prev.resolve) {
      appLog.w(
        TAG,
        'resolve overwritten while previous dialog is still open!',
        '\n  Previous:', prev.type, prev.message?.slice(0, 60),
        '\n  New:     ', type, message?.slice(0, 60),
      )
      prev.resolve(null)
    }
    state.value = {
      visible: true,
      type,
      title: opts.title || '',
      message,
      value: opts.value ?? '',
      placeholder: opts.placeholder || '',
      confirmText: opts.confirmText || '',
      cancelText: opts.cancelText || '',
      dangerous: opts.dangerous ?? false,
      extraText: opts.extraText || '',
      extraPrimedText: opts.extraPrimedText || '',
      onExtraAction: opts.onExtraAction ?? null,
      generateText: opts.generateText || '',
      onGenerate: opts.onGenerate ?? null,
      singleLine: opts.singleLine ?? false,
      enterInsertsNewline: opts.enterInsertsNewline ?? false,
      resolve,
    }
  })
}

function confirm(message: string, opts?: Parameters<typeof open>[2]): Promise<boolean> {
  return open('confirm', message, opts) as Promise<boolean>
}

function prompt(message: string, opts?: { value?: string; placeholder?: string; title?: string; confirmText?: string; cancelText?: string; dangerous?: boolean; generateText?: string; onGenerate?: () => Promise<string | null>; singleLine?: boolean; enterInsertsNewline?: boolean }): Promise<string | null> {
  return open('prompt', message, opts) as Promise<string | null>
}

function alert(message: string, opts?: { title?: string; confirmText?: string }): Promise<boolean> {
  return open('alert', message, opts) as Promise<boolean>
}

function resolve(result: string | boolean | null) {
  if (!state.value.resolve) {
    appLog.w(TAG, 'resolve() called but no pending dialog')
  }
  state.value.resolve?.(result)
  state.value.visible = false
}

export function useDialog() {
  return { state, confirm, prompt, alert, resolve }
}
