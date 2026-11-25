import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Wiring guards for the per-profile instance.
 *
 * `index.ts` imports `electron` at module scope, so no unit test can load it.
 * The rules themselves are covered by `instanceProfile.test.ts`; what is
 * checked here is the one thing that has no visible failure mode: the ORDER.
 *
 * - The profile has to be applied before `requestSingleInstanceLock`: the lock
 *   is keyed by userData, so a copy that took the default path collides with
 *   the original and the copy silently quits.
 * - It has to run before `initStore`: the store file lives in userData, and a
 *   copy that opens the original's file would share (and overwrite) its
 *   server list.
 * - The self-upgrade handoff has to carry the instance id: the new process
 *   runs from the shared version store, where the path no longer says which
 *   copy it belongs to.
 *
 * Same convention as `singleInstanceWiring.test.ts`.
 */

const repoRoot = resolve(__dirname, '../../..')
const readRepoFile = (rel: string): string => readFileSync(resolve(repoRoot, rel), 'utf8')

const INDEX = 'desktop/src/main/index.ts'
const INSTALL = 'desktop/src/main/install.ts'

describe('the instance profile is applied before anything keyed by userData', () => {
  it('resolves the profile before the lock and the store', () => {
    const src = readRepoFile(INDEX)
    const applyAt = src.indexOf('applyInstanceProfile()')
    expect(applyAt, 'the profile must be applied').toBeGreaterThan(-1)
    expect(applyAt, 'the profile must be resolved before the lock').toBeLessThan(
      src.indexOf('requestSingleInstanceLock'),
    )
    expect(applyAt, 'the profile must be resolved before the store opens').toBeLessThan(src.indexOf('initStore()'))
  })

  it('the upgrade handoff carries the instance id', () => {
    // Both `handOffToPointedVersion` and `restartInto` go through this spawn.
    const src = readRepoFile(INSTALL)
    expect(src, 'the next process must be told which copy it belongs to').toContain(
      'spawn(bin, currentInstanceArgs()',
    )
  })
})
