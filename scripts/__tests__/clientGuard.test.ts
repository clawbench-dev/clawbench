import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  AUX_PRELOAD_BRIDGES,
  BRIDGE_CONTRACT_PATH,
  CONTRACT_PATHS,
  DESKTOP_BRIDGE_IMPLEMENTATION_PATH,
  PROTOCOL_FILE,
  bridgeBreakingChanges,
  bridgeMembers,
  contractViolation,
  decideClientBuild,
  healthBreakingChanges,
  healthFields,
  parseProtocol,
  preloadBridgeBreakingChanges,
  preloadBridgeMembers,
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

describe('healthBreakingChanges', () => {
  const health = (...fields: string[]) =>
    [
      'package handler',
      '',
      'type healthResponse struct {',
      '\tApp string `json:"app"`',
      ...fields,
      '}',
      '',
      'func next() int { return 0 }',
    ].join('\n')

  const before = health('\tVersion string `json:"version"`', '\tProtocol int `json:"protocol"`')

  it('accepts an added field', () => {
    // The real case (v0.143.0): `pid` was added so the desktop shell can tell
    // its own server from an older instance on the same port. A stale shell
    // never reads the field, so shipping it without a protocol bump is safe.
    const after = health(
      '\tVersion string `json:"version"`',
      '\tProtocol int `json:"protocol"`',
      '\tPID int `json:"pid"`',
    )
    expect(healthBreakingChanges(before, after)).toEqual([])
  })

  it('rejects a removed field', () => {
    const after = health('\tVersion string `json:"version"`')
    expect(healthBreakingChanges(before, after)).toEqual(['health: protocol was removed'])
  })

  it('rejects a renamed field', () => {
    // Renaming is a removal on the wire, whatever the Go field is called.
    const after = health(
      '\tVersion string `json:"version"`',
      '\tProtocolNumber int `json:"protocolNumber"`',
    )
    expect(healthBreakingChanges(before, after)).toEqual(['health: protocol was removed'])
  })

  it('reads the wire name, not the Go field name', () => {
    // `PID` is `pid` on the wire; a shell keyed on the Go name would miss it.
    const fields = healthFields(
      health('\tVersion string `json:"version"`', '\tProtocol int `json:"protocol"`', '\tPID int `json:"pid"`'),
    )
    expect([...fields.keys()]).toEqual(['app', 'version', 'protocol', 'pid'])
  })

  it('fails closed when either side cannot be read', () => {
    expect(healthBreakingChanges(before, null)).toHaveLength(1)
    expect(healthBreakingChanges('package handler', before)).toHaveLength(1)
  })
})

