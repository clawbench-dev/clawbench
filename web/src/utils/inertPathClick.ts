import { openInertPathPicker, deriveSearchQuery } from '@/composables/useInertPathPicker'
import { readInertLineTarget } from '@/composables/useFilePathAnnotation'
import { isShareMode } from '@/share/shareMode'
import type { NavigationSurface } from '@/composables/useNavigationContext'

/**
 * Document-level click layer that turns a verified-missing path chip into a
 * filename-search entry point.
 *
 * Why one global listener instead of per-site guards: a path annotation can be
 * rendered by a dozen hosts (chat, task prompt/execution, forge detail, markdown
 * preview, tool-detail drawer, file-diff drawer, table-row modal). Ten of them
 * exclude inert paths with an explicit `:not([data-path-type="none"])`, but two
 * (`ToolDetailDrawer`, `FileDiffsDrawer`) match a bare `.chat-file-path` and
 * would emit a `file-open` for a file that does not exist. Patching each host
 * would repeat the dragClickGuard mistake (a per-row check that reached 4 of ~50
 * rows); one capture-phase listener covers every present and future host.
 *
 * CAPTURE phase is load-bearing: it runs before the host's own handler, so
 * `stopPropagation()` prevents the ungated drawers from acting on the click at
 * all. A bubble-phase listener would run after them — too late.
 *
 * Only a VERIFIED-MISSING path is actionable. A glob pattern (`src/*.go`) also
 * carries `chat-file-path-inert` but never gets `data-file-path` (markInertLink
 * fires before the annotation class is added), so the query derivation below is
 * what excludes it — a pattern has no filename to search for. That is the ONLY
 * exclusion: the selector deliberately does not also require the attribute, as
 * two overlapping guards would leave neither independently pinned by a test.
 */
export function installInertPathClick(): () => void {
    function handler(e: MouseEvent) {
        // A drag that selected text inside the chip still ends in a click on it.
        // dragClickGuard (installed BEFORE this layer — see App.vue) suppresses
        // those with preventDefault; honoring the flag keeps a text selection
        // from opening the search panel. This ordering coupling is asserted by
        // inertPathClickWiring.test.ts.
        if (e.defaultPrevented) return
        // Primary button only; modified clicks keep their usual meaning.
        if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return

        const target = e.target as Element | null
        const chip = target?.closest?.<HTMLElement>('.chat-file-path-inert')
        if (!chip) return

        // The public share page renders the same chips but is anonymous and
        // read-only: /api/dir/search is auth-protected, and a viewer must not be
        // able to probe the creator's directory layout. (This layer is only
        // installed by App.vue, so the share SPA never reaches here — the guard
        // is kept so a future reuse cannot silently break that boundary.)
        if (isShareMode()) return

        // Deriving the query is the SINGLE exclusion that keeps a glob pattern
        // non-interactive: it carries the inert class but never gets
        // `data-file-path` (markInertLink fires before the annotation class is
        // added), so `deriveSearchQuery(null)` yields '' and we bail. There is
        // deliberately no separate `if (!path)` guard — an overlapping check
        // would leave neither independently pinned by a test (verified by
        // mutation: removing it changed no outcome).
        const path = chip.getAttribute('data-file-path')
        if (!deriveSearchQuery(path)) return

        e.preventDefault()
        e.stopPropagation()

        const { lineStart, lineEnd, lineRanges } = readInertLineTarget(chip)
        openInertPathPicker({
            path: path!,
            lineStart,
            lineEnd,
            lineRanges,
            source: inferSurface(chip),
        })
    }

    document.addEventListener('click', handler, true)
    return () => document.removeEventListener('click', handler, true)
}

/**
 * Best-effort surface inference for the navigation origin record, mirroring the
 * container selectors used by the local-link guard in App.vue. Returns undefined
 * when the chip is not inside a known panel, which `openFilePath` accepts.
 */
function inferSurface(chip: HTMLElement): NavigationSurface | undefined {
    if (chip.closest('.chat-panel, .chat-panel-content, .chat-message, .chat-messages')) return 'chat'
    if (chip.closest('.task-panel, .task-overview, .task-exec-detail')) return 'task'
    if (chip.closest('.history-panel')) return 'history'
    if (chip.closest('.forge-detail')) return 'forge'
    return undefined
}
