import { describe, it, expect, vi } from 'vitest'
import { attemptSavedPasswordLogin } from '@/utils/savedPasswordLogin'

/**
 * Guards the silent-failure regression in the 401/403 auto-login path.
 *
 * Previously every failure — a rejected password, an unreachable server —
 * fell through to the login page with no message at all, so a rotated
 * password looked like the app randomly logging the user out. The outcomes
 * below are what let App.vue show a reason.
 */

describe('attemptSavedPasswordLogin', () => {
  it('authenticates and mirrors the password back', async () => {
    const login = vi.fn().mockResolvedValue({ ok: true })
    const persistPassword = vi.fn().mockResolvedValue(undefined)

    const outcome = await attemptSavedPasswordLogin({
      getSavedPassword: () => 'saved-pw',
      login,
      persistPassword,
    })

    expect(outcome).toBe('authenticated')
    expect(login).toHaveBeenCalledWith('saved-pw')
    expect(persistPassword).toHaveBeenCalledWith('saved-pw')
  })

  it('reports no-password when the host has none stored', async () => {
    const login = vi.fn()

    const outcome = await attemptSavedPasswordLogin({ getSavedPassword: () => '', login })

    expect(outcome).toBe('no-password')
    // Nothing to try — and this is not a failure worth reporting to the user.
    expect(login).not.toHaveBeenCalled()
  })

  it('reports no-password for undefined', async () => {
    const login = vi.fn()
    expect(await attemptSavedPasswordLogin({ getSavedPassword: () => undefined, login })).toBe('no-password')
    expect(login).not.toHaveBeenCalled()
  })

  it('reports auth-failed when the stored password is rejected', async () => {
    // The key case: this MUST be distinguishable from no-password, or the user
    // gets no explanation for being sent back to the login page.
    const outcome = await attemptSavedPasswordLogin({
      getSavedPassword: () => 'stale-pw',
      login: async () => ({ ok: false }),
    })

    expect(outcome).toBe('auth-failed')
  })

  it('reports network-error when the login request throws', async () => {
    const outcome = await attemptSavedPasswordLogin({
      getSavedPassword: () => 'saved-pw',
      login: async () => { throw new Error('connection refused') },
    })

    expect(outcome).toBe('network-error')
  })

  it('reports network-error when reading the password throws', async () => {
    // A failing native bridge must not surface as an auth failure.
    const outcome = await attemptSavedPasswordLogin({
      getSavedPassword: () => { throw new Error('bridge gone') },
      login: async () => ({ ok: true }),
    })

    expect(outcome).toBe('network-error')
  })

  it('stays authenticated when mirroring the password back fails', async () => {
    // The mirror is best-effort: the user IS logged in, so a native write
    // failure must not bounce them back to the login page.
    const outcome = await attemptSavedPasswordLogin({
      getSavedPassword: () => 'saved-pw',
      login: async () => ({ ok: true }),
      persistPassword: async () => { throw new Error('ipc failed') },
    })

    expect(outcome).toBe('authenticated')
  })

  it('awaits an async getSavedPassword', async () => {
    const outcome = await attemptSavedPasswordLogin({
      getSavedPassword: async () => 'async-pw',
      login: async () => ({ ok: true }),
    })

    expect(outcome).toBe('authenticated')
  })
})
