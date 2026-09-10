import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useNavigationContext } from '../useNavigationContext'
import { useNavigationStateMachine, type BackStateMachineHooks } from '../useNavigationStateMachine'

describe('navigationBackFlow', () => {
  const nav = useNavigationContext()

  beforeEach(() => {
    nav.resetForTesting()
    vi.clearAllMocks()
  })

  function createMockHooks(overrides: Partial<BackStateMachineHooks> = {}): BackStateMachineHooks {
    return {
      hasTopmostOverlay: vi.fn().mockReturnValue(false),
      closeTopmostOverlay: vi.fn().mockReturnValue(false),
      isEditing: vi.fn().mockReturnValue(false),
      exitEdit: vi.fn(),
      canGoBackFile: vi.fn().mockReturnValue(false),
      goBackFile: vi.fn().mockResolvedValue(true),
      hasOrigin: vi.fn().mockReturnValue(false),
      returnToOrigin: vi.fn().mockResolvedValue(true),
      canGoBackDir: vi.fn().mockReturnValue(false),
      goBackDir: vi.fn().mockResolvedValue(true),
      closeOverlay: vi.fn().mockReturnValue(false),
      handleOther: vi.fn().mockReturnValue(false),
      ...overrides,
    }
  }

  it('1. drawer 优先', async () => {
    const hooks = createMockHooks({
      hasTopmostOverlay: vi.fn().mockReturnValue(true),
      closeTopmostOverlay: vi.fn().mockReturnValue(true),
      isEditing: vi.fn().mockReturnValue(true),
      canGoBackFile: vi.fn().mockReturnValue(true),
      hasOrigin: vi.fn().mockReturnValue(true),
    })
    const { navigateBack } = useNavigationStateMachine(hooks)

    const handled = await navigateBack('android')
    expect(handled).toBe(true)
    expect(hooks.closeTopmostOverlay).toHaveBeenCalled()
    expect(hooks.exitEdit).not.toHaveBeenCalled()
    expect(hooks.goBackFile).not.toHaveBeenCalled()
    expect(hooks.returnToOrigin).not.toHaveBeenCalled()
  })

  it('2. 编辑优先', async () => {
    const hooks = createMockHooks({
      isEditing: vi.fn().mockReturnValue(true),
      canGoBackFile: vi.fn().mockReturnValue(true),
      hasOrigin: vi.fn().mockReturnValue(true),
    })
    const { navigateBack } = useNavigationStateMachine(hooks)

    const handled = await navigateBack('header')
    expect(handled).toBe(true)
    expect(hooks.hasTopmostOverlay).toHaveBeenCalled()
    expect(hooks.closeTopmostOverlay).not.toHaveBeenCalled()
    expect(hooks.exitEdit).toHaveBeenCalled()
    expect(hooks.goBackFile).not.toHaveBeenCalled()
    expect(hooks.returnToOrigin).not.toHaveBeenCalled()
  })

  it('3. 文件历史优先于来源', async () => {
    const hooks = createMockHooks({
      canGoBackFile: vi.fn().mockReturnValue(true),
      hasOrigin: vi.fn().mockReturnValue(true),
    })
    const { navigateBack } = useNavigationStateMachine(hooks)

    const handled = await navigateBack('header')
    expect(handled).toBe(true)
    expect(hooks.goBackFile).toHaveBeenCalled()
    expect(hooks.returnToOrigin).not.toHaveBeenCalled()
  })

  it('4. 来源优先于父目录', async () => {
    const hooks = createMockHooks({
      canGoBackFile: vi.fn().mockReturnValue(false),
      hasOrigin: vi.fn().mockReturnValue(true),
      canGoBackDir: vi.fn().mockReturnValue(true),
    })
    const { navigateBack } = useNavigationStateMachine(hooks)

    const handled = await navigateBack('android')
    expect(handled).toBe(true)
    expect(hooks.returnToOrigin).toHaveBeenCalled()
    expect(hooks.goBackDir).not.toHaveBeenCalled()
  })

  it('5. 根目录无处理时返回 false', async () => {
    const hooks = createMockHooks({
      canGoBackFile: vi.fn().mockReturnValue(false),
      hasOrigin: vi.fn().mockReturnValue(false),
      canGoBackDir: vi.fn().mockReturnValue(false),
      handleOther: vi.fn().mockReturnValue(false),
    })
    const { navigateBack } = useNavigationStateMachine(hooks)

    const handled = await navigateBack('android')
    expect(handled).toBe(false)
  })

  it('6. busy 时不重复执行', async () => {
    nav.setBusy(true)
    const hooks = createMockHooks({
      hasOrigin: vi.fn().mockReturnValue(true),
    })
    const { navigateBack } = useNavigationStateMachine(hooks)

    const handled = await navigateBack('header')
    expect(handled).toBe(true)
    expect(hooks.returnToOrigin).not.toHaveBeenCalled()

    // When not busy, it executes and resets busy in finally
    nav.setBusy(false)
    const handled2 = await navigateBack('header')
    expect(handled2).toBe(true)
    expect(hooks.returnToOrigin).toHaveBeenCalled()
    expect(nav.busy.value).toBe(false)
  })

  describe('close reason (dismiss, not "go back one step")', () => {
    it('returns to the origin when a jump origin is pending', async () => {
      const hooks = createMockHooks({
        hasOrigin: vi.fn().mockReturnValue(true),
        returnToOrigin: vi.fn().mockResolvedValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)

      expect(await navigateBack('close')).toBe(true)
      expect(hooks.returnToOrigin).toHaveBeenCalledTimes(1)
    })

    it('closes the overlay when there is no origin to return to', async () => {
      const hooks = createMockHooks({
        hasOrigin: vi.fn().mockReturnValue(false),
        canCloseOverlay: vi.fn().mockReturnValue(true),
        closeOverlay: vi.fn().mockReturnValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)

      expect(await navigateBack('close')).toBe(true)
      expect(hooks.closeOverlay).toHaveBeenCalledTimes(1)
    })

    it('never falls through to the file/directory steps', async () => {
      // Regression guard: without an explicit stop inside the close branch, a
      // dismiss kept walking the priority ladder and turned into "previous
      // file" / "parent directory" — i.e. a navigation the user never asked for.
      const hooks = createMockHooks({
        hasOrigin: vi.fn().mockReturnValue(false),
        canCloseOverlay: vi.fn().mockReturnValue(false),
        canGoBackFile: vi.fn().mockReturnValue(true),
        canGoBackDir: vi.fn().mockReturnValue(true),
        canHandleOther: vi.fn().mockReturnValue(true),
      })
      const { navigateBack, canHandleBack } = useNavigationStateMachine(hooks)

      expect(canHandleBack('close')).toBe(false)
      expect(await navigateBack('close')).toBe(false)
      expect(hooks.goBackFile).not.toHaveBeenCalled()
      expect(hooks.goBackDir).not.toHaveBeenCalled()
      expect(hooks.handleOther).not.toHaveBeenCalled()
    })
  })

  describe('Mobile Acceptance Criteria M1-M12', () => {
    it('M1: Chat -> File -> header back returns to Chat', async () => {
      nav.start({ surface: 'chat', tab: 'chat', label: 'Back to Chat' })
      const hooks = createMockHooks({
        canGoBackFile: vi.fn().mockReturnValue(false),
        hasOrigin: vi.fn().mockReturnValue(true),
        returnToOrigin: vi.fn().mockImplementation(async () => {
          nav.consume()
          return true
        }),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('header')
      expect(handled).toBe(true)
      expect(hooks.returnToOrigin).toHaveBeenCalled()
      expect(nav.hasOrigin.value).toBe(false)
    })

    it('M2: Chat -> File -> Android back matches header back and prevents exit', async () => {
      nav.start({ surface: 'chat', tab: 'chat', label: 'Back to Chat' })
      const hooks = createMockHooks({
        canGoBackFile: vi.fn().mockReturnValue(false),
        hasOrigin: vi.fn().mockReturnValue(true),
        returnToOrigin: vi.fn().mockImplementation(async () => {
          nav.consume()
          return true
        }),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('android')
      expect(handled).toBe(true)
      expect(hooks.returnToOrigin).toHaveBeenCalled()
    })

    it('M4: Markdown -> Dir -> back restores markdown, viewMode and scrollTop', async () => {
      nav.start({
        surface: 'file',
        tab: 'view',
        filePath: 'doc.md',
        viewMode: 'rendered',
        scrollTop: 250,
        label: 'Back to doc.md',
      })
      expect(nav.origin.value?.filePath).toBe('doc.md')
      expect(nav.origin.value?.viewMode).toBe('rendered')
      expect(nav.origin.value?.scrollTop).toBe(250)

      const hooks = createMockHooks({
        canGoBackFile: vi.fn().mockReturnValue(false),
        hasOrigin: vi.fn().mockReturnValue(true),
        returnToOrigin: vi.fn().mockImplementation(async () => {
          nav.consume()
          return true
        }),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('header')
      expect(handled).toBe(true)
      expect(hooks.returnToOrigin).toHaveBeenCalled()
      expect(nav.hasOrigin.value).toBe(false)
    })

    it('M5: Markdown -> Dir -> File -> consecutive backs go File -> Markdown -> Tab', async () => {
      nav.start({ surface: 'file', tab: 'chat', filePath: 'readme.md' })
      let inSubFile = true
      const hooks = createMockHooks({
        canGoBackFile: vi.fn().mockImplementation(() => inSubFile),
        goBackFile: vi.fn().mockImplementation(async () => {
          inSubFile = false
          return true
        }),
        hasOrigin: vi.fn().mockReturnValue(true),
        returnToOrigin: vi.fn().mockImplementation(async () => {
          nav.consume()
          return true
        }),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)

      // First back: from sub-file back to previous file / location
      const first = await navigateBack('header')
      expect(first).toBe(true)
      expect(hooks.goBackFile).toHaveBeenCalledTimes(1)
      expect(hooks.returnToOrigin).not.toHaveBeenCalled()

      // Second back: from file back to origin
      const second = await navigateBack('header')
      expect(second).toBe(true)
      expect(hooks.returnToOrigin).toHaveBeenCalledTimes(1)
      expect(nav.hasOrigin.value).toBe(false)
    })

    it('M6: File A -> File B -> Web edge swipe returns to A', async () => {
      const hooks = createMockHooks({
        canGoBackFile: vi.fn().mockReturnValue(true),
        goBackFile: vi.fn().mockResolvedValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('android')
      expect(handled).toBe(true)
      expect(hooks.goBackFile).toHaveBeenCalledTimes(1)
    })

    it('M7: browse -> subdir -> back returns to parent directory without origin bar', async () => {
      const hooks = createMockHooks({
        canGoBackFile: vi.fn().mockReturnValue(false),
        hasOrigin: vi.fn().mockReturnValue(false),
        canGoBackDir: vi.fn().mockReturnValue(true),
        goBackDir: vi.fn().mockResolvedValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('android')
      expect(handled).toBe(true)
      expect(hooks.goBackDir).toHaveBeenCalledTimes(1)
      expect(hooks.returnToOrigin).not.toHaveBeenCalled()
    })

    it('M8: Open drawer -> back closes only drawer', async () => {
      const hooks = createMockHooks({
        hasTopmostOverlay: vi.fn().mockReturnValue(true),
        closeTopmostOverlay: vi.fn().mockReturnValue(true),
        hasOrigin: vi.fn().mockReturnValue(true),
        canGoBackDir: vi.fn().mockReturnValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('android')
      expect(handled).toBe(true)
      expect(hooks.closeTopmostOverlay).toHaveBeenCalledTimes(1)
      expect(hooks.returnToOrigin).not.toHaveBeenCalled()
      expect(hooks.goBackDir).not.toHaveBeenCalled()
    })

    it('M9: Unsaved editing -> back triggers exit confirmation without navigating', async () => {
      const hooks = createMockHooks({
        isEditing: vi.fn().mockReturnValue(true),
        exitEdit: vi.fn(),
        hasOrigin: vi.fn().mockReturnValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('android')
      expect(handled).toBe(true)
      expect(hooks.exitEdit).toHaveBeenCalledTimes(1)
      expect(hooks.returnToOrigin).not.toHaveBeenCalled()
    })
  })

  describe('browse search / multi-select transient layers', () => {
    it('search open in a subdir exits search instead of going up a directory', async () => {
      const hooks = createMockHooks({
        canExitSearch: vi.fn().mockReturnValue(true),
        exitSearch: vi.fn(),
        canGoBackDir: vi.fn().mockReturnValue(true),
        goBackDir: vi.fn().mockResolvedValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('android')
      expect(handled).toBe(true)
      expect(hooks.exitSearch).toHaveBeenCalledTimes(1)
      expect(hooks.goBackDir).not.toHaveBeenCalled()
    })

    it('search open with a pending origin exits search before returning to the origin', async () => {
      const hooks = createMockHooks({
        canExitSearch: vi.fn().mockReturnValue(true),
        exitSearch: vi.fn(),
        hasOrigin: vi.fn().mockReturnValue(true),
        returnToOrigin: vi.fn().mockResolvedValue(true),
        canGoBackDir: vi.fn().mockReturnValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('edge-swipe')
      expect(handled).toBe(true)
      expect(hooks.exitSearch).toHaveBeenCalledTimes(1)
      expect(hooks.returnToOrigin).not.toHaveBeenCalled()
      expect(hooks.goBackDir).not.toHaveBeenCalled()
    })

    it('search dismissed by one back press; a second back then goes up a directory', async () => {
      const searchActive = { value: true }
      const hooks = createMockHooks({
        canExitSearch: vi.fn().mockImplementation(() => searchActive.value),
        exitSearch: vi.fn().mockImplementation(() => { searchActive.value = false }),
        canGoBackDir: vi.fn().mockReturnValue(true),
        goBackDir: vi.fn().mockResolvedValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      expect(await navigateBack('android')).toBe(true)
      expect(hooks.exitSearch).toHaveBeenCalledTimes(1)
      expect(hooks.goBackDir).not.toHaveBeenCalled()
      expect(await navigateBack('android')).toBe(true)
      expect(hooks.exitSearch).toHaveBeenCalledTimes(1)
      expect(hooks.goBackDir).toHaveBeenCalledTimes(1)
    })

    it('multi-select open exits multi-select instead of going up a directory', async () => {
      const hooks = createMockHooks({
        canExitMultiSelect: vi.fn().mockReturnValue(true),
        exitMultiSelect: vi.fn(),
        canGoBackDir: vi.fn().mockReturnValue(true),
        goBackDir: vi.fn().mockResolvedValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('android')
      expect(handled).toBe(true)
      expect(hooks.exitMultiSelect).toHaveBeenCalledTimes(1)
      expect(hooks.goBackDir).not.toHaveBeenCalled()
    })

    it('search takes precedence over multi-select when both predicates report true', async () => {
      const hooks = createMockHooks({
        canExitSearch: vi.fn().mockReturnValue(true),
        exitSearch: vi.fn(),
        canExitMultiSelect: vi.fn().mockReturnValue(true),
        exitMultiSelect: vi.fn(),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('header')
      expect(handled).toBe(true)
      expect(hooks.exitSearch).toHaveBeenCalledTimes(1)
      expect(hooks.exitMultiSelect).not.toHaveBeenCalled()
    })
  })

  describe('jump origin spent directly on back', () => {
    it('back returns to the jump origin without popping in-panel drill levels', async () => {
      nav.start({ surface: 'chat', tab: 'chat', label: 'Back to Chat' })
      const hooks = createMockHooks({
        canGoBackFile: vi.fn().mockReturnValue(false),
        hasOrigin: vi.fn().mockReturnValue(true),
        returnToOrigin: vi.fn().mockImplementation(async () => {
          nav.consume()
          return true
        }),
        canHandleOther: vi.fn().mockReturnValue(true),
        handleOther: vi.fn().mockReturnValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('android')
      expect(handled).toBe(true)
      expect(hooks.returnToOrigin).toHaveBeenCalledTimes(1)
      expect(hooks.handleOther).not.toHaveBeenCalled()
      expect(nav.hasOrigin.value).toBe(false)
    })

    it('with no origin pending the in-panel drill handler still consumes back', async () => {
      const hooks = createMockHooks({
        hasOrigin: vi.fn().mockReturnValue(false),
        canGoBackDir: vi.fn().mockReturnValue(false),
        canHandleOther: vi.fn().mockReturnValue(true),
        handleOther: vi.fn().mockReturnValue(true),
      })
      const { navigateBack } = useNavigationStateMachine(hooks)
      const handled = await navigateBack('android')
      expect(handled).toBe(true)
      expect(hooks.handleOther).toHaveBeenCalledTimes(1)
    })
  })
})
