import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

/**
 * The remote-control request queue.
 *
 * The helper speaks newline-delimited JSON and replies are correlated by
 * POSITION, which makes a stuck helper dangerous in two ways: every pointer
 * move (they arrive at IPC rate) leaves a promise and a queue entry behind
 * forever, and a request that silently leaves the queue would shift every later
 * reply onto the wrong event. The deadline below is what bounds both.
 */

const state = vi.hoisted(() => ({
  spawns: 0,
  killed: 0,
  writes: [] as string[],
  /** Handlers registered by the module on the current fake child. */
  listeners: {} as Record<string, Array<(arg?: unknown) => void>>,
  log: [] as string[],
}))

function makeProc() {
  const on = (key: string) => (ev: string, fn: (arg?: unknown) => void) => {
    ;(state.listeners[`${key}:${ev}`] ||= []).push(fn)
  }
  return {
    stdout: { setEncoding: () => {}, on: on('out') },
    stderr: { resume: () => {} },
    stdin: {
      on: () => {},
      write: (line: string) => {
        state.writes.push(line)
        return true
      },
      end: () => {},
    },
    on: on('proc'),
    kill: () => {
      state.killed += 1
      for (const fn of state.listeners['proc:exit'] ?? []) fn()
    },
  }
}

vi.mock('electron', () => ({
  app: { isPackaged: false, getAppPath: () => '/tmp/clawbench-test' },
  screen: { getAllDisplays: () => [] },
}))

vi.mock('node:child_process', () => {
  // Same function behind both keys: a CJS builtin is reached through either the
  // named import or the interop default depending on how it is loaded.
  const spawn = () => {
    state.spawns += 1
    state.listeners = {}
    return makeProc()
  }
  return { spawn, default: { spawn } }
})

vi.mock('node:fs', () => ({
  default: { existsSync: () => true },
  existsSync: () => true,
}))

vi.mock('./window', () => ({ getMainWindow: () => null }))
vi.mock('./capture', () => ({ currentCaptureDisplayId: () => '' }))
vi.mock('./clientLog', () => ({
  record: (_level: string, _tag: string, msg: string) => {
    state.log.push(msg)
  },
}))

import { applyRemoteControlEvent, stopRemoteControlHelper } from './remoteControl'

/** Deliver one reply line from the helper, as its stdout would. */
function reply(payload: string): void {
  for (const fn of state.listeners['out:data'] ?? []) fn(payload)
}

beforeEach(() => {
  stopRemoteControlHelper()
  state.spawns = 0
  state.killed = 0
  state.writes = []
  state.listeners = {}
  state.log = []
})

afterEach(() => {
  vi.useRealTimers()
})

describe('applyRemoteControlEvent', () => {
  it('resolves with the helper reply', async () => {
    const done = applyRemoteControlEvent({ kind: 'move', x: 0.5, y: 0.5 })
    reply('{"ok":true}\n')
    await expect(done).resolves.toBe(true)
    expect(state.writes).toHaveLength(1)
  })

  it('gives up on a helper that stops answering, instead of queueing forever', async () => {
    // Pointer moves arrive at IPC rate: without a deadline every one of them
    // would hold a promise and a queue slot for as long as the wedge lasts.
    vi.useFakeTimers()
    const done = applyRemoteControlEvent({ kind: 'move', x: 0.1, y: 0.1 })

    await vi.advanceTimersByTimeAsync(2000)

    await expect(done).resolves.toBe(false)
    expect(state.log.join()).toContain('did not answer')
    // The pipeline is dropped rather than half-kept: positional correlation
    // cannot survive a hole in the queue.
    expect(state.killed).toBe(1)

    // The next event gets a fresh helper instead of the wedged one.
    const next = applyRemoteControlEvent({ kind: 'move', x: 0.2, y: 0.2 })
    expect(state.spawns).toBe(2)
    reply('{"ok":true}\n')
    await expect(next).resolves.toBe(true)
  })

  it('fails every in-flight request when the helper is reset', async () => {
    vi.useFakeTimers()
    const first = applyRemoteControlEvent({ kind: 'move', x: 0.1, y: 0.1 })
    const second = applyRemoteControlEvent({ kind: 'move', x: 0.2, y: 0.2 })

    await vi.advanceTimersByTimeAsync(2000)

    // Both were waiting on the same wedged process; neither may hang.
    await expect(first).resolves.toBe(false)
    await expect(second).resolves.toBe(false)
  })

  it('does not hold a timer open after a normal reply', async () => {
    // A leaked timer per move would be its own (slower) leak.
    vi.useFakeTimers()
    const done = applyRemoteControlEvent({ kind: 'move', x: 0.5, y: 0.5 })
    reply('{"ok":true}\n')
    await expect(done).resolves.toBe(true)

    await vi.advanceTimersByTimeAsync(5000)
    expect(state.killed).toBe(0)
  })
})