describe('preloadBridgeBreakingChanges', () => {
  const preload = (...members: string[]) =>
    [
      'contextBridge.exposeInMainWorld(\'ClawBenchNative\', {',
      ...members,
      '})',
    ].join('\n')

  const before = preload(
    '  isNativeApp: () => true,',
    '  getLanguage: () => \'zh\',',
    '  downloadFile: (url: string) => { ipcRenderer.send(\'download\', { url }) },',
  )

  it('accepts an added optional method', () => {
    // The real case: the desktop gained getCloseAction/setCloseAction and an old
    // shell simply lacks them, so the page must keep working (v0.141.0 failed
    // here because the whole preload file was treated as all-or-nothing).
    const after = preload(
      '  isNativeApp: () => true,',
      '  getLanguage: () => \'zh\',',
      '  downloadFile: (url: string) => { ipcRenderer.send(\'download\', { url }) },',
      '  getCloseAction: () => { try { return ipcRenderer.sendSync(\'x\') } catch { return \'ask\' } },',
      '  setCloseAction: (action: string) => { ipcRenderer.send(\'y\', action) },',
    )
    expect(preloadBridgeBreakingChanges(before, after)).toEqual([])
  })

  it('rejects a removed method', () => {
    const after = preload('  isNativeApp: () => true,', '  getLanguage: () => \'zh\',')
    expect(preloadBridgeBreakingChanges(before, after)).toEqual([
      'preload: downloadFile was removed',
    ])
  })

  it('counts a member added later as a removal when it is dropped again', () => {
    // The release guard compares the previous tag to the new one, so a method
    // that only ever existed in the newer release must still be caught: the
    // page's interface declares it, and the shell that just shipped had it.
    const baseline = preload('  isNativeApp: () => true,', '  getLanguage: () => \'zh\',')
    const added = preload(
      '  isNativeApp: () => true,',
      '  getLanguage: () => \'zh\',',
      '  downloadFile: (url: string) => { ipcRenderer.send(\'download\', { url }) },',
    )
    expect(preloadBridgeBreakingChanges(baseline, added)).toEqual([])
    expect(preloadBridgeBreakingChanges(added, baseline)).toEqual([
      'preload: downloadFile was removed',
    ])
  })

  it('finds members even when a method body has its own braces', () => {
    expect([...preloadBridgeMembers(before).keys()]).toEqual([
      'isNativeApp',
      'getLanguage',
      'downloadFile',
    ])
  })

  it('fails closed when either side cannot be read', () => {
    expect(preloadBridgeBreakingChanges(before, null)).toHaveLength(1)
    expect(preloadBridgeBreakingChanges('', before)).toHaveLength(1)
  })

  it('reads a named bridge other than ClawBenchNative', () => {
    // v0.149.0 failed the release here: the overlay's preload exposes
    // `AnnotateHost`, but every file under desktop/src/preload was diffed as
    // `ClawBenchNative` and could not be read — so two ADDED verbs (留存画面 /
    // 录制) were reported as a breaking contract change.
    const overlay = (...members: string[]) =>
      [
        "contextBridge.exposeInMainWorld('AnnotateHost', {",
        ...members,
        '})',
      ].join('\n')
    const old = overlay('  ink(msg: string): void {},', '  close(): void {},')
    const next = overlay(
      '  ink(msg: string): void {},',
      '  close(): void {},',
      '  shot(): void {},',
      '  record(): void {},',
      '  onToolbar(cb: (state: unknown) => void): void {},',
    )
    expect(preloadBridgeBreakingChanges(old, next, 'AnnotateHost')).toEqual([])
    expect(preloadBridgeBreakingChanges(next, old, 'AnnotateHost')).toEqual([
      'preload: shot was removed',
      'preload: record was removed',
      'preload: onToolbar was removed',
    ])
    // Not found under this name → still fails closed rather than passes.
    expect(preloadBridgeBreakingChanges(old, next)).toHaveLength(1)
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

  it('keeps the cross-version bridge names spelled as the previous shell has them', () => {
    // The interface member and the preload member are the same cross-version
    // contract as the `fused_downloads` JSON field. v0.146.0 renamed them to
    // `installIntegratedUpdate` (together with the internal implementation) and
    // the release failed here: a shell built before the rename exposes only the
    // old name, so renaming strands every installed integrated package with no
    // in-app upgrade — or forces a protocol bump that reinstalls every client.
    // Pin both spellings so the internal rename cannot drag them along again.
    expect([...bridgeMembers(repoFile(BRIDGE_CONTRACT_PATH)).keys()]).toContain(
      'installFusedUpdate',
    )
    expect([...bridgeMembers(repoFile(BRIDGE_CONTRACT_PATH)).keys()]).not.toContain(
      'installIntegratedUpdate',
    )
    expect([...preloadBridgeMembers(repoFile(DESKTOP_BRIDGE_IMPLEMENTATION_PATH)).keys()]).toContain(
      'installFusedUpdate',
    )
    // The IPC channel is the other half: a preload calling a channel the main
    // process no longer registers would fail at runtime, not at the gate.
    expect(repoFile('desktop/src/main/bridge.ts')).toContain("'native:install-fused-update'")
    expect(repoFile(DESKTOP_BRIDGE_IMPLEMENTATION_PATH)).toContain("'native:install-fused-update'")
  })

  it('keeps the contract list in one place', () => {
    expect(CONTRACT_PATHS).toContain('web/src/utils/clawbenchNative.ts')
    expect(CONTRACT_PATHS).toContain('desktop/src/preload')
    expect(PROTOCOL_FILE).toBe('internal/version/client_protocol.txt')
  })

  it('judges the overlay and capsule preloads as growing bridges too', () => {
    // Each is its own window's entire bridge, named by the global it exposes.
    // A preload added to the folder but missing here would fall back to the
    // all-or-nothing rule and fail every additive verb.
    for (const { path, global } of AUX_PRELOAD_BRIDGES) {
      expect(CONTRACT_PATHS.some((p) => path.startsWith(p))).toBe(true)
      expect([...preloadBridgeMembers(repoFile(path), global).keys()].length).toBeGreaterThan(0)
    }
    // The main bridge must not be listed twice: its comparison is stricter.
    expect(AUX_PRELOAD_BRIDGES.map((b) => b.path)).not.toContain(
      DESKTOP_BRIDGE_IMPLEMENTATION_PATH,
    )
  })
})
