import { appLog } from '@/utils/appLog'
import { useNavigationContext } from './useNavigationContext'

export type BackReason = 'header' | 'android' | 'edge-swipe' | 'origin-bar' | 'close'

/** The step a back press resolves to. `null` means "nothing to handle". */
export type BackStep = 'overlay' | 'edit' | 'file' | 'origin' | 'close-overlay' | 'dir' | 'other'

export interface BackStateMachineHooks {
  /** Pure predicate — must not mutate state. */
  hasTopmostOverlay: () => boolean
  closeTopmostOverlay: () => boolean
  isEditing: () => boolean
  exitEdit: () => void
  canGoBackFile: () => boolean
  goBackFile: () => Promise<boolean>
  hasOrigin: () => boolean
  returnToOrigin: () => Promise<boolean>
  canGoBackDir: () => boolean
  goBackDir: () => Promise<boolean>
  /** Pure predicate — must not mutate state. */
  canCloseOverlay?: () => boolean
  closeOverlay?: () => boolean
  /** Pure predicate — must not mutate state. */
  canHandleOther?: () => boolean
  handleOther?: () => boolean
}

/**
 * Unified back navigation.
 *
 * The *decision* is deliberately separated from the *execution*: every hook
 * used to pick a step is a side-effect-free predicate, so `canHandleBack()`
 * can answer "will this press be consumed?" synchronously. Android needs that
 * answer in the same tick it dispatches `clawbench-back-press` (see
 * MainActivity#onBackPressed), while the navigation itself is async.
 */
export function useNavigationStateMachine(hooks: BackStateMachineHooks) {
  const navigation = useNavigationContext()

  function resolveStep(reason: BackReason): BackStep | null {
    // 1. 关闭最顶层 drawer、menu、search、TOC 或 context menu
    if (hooks.hasTopmostOverlay()) return 'overlay'

    // 2. 文件编辑状态下退出编辑；未保存修改继续使用现有确认逻辑
    if (hooks.isEditing()) return 'edit'

    // The user clicked the origin banner explicitly
    if (reason === 'origin-bar' && hooks.hasOrigin()) return 'origin'

    // The user clicked the close button. Closing is not "go back one step":
    // falling through to the file/dir steps would turn a dismiss into a
    // navigation (opening the previous file, or walking up a directory).
    if (reason === 'close') {
      if (hooks.hasOrigin()) return 'origin'
      if (hooks.canCloseOverlay?.()) return 'close-overlay'
      return null
    }

    // 3. fileNav.canGoBack 为 true 时恢复上一个文件
    if (hooks.canGoBackFile()) return 'file'

    // 4. navigation.hasOrigin 为 true 时恢复来源
    if (hooks.hasOrigin()) return 'origin'

    // Overlay is open but there is no file history and no origin
    if (hooks.canCloseOverlay?.()) return 'close-overlay'

    // 5. 当前 browse 目录不是根目录时返回父目录
    if (hooks.canGoBackDir()) return 'dir'

    // 6. Fallback for other registered page handlers (settings drill-down, tasks…)
    if (hooks.canHandleOther?.()) return 'other'

    return null
  }

  async function runStep(step: BackStep): Promise<boolean> {
    switch (step) {
      case 'overlay':
        return hooks.closeTopmostOverlay()
      case 'edit':
        hooks.exitEdit()
        return true
      case 'file':
        return await hooks.goBackFile()
      case 'origin':
        return await hooks.returnToOrigin()
      case 'close-overlay':
        return hooks.closeOverlay?.() ?? false
      case 'dir':
        return await hooks.goBackDir()
      case 'other':
        return hooks.handleOther?.() ?? false
    }
  }

  /** Synchronous probe — safe to call before deciding to await `navigateBack`. */
  function canHandleBack(reason: BackReason): boolean {
    // A navigation is already in flight; the press is consumed either way, so
    // the platform must not fall through to its own back behaviour.
    if (navigation.busy.value) return true
    return resolveStep(reason) !== null
  }

  async function navigateBack(reason: BackReason): Promise<boolean> {
    if (navigation.busy.value) {
      return true
    }
    const step = resolveStep(reason)
    appLog.i('Navigation', `navigateBack: reason=${reason} step=${step ?? 'none'}`)
    if (step === null) {
      // 无可处理状态时返回 false，交给 Android/浏览器默认退出行为
      return false
    }

    navigation.setBusy(true)
    try {
      return await runStep(step)
    } finally {
      navigation.setBusy(false)
    }
  }

  return {
    navigateBack,
    canHandleBack,
  }
}
