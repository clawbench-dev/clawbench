import { describe, expect, it, vi, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The "a share changed" notification must be emitted at every place a share is
 * actually created or revoked, and ONLY there.
 *
 * This is a source guard on purpose. The three emit sites live in three
 * components that each need a mounted dialog, a server stub and a confirmed
 * prompt to reach — a behaviour test per site would be three near-copies of the
 * dialog's own suite, and the failure it protects against (someone adds a new
 * share entry point and forgets the emit) would still slip through, because the
 * new site has no test at all. Reading the source lets this test assert the
 * INVENTORY: every file that writes a share is listed here with the number of
 * emits it must contain.
 */

// __dirname is web/src/components/share/__tests__ — four levels up is web/,
// which is the prefix the file paths below are written against.
const webRoot = join(__dirname, '..', '..', '..', '..')

function src(rel: string): string {
  return readFileSync(join(webRoot, rel), 'utf8')
}

/** Every place that creates or revokes a share, and how many emits it owns. */
const EMIT_SITES: Array<{ file: string; kind: string; why: string }> = [
  {
    file: 'src/components/session/SessionShareDialog.vue',
    kind: 'session',
    // createLink + revokeLink. NOT loadStatus: reading the current state is not
    // a change, and emitting there would make the tab refetch on every open.
    why: 'the conversation share dialog creates and revokes links',
  },
  {
    file: 'src/components/file/ShareLinkDialog.vue',
    kind: 'file',
    // createLink (also covers regenerate, which calls it) + revokeLink.
    why: 'the file share dialog creates, regenerates and revokes links',
  },
  {
    file: 'src/components/live/MeetingDetail.vue',
    kind: 'meeting',
    // setShareEnabled.
    why: "the meeting result page decides whether the record's link stays readable",
  },
]

beforeEach(() => {
  vi.clearAllMocks()
})

describe('share change notification — emit sites', () => {
  for (const { file, kind, why } of EMIT_SITES) {
    it(`emits ${kind} from ${why}`, () => {
      const source = src(file)
      const emits = source.match(new RegExp(`emitShareChanged\\('${kind}'\\)`, 'g')) ?? []

      // Exactly two for the two dialogs (create + revoke); one for the meeting
      // switch. A change here means a new entry point was added — which is
      // exactly when someone forgets the emit, so the number is pinned.
      const expected = kind === 'meeting' ? 1 : 2
      expect(
        emits.length,
        `${file} must emit ${expected} ${kind} change(s) — if you added a share entry point, add its emit here too`,
      ).toBe(expected)

      expect(source, `${file} must import the emitter`).toContain(
        "from '@/composables/useShareEvents'",
      )
    })
  }

  it('does not emit while merely READING the current share state', () => {
    // loadStatus reconciles the badge with the server. It runs on every dialog
    // open, so an emit there would refetch the whole tab each time a user
    // merely looked at an existing share.
    const sessionDialog = src('src/components/session/SessionShareDialog.vue')
    const loadStatus = sessionDialog.slice(
      sessionDialog.indexOf('async function loadStatus'),
      sessionDialog.indexOf('async function createLink'),
    )
    expect(loadStatus, 'loadStatus must not announce a change').not.toContain('emitShareChanged')

    const fileDialog = src('src/components/file/ShareLinkDialog.vue')
    const fileLoad = fileDialog.slice(
      fileDialog.indexOf('async function loadStatus'),
      fileDialog.indexOf('async function createLink'),
    )
    expect(fileLoad, 'loadStatus must not announce a change').not.toContain('emitShareChanged')
  })

  it('has a subscriber that refetches only while visible', () => {
    const panel = src('src/components/share/ShareCenterPanel.vue')
    expect(panel, 'the panel must subscribe').toContain('onShareChanged(')
    // Gated on `active`: a hidden tab refetches three endpoints for nothing,
    // and the activation watcher already covers it.
    expect(panel).toMatch(/onShareChanged\(\(\) => \{\s*if \(props\.active\)/)
    // And it must unsubscribe, or a remount leaves a dead panel fetching.
    expect(panel, 'the panel must unsubscribe on unmount').toContain('onBeforeUnmount(')
  })
})
