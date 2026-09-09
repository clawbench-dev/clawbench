import type { Ref } from 'vue'
import { useFileNavStack } from './useFileNavStack'
import { useNavigationContext, type NavigationOrigin } from './useNavigationContext'

/**
 * Suspended directory excursions.
 *
 * Module-level on purpose: the state this composable snapshots (the file
 * history stack and the navigation origin) are module-level singletons, so a
 * per-call stack would let two callers disagree about which visit is
 * suspended. There is exactly one file visit to suspend at a time.
 */
const suspended: Array<{
  files: ReturnType<ReturnType<typeof useFileNavStack>['snapshot']>
  navigation: ReturnType<ReturnType<typeof useNavigationContext>['snapshot']>
  browseSession: boolean
  /** Directory the file was being viewed from, so the excursion can unwind to
   *  it instead of stranding the user in the jumped-to directory. */
  directory: string | null
}> = []

/** @internal Reset all suspended excursions — for tests only */
export function _resetForTesting() {
  suspended.length = 0
}

/** A directory excursion suspends the file visit, including its own origin. */
export function useDirectoryReturn(browseSession: Ref<boolean>) {
  const files = useFileNavStack()
  const navigation = useNavigationContext()

  function enter(origin: NavigationOrigin, directory: string | null = null) {
    suspended.push({ files: files.snapshot(), navigation: navigation.snapshot(), browseSession: browseSession.value, directory })
    files.closeOverlay()
    navigation.consume()
    browseSession.value = false
    navigation.start(origin)
  }

  /** Pops the suspended visit. Returns null when there is nothing to restore. */
  function restore(): { directory: string | null } | null {
    const previous = suspended.pop()
    if (!previous) return null
    files.restore(previous.files)
    navigation.restore(previous.navigation)
    browseSession.value = previous.browseSession
    return { directory: previous.directory }
  }

  function clear() { suspended.length = 0 }

  /** True while a directory excursion is in progress (a file visit is suspended). */
  function pending() { return suspended.length > 0 }

  return { enter, restore, clear, pending }
}
