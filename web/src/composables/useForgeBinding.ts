import { ref } from 'vue'
import { fetchForgeBinding } from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'

const TAG = 'UseForgeBinding'

// Module-level singleton: the bound platform decides the dock icon, and the
// dock is rendered by App.vue while the binding is loaded by the forge panel.
// A shared ref keeps both in sync without prop drilling.
//
// `platform` is the raw forge platform ("github" | "gitlab"), or '' when
// nothing is bound (or no project is selected).
const platform = ref('')
const host = ref('')

/**
 * setForgeBindingState updates the shared binding from a caller that already
 * holds a binding object (the forge panel), avoiding a duplicate fetch.
 */
export function setForgeBindingState(binding: { platform?: string; host?: string } | null) {
    platform.value = binding?.platform ?? ''
    host.value = binding?.host ?? ''
}

/**
 * useForgeBinding exposes which forge the current project is bound to.
 *
 * The dock icon uses this to show the GitHub logo for github.com and the
 * GitLab logo otherwise, so the tab reflects the actual integration rather
 * than implying GitHub for every user.
 */
export function useForgeBinding() {
    async function refresh() {
        try {
            const res = await fetchForgeBinding()
            setForgeBindingState(res?.binding ?? null)
        } catch (err) {
            // A failed lookup must not leave a stale platform driving the icon.
            appLog.w(TAG, 'refresh failed', err)
            clear()
        }
    }

    function clear() {
        platform.value = ''
        host.value = ''
    }

    return { platform, host, refresh, clear }
}
