import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useDialog } from '@/composables/useDialog'

describe('useDialog', () => {
  const { state, confirm, prompt, alert, resolve } = useDialog()

  beforeEach(() => {
    // Reset state between tests by resolving any pending dialog
    if (state.value.visible) {
      resolve(null)
    }
  })

  describe('confirm', () => {
    it('opens a confirm dialog and sets state', async () => {
      const promise = confirm('Are you sure?')

      expect(state.value.visible).toBe(true)
      expect(state.value.type).toBe('confirm')
      expect(state.value.message).toBe('Are you sure?')

      // Resolve to clean up
      resolve(true)
      await promise
    })

    it('passes options to state', async () => {
      const promise = confirm('Delete item?', {
        title: 'Confirm Delete',
        dangerous: true,
        confirmText: 'Delete',
        cancelText: 'Cancel',
      })

      expect(state.value.title).toBe('Confirm Delete')
      expect(state.value.dangerous).toBe(true)
      expect(state.value.confirmText).toBe('Delete')
      expect(state.value.cancelText).toBe('Cancel')

      resolve(false)
      await promise
    })
  })

  describe('prompt', () => {
    it('opens a prompt dialog and sets state', async () => {
      const promise = prompt('Enter your name:')

      expect(state.value.visible).toBe(true)
      expect(state.value.type).toBe('prompt')
      expect(state.value.message).toBe('Enter your name:')
      expect(state.value.value).toBe('')

      resolve(null)
      await promise
    })

    it('passes value and placeholder options', async () => {
      const promise = prompt('Enter value:', {
        value: 'default',
        placeholder: 'Type here...',
      })

      expect(state.value.value).toBe('default')
      expect(state.value.placeholder).toBe('Type here...')

      resolve(null)
      await promise
    })

    it('resolves with the entered value', async () => {
      const promise = prompt('Enter value:')

      resolve('my answer')
      const result = await promise
      expect(result).toBe('my answer')
    })

    it('resolves with null when cancelled', async () => {
      const promise = prompt('Enter value:')

      resolve(null)
      const result = await promise
      expect(result).toBeNull()
    })

    it('passes generate options to state', async () => {
      const onGenerate = vi.fn().mockResolvedValue('Generated')
      const promise = prompt('Rename:', { generateText: 'Auto-generate', onGenerate })

      expect(state.value.generateText).toBe('Auto-generate')
      expect(state.value.onGenerate).toBe(onGenerate)

      resolve(null)
      await promise
    })

    it('defaults generate fields to empty/null', async () => {
      const promise = prompt('Enter:')

      expect(state.value.generateText).toBe('')
      expect(state.value.onGenerate).toBeNull()

      resolve(null)
      await promise
    })

    it('does not carry generate options into the next dialog', async () => {
      const onGenerate = vi.fn().mockResolvedValue('Generated')
      const first = prompt('Rename:', { generateText: 'Auto-generate', onGenerate })
      resolve(null)
      await first

      const second = confirm('Sure?')
      expect(state.value.generateText).toBe('')
      expect(state.value.onGenerate).toBeNull()
      resolve(true)
      await second
    })
  })

  describe('alert', () => {
    it('opens an alert dialog and sets state', async () => {
      const promise = alert('Something happened!')

      expect(state.value.visible).toBe(true)
      expect(state.value.type).toBe('alert')
      expect(state.value.message).toBe('Something happened!')

      resolve(true)
      await promise
    })

    it('passes title option', async () => {
      const promise = alert('Error!', { title: 'Alert' })

      expect(state.value.title).toBe('Alert')

      resolve(true)
      await promise
    })
  })

  describe('resolve', () => {
    it('hides the dialog after resolve', async () => {
      const promise = confirm('Test?')

      expect(state.value.visible).toBe(true)
      resolve(true)
      await promise
      expect(state.value.visible).toBe(false)
    })

    it('resolves the promise with the given value', async () => {
      const promise = confirm('Test?')

      resolve(true)
      const result = await promise
      expect(result).toBe(true)
    })

    it('resolves with false when cancelled', async () => {
      const promise = confirm('Test?')

      resolve(false)
      const result = await promise
      expect(result).toBe(false)
    })

    it('resolves with null for prompt cancellation', async () => {
      const promise = prompt('Enter:')

      resolve(null)
      const result = await promise
      expect(result).toBeNull()
    })
  })

  describe('shared singleton state', () => {
    it('multiple useDialog() calls share the same state', () => {
      const instance1 = useDialog()
      const instance2 = useDialog()

      instance1.state.value.visible = true
      expect(instance2.state.value.visible).toBe(true)

      // Clean up
      instance1.resolve(null)
    })
  })

  describe('defaults', () => {
    it('confirm dialog has dangerous=false by default', async () => {
      const promise = confirm('Test?')
      expect(state.value.dangerous).toBe(false)
      resolve(false)
      await promise
    })

    it('prompt dialog has empty strings for optional fields', async () => {
      const promise = prompt('Test?')
      expect(state.value.placeholder).toBe('')
      expect(state.value.confirmText).toBe('')
      expect(state.value.cancelText).toBe('')
      resolve(null)
      await promise
    })
  })
})
