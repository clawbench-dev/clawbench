import { describe, it, expect, vi, beforeEach } from 'vitest'

/**
 * Screen-capture plumbing for the live share feature.
 *
 * The properties that matter are security/correctness invariants that are
 * invisible at runtime until they break:
 *
 *   1. A getDisplayMedia call WITHOUT a declared source must be refused, and the
 *      declaration must be consumed exactly once (otherwise any page in the
 *      renderer could silently capture the screen later).
 *   2. System (loopback) audio is Windows-only; asking for it elsewhere must not
 *      be forwarded to Electron, where it makes the whole capture fail.
 *
 * The decision logic is a pure function (`decideCapture`) so these can be
 * asserted without faking Electron's capture stack; the handler test only pins
 * that it wires that function up and consumes the declaration.
 */

const sources = vi.hoisted(() => ({
  value: [] as Array<{ id: string; name: string; thumbnail: { isEmpty: () => boolean; toDataURL: () => string } }>,
}))
const displayHandler = vi.hoisted(() => ({
  value: null as null | ((req: unknown, cb: (s: unknown) => void) => void),
}))
const recorded = vi.hoisted(() => ({ value: [] as string[] }))

vi.mock('electron', () => ({
  ipcMain: { handle: () => undefined },
  session: {
    defaultSession: {
      setDisplayMediaRequestHandler: (fn: unknown) => {
        displayHandler.value = fn as (req: unknown, cb: (s: unknown) => void) => void
      },
    },
  },
  desktopCapturer: {
    getSources: async () => sources.value,
  },
}))

vi.mock('./clientLog', () => ({
  record: (level: string, tag: string, msg: string) => {
    recorded.value.push(`${level}/${tag}: ${msg}`)
  },
}))

import {
  _pendingCaptureForTest,
  _resetCaptureForTest,
  _setPendingCaptureForTest,
  audioCaptureSupport,
  decideCapture,
  installDisplayMediaHandler,
  listCaptureSources,
} from './capture'

function fakeSource(id: string, name = id, empty = true) {
  return {
    id,
    name,
    thumbnail: { isEmpty: () => empty, toDataURL: () => `data:image/png;base64,${id}` },
  }
}

/** Invoke the installed handler and return what it answered with. */
async function requestCapture(req: { audioRequested?: boolean } = {}): Promise<Record<string, unknown>> {
  if (!displayHandler.value) throw new Error('handler not installed')
  return (await new Promise<Record<string, unknown>>((resolve) => {
    displayHandler.value!({ audioRequested: req.audioRequested ?? false }, resolve as (s: unknown) => void)
  })) ?? {}
}

beforeEach(() => {
  sources.value = [fakeSource('screen:1:0'), fakeSource('window:2:0')]
  displayHandler.value = null
  recorded.value = []
  _resetCaptureForTest()
  installDisplayMediaHandler()
})

describe('decideCapture', () => {
  const available = [{ id: 'screen:1:0' }, { id: 'window:2:0' }]

  it('refuses when nothing was declared', () => {
    expect(decideCapture(null, false, true, available)).toBeNull()
  })

  it('refuses when the declared source is gone', () => {
    // The monitor may have been unplugged between listing and capture.
    expect(decideCapture({ sourceId: 'screen:9:0', systemAudio: false }, false, true, available)).toBeNull()
  })

  it('accepts a declared, available source', () => {
    expect(decideCapture({ sourceId: 'screen:1:0', systemAudio: false }, false, true, available)).toEqual({
      sourceId: 'screen:1:0',
      withAudio: false,
    })
  })

  it('includes audio only when asked, declared and supported', () => {
    const want = { sourceId: 'screen:1:0', systemAudio: true }
    expect(decideCapture(want, true, true, available)?.withAudio).toBe(true)
    // Renderer did not ask for audio.
    expect(decideCapture(want, false, true, available)?.withAudio).toBe(false)
    // Renderer did not declare system audio.
    expect(decideCapture({ sourceId: 'screen:1:0', systemAudio: false }, true, true, available)?.withAudio).toBe(false)
    // Platform cannot do loopback: forwarding it would fail the whole capture.
    expect(decideCapture(want, true, false, available)?.withAudio).toBe(false)
  })
})

describe('audioCaptureSupport', () => {
  it('reports loopback availability per platform', () => {
    const support = audioCaptureSupport()
    if (process.platform === 'win32') {
      expect(support.systemAudio).toBe(true)
      expect(support.reason).toBe('')
    } else {
      expect(support.systemAudio).toBe(false)
      expect(support.reason).not.toBe('')
    }
  })
})

describe('listCaptureSources', () => {
  it('maps screen ids to the screen kind', async () => {
    const list = await listCaptureSources()
    expect(list).toHaveLength(2)
    expect(list[0]).toMatchObject({ id: 'screen:1:0', kind: 'screen' })
    expect(list[1]).toMatchObject({ id: 'window:2:0', kind: 'window' })
  })

  it('omits the thumbnail when the source has none', async () => {
    sources.value = [fakeSource('screen:1:0', 'screen', true)]
    const list = await listCaptureSources()
    expect(list[0].thumbnail).toBe('')
  })

  it('includes a data URL when a thumbnail exists', async () => {
    sources.value = [fakeSource('screen:1:0', 'screen', false)]
    const list = await listCaptureSources()
    expect(list[0].thumbnail).toContain('data:image/png')
  })
})

describe('display media handler', () => {
  it('refuses when no source was declared', async () => {
    const answer = await requestCapture()
    // An empty answer makes getDisplayMedia reject — the correct outcome for an
    // undeclared request.
    expect(answer).toEqual({})
    expect(recorded.value.join()).toContain('refused')
  })

  it('answers with the declared source', async () => {
    _setPendingCaptureForTest({ sourceId: 'screen:1:0', systemAudio: false })

    const answer = await requestCapture()

    expect(answer.video).toMatchObject({ id: 'screen:1:0' })
  })

  it('consumes the declaration exactly once', async () => {
    _setPendingCaptureForTest({ sourceId: 'screen:1:0', systemAudio: false })

    expect((await requestCapture()).video).toBeDefined()
    expect(_pendingCaptureForTest()).toBeNull()

    // A second request must be refused: nothing is remembered between calls.
    expect(await requestCapture()).toEqual({})
  })

  it('never forwards system audio on a platform without loopback', async () => {
    if (process.platform === 'win32') return // loopback is valid there
    _setPendingCaptureForTest({ sourceId: 'screen:1:0', systemAudio: true })

    const answer = await requestCapture({ audioRequested: true })

    expect(answer.audio).toBeUndefined()
  })

  it('refuses when the declared source disappeared', async () => {
    _setPendingCaptureForTest({ sourceId: 'screen:9:0', systemAudio: false })

    expect(await requestCapture()).toEqual({})
    expect(recorded.value.join()).toContain('unavailable')
  })

  it('does not leak the pending declaration across installs', () => {
    installDisplayMediaHandler()
    expect(_pendingCaptureForTest()).toBeNull()
  })
})
