import { ref, computed } from 'vue'
import { fetchForgeBinding, type ForgeBinding } from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'

const TAG = 'UseForgeBinding'

// How long a resolved binding stays fresh. The binding is a project property
// that changes only when the user rebinds or unbinds, so a short window is
// enough to collapse the burst of fetches that a single navigation produces
// (App mount + task list + task form + event card + forge panel all resolve it).
const BINDING_TTL_MS = 5_000

// Module-level singleton: the bound platform decides the dock icon, and the
// dock is rendered by App.vue while the binding is loaded by the forge panel.
// A shared ref keeps both in sync without prop drilling.
//
// `binding` is the raw ForgeBinding, or null when nothing is bound (or no
// project is selected). `resolved` distinguishes "not fetched yet" from
// "fetched and confirmed unbound" — without it every consumer would render the
// "no repository bound" warning during the initial load, which is wrong.
const binding = ref<ForgeBinding | null>(null)
const resolved = ref(false)

// In-flight dedup: concurrent callers share one request instead of each
// issuing its own. Without this, opening the task list fires 3-5 identical
// GETs that queue behind each other on the server's 2-connection read pool.
let inflight: Promise<ForgeBinding | null> | null = null
let fetchedAt = 0

// Monotonic guard: a stale response (project switched mid-flight) must not
// overwrite the newer project's binding.
let requestSeq = 0

const platform = computed(() => binding.value?.platform ?? '')

/**
 * The canonical "owner/repo" label, or '' when either half is missing.
 *
 * Shared by the `slug` computed and setForgeBindingState so the two can never
 * disagree: a binding with an empty owner or repo reads as unbound either way,
 * rather than one path yielding "/" and the other "".
 */
function bindingSlug(b: { owner?: string; repo?: string } | null | undefined): string {
    return b && b.owner && b.repo ? `${b.owner}/${b.repo}` : ''
}

/** The canonical "owner/repo" label, or '' when unbound. */
const slug = computed(() => bindingSlug(binding.value))

/**
 * setForgeBindingState updates the shared binding from a caller that already
 * holds a binding object (the forge panel), avoiding a duplicate fetch. It
 * marks the value resolved, since the caller has just read it from the server.
 */
export function setForgeBindingState(next: { platform?: string; host?: string; owner?: string; repo?: string } | null) {
    binding.value = next
        ? {
            platform: next.platform ?? '',
            host: next.host ?? '',
            owner: next.owner ?? '',
            repo: next.repo ?? '',
            slug: bindingSlug(next),
        }
        : null
    resolved.value = true
    fetchedAt = Date.now()
}

/**
 * forgeDockIconKind decides which brand icon the forge dock tab shows.
 *
 * GitHub is the default — including before any repository is bound, since the
 * tab exists regardless of binding. Only an actually-bound GitLab repository
 * switches it to the GitLab mark.
 */
export function forgeDockIconKind(currentPlatform: string): 'github' | 'gitlab' {
    return currentPlatform === 'gitlab' ? 'gitlab' : 'github'
}

/**
 * useForgeBinding exposes which forge the current project is bound to, and is
 * the single source of truth for every consumer that needs it.
 *
 * The dock icon uses `platform` to show the GitHub logo for github.com and the
 * GitLab logo otherwise, so the tab reflects the actual integration rather
 * than implying GitHub for every user. Task views use `slug`/`resolved` to
 * label the watched repository without flashing a false "unbound" state.
 */
export function useForgeBinding() {
    /**
     * Load the binding, coalescing concurrent callers onto one request.
     *
     * @param force bypass the TTL cache AND supersede any in-flight lookup.
     *              Use after a write (bind/unbind): an in-flight request was
     *              started before the write, so its answer is stale by
     *              definition and must not be adopted.
     */
    async function refresh(force = false): Promise<ForgeBinding | null> {
        if (!force && resolved.value && Date.now() - fetchedAt < BINDING_TTL_MS) {
            return binding.value
        }
        // Only a cache-bypassing call may preempt an in-flight lookup; everyone
        // else rides along on it.
        if (!force && inflight) return inflight

        // Bumping the sequence makes any older in-flight response discard its
        // result instead of overwriting this one.
        const seq = ++requestSeq
        const request = (async () => {
            try {
                const res = await fetchForgeBinding()
                if (seq !== requestSeq) return binding.value
                binding.value = res?.binding ?? null
                resolved.value = true
                fetchedAt = Date.now()
                return binding.value
            } catch (err) {
                // A failed lookup must not leave a stale binding driving the
                // icon or the repository label, so the value is cleared. It is
                // deliberately NOT stamped into the cache: a transient failure
                // would otherwise be served as "confirmed unbound" for the
                // whole TTL, showing a bound project as unbound with no retry.
                // `resolved` still flips so consumers leave the loading state.
                appLog.w(TAG, 'refresh failed', err)
                if (seq === requestSeq) {
                    binding.value = null
                    resolved.value = true
                    fetchedAt = 0
                }
                return binding.value
            } finally {
                if (seq === requestSeq) inflight = null
            }
        })()
        inflight = request
        return request
    }

    return { binding, platform, slug, resolved, refresh }
}

/**
 * resetForgeBindingState drops the cached binding. Called on project switch:
 * the binding belongs to the previous project and must not be shown for the
 * new one while its own lookup is in flight.
 */
export function resetForgeBindingState() {
    binding.value = null
    resolved.value = false
    fetchedAt = 0
    inflight = null
    requestSeq++
}
