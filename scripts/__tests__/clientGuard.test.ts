import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  BRIDGE_CONTRACT_PATH,
  CONTRACT_PATHS,
  PROTOCOL_FILE,
  bridgeBreakingChanges,
  bridgeMembers,
  contractViolation,
  decideClientBuild,
  parseProtocol,
  previousTag,
} from '../lib/client-guard.mjs'

/**
 * The decision table for "rebuild the clients or reuse the previous release".
 *
 * Every case here is a way the release can silently ship an incompatible pair:
 * reusing a build that predates the protocol, skipping a rebuild after the
 * shell changed, or shipping the previous APK when the contract moved.
 */

const decide = (over: Record<string, unknown> = {}) =>
  decideClientBuild({
    prevTag: 'v0.129.0',
    prevHasProtocol: true,
    androidChanged: [],
    desktopChanged: [],
    protocolBumped: false,
    tag: 'v0.130.0',
    ...over,
  })

/** Read a repo file from either the repo root or a worktree one level down. */
function repoFile(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const p = resolve(base, rel)
    if (existsSync(p)) return readFileSync(p, 'utf8')
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

describe('decideClientBuild', () => {
  it('reuses both clients when nothing under android/ or desktop/ changed', () => {
    const d = decide()
    expect(d.androidChanged).toBe(false)
    expect(d.desktopChanged).toBe(false)
    expect(d.androidSource).toBe('v0.129.0')
    expect(d.desktopSource).toBe('v0.129.0')
  })

  it('rebuilds only the client whose sources changed', () => {
    const android = decide({ androidChanged: ['android/app/build.gradle'] })
    expect(android.androidChanged).toBe(true)
    expect(android.androidSource).toBe('v0.130.0')
    expect(android.desktopChanged).toBe(false)

    const desktop = decide({ desktopChanged: ['desktop/src/main/bridge.ts'] })
    expect(desktop.desktopChanged).toBe(true)
    expect(desktop.desktopSource).toBe('v0.130.0')
    expect(desktop.androidChanged).toBe(false)
  })

  it('always rebuilds on the first release', () => {
    // There is no previous artifact to copy.
    const d = decide({ prevTag: '', prevHasProtocol: false })
    expect(d.androidChanged).toBe(true)
    expect(d.desktopChanged).toBe(true)
    expect(d.reason).toContain('first')
  })

  it('rebuilds when the previous release predates the protocol', () => {
    // The killer case: a pre-protocol shell gates on version equality. Handing
    // it this release's reused artifact makes it download, still see a version
    // mismatch, and loop forever.
    const d = decide({ prevHasProtocol: false })
    expect(d.androidChanged).toBe(true)
    expect(d.desktopChanged).toBe(true)
    expect(d.reason).toContain('predates')
  })

  it('rebuilds both when the protocol number was bumped', () => {
    // The number is compiled into each shell, so a bump means every existing
    // shell is incompatible by definition.
    const d = decide({ protocolBumped: true })
    expect(d.androidChanged).toBe(true)
    expect(d.desktopChanged).toBe(true)
    expect(d.reason).toContain('protocol')
  })
})

describe('contractViolation', () => {
  it('fails when a contract file broke and the protocol stayed put', () => {
    expect(
      contractViolation({
        breakingContractPaths: ['web/src/utils/clawbenchNative.ts'],
        prevProtocol: 3,
        protocol: 3,
      }),
    ).toBe(true)
  })

  it('passes when the protocol was bumped alongside the change', () => {
    expect(
      contractViolation({
        breakingContractPaths: ['web/src/utils/clawbenchNative.ts'],
        prevProtocol: 3,
        protocol: 4,
      }),
    ).toBe(false)
  })

  it('passes when the guard is being introduced (no previous protocol)', () => {
    // The rebuild is forced instead; there is nothing to compare against.
    expect(
      contractViolation({
        breakingContractPaths: ['internal/handler/health.go'],
        prevProtocol: null,
        protocol: 1,
      }),
    ).toBe(false)
  })

  it('passes when no contract file changed', () => {
    expect(contractViolation({ breakingContractPaths: [], prevProtocol: 3, protocol: 3 })).toBe(false)
  })
})

/** A miniature bridge interface, formatted exactly like the real one. */
const bridge = (members: string) => `export interface ClawBenchNative {\n${members}\n}\n\nexport function getNative() {\n  return null\n}\n`

describe('bridgeMembers', () => {
  it('records every member and whether it is optional', () => {
    const members = bridgeMembers(
      bridge(['  isNativeApp(): boolean', '  getCapabilities?(): NativeCapabilities', '  log(level: string): void'].join('\n')),
    )
    expect(members.size).toBe(3)
    expect(members.get('isNativeApp')).toEqual({ optional: false })
    expect(members.get('getCapabilities')).toEqual({ optional: true })
    expect(members.get('log')).toEqual({ optional: false })
  })

  it('does not mistake multi-line signatures, comments or nested fields for members', () => {
    const members = bridgeMembers(
      bridge(
        [
          '  /** Whether the host can do it. */',
          '  can?(): boolean',
          '  showSelectionMenu?(',
          '    text: string,',
          '    left: number,',
          '  ): void',
          '  remoteControl?(ev: {',
          "    kind: 'move' | 'down'",
          '  }): Promise<void> | void',
          '  // Async reads',
          '  getAppVersion(): Promise<string>',
        ].join('\n'),
      ),
    )
    expect([...members.keys()]).toEqual(['can', 'showSelectionMenu', 'remoteControl', 'getAppVersion'])
  })

  it('reads the real interface, so a reformat cannot silently blind the guard', () => {
    // An unreadable interface fails closed (see below), which would block every
    // release — this test is what turns that into a PR-time failure instead.
    const members = bridgeMembers(repoFile(BRIDGE_CONTRACT_PATH))
    expect(members.size).toBeGreaterThan(20)
    expect(members.get('isNativeApp')).toEqual({ optional: false })
    expect(members.get('getCapabilities')).toEqual({ optional: true })
  })
})

describe('bridgeBreakingChanges', () => {
  const before = bridge(
    [
      '  isNativeApp(): boolean',
      '  getLanguage(): string',
      '  downloadFileWithProgress?(path: string): Promise<void>',
    ].join('\n'),
  )

  it('accepts a purely additive change', () => {
    const after = bridge(
      [
        '  isNativeApp(): boolean',
        '  getLanguage(): string',
        '  downloadFileWithProgress?(path: string): Promise<void>',
        '  getCapabilities?(): NativeCapabilities',
        '  readClipboardText?(): Promise<string>',
      ].join('\n'),
    )
    expect(bridgeBreakingChanges(before, after)).toEqual([])
  })

  it('accepts a widened return type', () => {
    // `void | Promise<void>` is what a shell returning nothing satisfies; every
    // caller awaits, so this cannot break an older host.
    const after = bridge(
      [
        '  isNativeApp(): boolean',
        '  getLanguage(): string',
        '  downloadFileWithProgress?(path: string): void | Promise<void>',
      ].join('\n'),
    )
    expect(bridgeBreakingChanges(before, after)).toEqual([])
  })

  it('rejects a removed member', () => {
    const after = bridge(['  isNativeApp(): boolean', '  getLanguage(): string'].join('\n'))
    expect(bridgeBreakingChanges(before, after)).toEqual(['downloadFileWithProgress was removed'])
  })

  it('rejects a member that became mandatory', () => {
    // The page would call it unconditionally on a host that never had it.
    const after = bridge(
      [
        '  isNativeApp(): boolean',
        '  getLanguage(): string',
        '  downloadFileWithProgress(path: string): Promise<void>',
      ].join('\n'),
    )
    expect(bridgeBreakingChanges(before, after)).toEqual(['downloadFileWithProgress became mandatory'])
  })

  it('rejects a new mandatory member', () => {
    const after = bridge(
      [
        '  isNativeApp(): boolean',
        '  getLanguage(): string',
        '  downloadFileWithProgress?(path: string): Promise<void>',
        '  getCapabilities(): NativeCapabilities',
      ].join('\n'),
    )
    expect(bridgeBreakingChanges(before, after)).toEqual(['getCapabilities is new but mandatory (mark it optional)'])
  })

  it('fails closed when either side cannot be read', () => {
    // Silence here would mean "compatible" for a file we never parsed.
    expect(bridgeBreakingChanges(before, null)).toHaveLength(1)
    expect(bridgeBreakingChanges('not an interface', before)).toHaveLength(1)
  })
})

describe('parseProtocol', () => {
  it('accepts a positive integer with surrounding whitespace', () => {
    expect(parseProtocol('1\n')).toBe(1)
    expect(parseProtocol(' 12 ')).toBe(12)
  })

  it('rejects anything else', () => {
    for (const raw of ['', '0', '-1', 'v2', '1.5', 'abc', null, undefined]) {
      expect(parseProtocol(raw as string)).toBeNull()
    }
  })
})

describe('previousTag', () => {
  it('picks the highest version tag that is not the current one', () => {
    expect(previousTag(['v0.130.0', 'v0.129.0', 'v0.128.0'], 'v0.130.0')).toBe('v0.129.0')
  })

  it('ignores non-release tags', () => {
    expect(previousTag(['v0.130.0', 'nightly', 'v0.129.0'], 'v0.130.0')).toBe('v0.129.0')
  })

  it('returns empty when there is no earlier release', () => {
    expect(previousTag(['v0.130.0'], 'v0.130.0')).toBe('')
    expect(previousTag([], 'v0.130.0')).toBe('')
  })
})

describe('release workflow wiring', () => {
  /**
   * The script is only half the feature: the workflow has to consult it and
   * actually skip the heavy steps. These are source guards because the failure
   * mode is invisible — the release stays green and simply rebuilds everything
   * (or, worse, reuses an artifact while the rebuild branch is what runs).
   */
  const yml = repoFile('.github/workflows/release.yml')
  const reuseScript = repoFile('scripts/lib/reuse-client-artifact.sh')

  it('defines the client-guard job and exports what downstream jobs need', () => {
    expect(yml).toMatch(/^ {2}client-guard:$/m)
    expect(yml).toMatch(/^ {6}android_changed: \$\{\{ steps\.guard\.outputs\.android_changed \}\}$/m)
    expect(yml).toMatch(/^ {6}desktop_changed: \$\{\{ steps\.guard\.outputs\.desktop_changed \}\}$/m)
    expect(yml).toMatch(/node scripts\/lib\/client-guard\.mjs/)
  })

  it('gates the APK build on the guard and has a reuse branch', () => {
    expect(yml).toMatch(/needs: \[client-guard\]/)
    expect(yml).toContain('NEEDS_BUILD=${{ needs.client-guard.outputs.android_changed }}')
    expect(yml).toContain('reuse-client-artifact.sh apk')
    expect(reuseScript).toContain('gh release download')
    // The reuse branch must be able to fall back to a real build.
    expect(yml).toContain('NEEDS_BUILD=true')
  })

  it('gates every desktop build on the guard', () => {
    const desktopJobs = yml.match(/^ {2}build-desktop-[a-z0-9-]+:$/gm) ?? []
    expect(desktopJobs.length).toBeGreaterThanOrEqual(4)
    // Each desktop job must consult the shared output; a job that ignores it
    // rebuilds silently and the release looks fine, just slow.
    const desktopBlocks = yml.split(/^ {2}build-desktop-[a-z0-9-]+:$/m).slice(1)
    for (const block of desktopBlocks) {
      expect(block).toMatch(/NEEDS_BUILD=\$\{\{ needs\.client-guard\.outputs\.desktop_changed \}\}/)
      expect(block).toContain('reuse-client-artifact.sh desktop')
    }
  })

  it('passes the reused client version to the Go binaries', () => {
    // The desktop installer keys its side-by-side directory off this value; a
    // release that reuses v0.129 must say so, or the startup handoff deletes
    // the pointer and reverts the upgrade.
    expect(yml).toContain('clawbench/internal/version.ClientBuildVersion')
    expect(yml).toMatch(/needs\.client-guard\.outputs\.desktop_source/)
  })

  it('keeps the contract list in one place', () => {
    expect(CONTRACT_PATHS).toContain('web/src/utils/clawbenchNative.ts')
    expect(PROTOCOL_FILE).toBe('internal/version/client_protocol.txt')
  })
})
