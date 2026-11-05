/**
 * The relay (LiveKit) endpoint + credential, as the create form edits it.
 *
 * One configuration for the whole installation, not per meeting: the relay is
 * deployed once and every meeting that opts in shares it. The credential is
 * write-only until the eye button asks for it: `load` only learns `hasSecret`,
 * and `reveal` is the one call that carries the stored values.
 *
 * Module-level state, like the other meeting singletons: the create form is
 * remounted every time the panel is opened, and re-fetching on each mount would
 * flicker the fields.
 */
import { ref } from 'vue'
import { apiGet, apiPatch, apiPost } from '@/utils/api'

export interface LiveSFUConfigState {
  url: string
  hasSecret: boolean
  configured: boolean
}

const state = ref<LiveSFUConfigState>({ url: '', hasSecret: false, configured: false })
const loaded = ref(false)
const busy = ref(false)

/** Result of one "save and test" attempt. */
export interface RelayTestResult {
  ok: boolean
  message: string
}

/** The stored credential, only available through the explicit reveal. */
export interface LiveSFURevealed {
  apiKey: string
  apiSecret: string
}

export function useLiveSFUConfig() {
  /** Read the stored configuration once (or again with force). */
  async function load(force = false): Promise<void> {
    if (loaded.value && !force) return
    try {
      state.value = await apiGet<LiveSFUConfigState>('/api/live-share/sfu')
      loaded.value = true
    } catch {
      // A failed read must not block the form: it just means "unknown", and the
      // save path will surface a real error if one exists.
    }
  }

  /**
   * Probe the draft values, and save them only if the probe passes.
   *
   * Test-then-save rather than save-then-test: storing a broken endpoint would
   * leave every later meeting refusing to start until somebody notices, and the
   * user would have no way to tell which of the three fields is wrong.
   */
  async function saveAndTest(draft: {
    url: string
    apiKey: string
    apiSecret: string
  }): Promise<RelayTestResult> {
    if (busy.value) return { ok: false, message: '' }
    busy.value = true
    try {
      const probe = await apiPost<{ success: boolean; message: string }>('/api/live-share/sfu/test', {
        url: draft.url.trim(),
        apiKey: draft.apiKey.trim(),
        apiSecret: draft.apiSecret.trim(),
      })
      if (!probe.success) return { ok: false, message: probe.message }

      const body: Record<string, unknown> = { url: draft.url.trim() }
      // Empty means "unchanged" — the form never received the stored secret, so
      // sending "" would wipe a working credential.
      if (draft.apiKey.trim()) body.apiKey = draft.apiKey.trim()
      if (draft.apiSecret.trim()) body.apiSecret = draft.apiSecret.trim()
      state.value = await apiPatch<LiveSFUConfigState>('/api/live-share/sfu', body)
      loaded.value = true
      return { ok: true, message: probe.message }
    } catch (e) {
      return { ok: false, message: e instanceof Error ? e.message : String(e) }
    } finally {
      busy.value = false
    }
  }

  /**
   * Ask the server for the stored credential.
   *
   * Deliberately not part of `load`: reading the panel must not pull a
   * plaintext secret into the page. Only the eye button calls this.
   */
  async function reveal(): Promise<LiveSFURevealed> {
    const res = await apiGet<LiveSFURevealed>('/api/live-share/sfu?reveal=1')
    return { apiKey: res.apiKey ?? '', apiSecret: res.apiSecret ?? '' }
  }

  return { state, loaded, busy, load, reveal, saveAndTest }
}

/** @internal Test-only: forget what was loaded. */
export function _resetLiveSFUConfigForTest(): void {
  state.value = { url: '', hasSecret: false, configured: false }
  loaded.value = false
  busy.value = false
}
