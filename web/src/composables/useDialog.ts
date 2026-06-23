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
  resolve: null,
})

function open(type: DialogState['type'], message: string, opts: {
  title?: string
  value?: string
  placeholder?: string
  confirmText?: string
  cancelText?: string
  dangerous?: boolean
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
      appLog.d(TAG, 'overwrite call stack:', new Error().stack)
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
      resolve,
    }
  })
}

function confirm(message: string, opts?: Parameters<typeof open>[2]): Promise<boolean> {
  return open('confirm', message, opts) as Promise<boolean>
}

function prompt(message: string, opts?: { value?: string; placeholder?: string; title?: string; confirmText?: string; cancelText?: string }): Promise<string | null> {
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
