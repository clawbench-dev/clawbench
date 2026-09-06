import { describe, expect, it, vi, afterEach } from 'vitest'
import { ref, computed, watch, nextTick } from 'vue'
import { useBadgeHighlight, HIGHLIGHT_MS, REDUCED_HIGHLIGHT_MS } from '@/composables/useBadgeHighlight'

// ────────────────────────────────────────────────────────────
// Badge capsule highlight animation — tests the REAL
// useBadgeHighlight composable used by AppHeader.vue.
//
// A badge segment content change (branch / file name / project name) flashes
// the changed segment's accent highlight for HIGHLIGHT_MS (or
// REDUCED_HIGHLIGHT_MS under reduced motion), then fades it off. There is no
// fill/collapse geometry anymore — the full text is shown by the AppHeader
// reveal card, which is the component's concern, not this composable's.
// ────────────────────────────────────────────────────────────

const controllers: ReturnType<typeof makeController>[] = []

function makeController(opts: { reducedMotion?: boolean } = {}) {
  const prefersReducedMotion = ref(opts.reducedMotion ?? false)
  const c = useBadgeHighlight({ prefersReducedMotion })

  function cleanup() {
    c.dispose()
  }

  return {
    highlightBadge: c.highlightBadge,
    pulseBadge: c.pulseBadge,
    segmentClass: c.segmentClass,
    cleanup,
  }
}

function newController(opts?: { reducedMotion?: boolean }) {
  const c = makeController(opts)
  controllers.push(c)
  return c
}

describe('badge highlight animation', () => {
  afterEach(() => {
    for (const c of controllers) c.cleanup()
    controllers.length = 0
    vi.useRealTimers()
  })

  it('should highlight immediately and clear after HIGHLIGHT_MS', async () => {
    vi.useFakeTimers()
    const { highlightBadge, pulseBadge } = newController()
    pulseBadge('branch')

    expect(highlightBadge.value).toBe('branch')

    vi.advanceTimersByTime(HIGHLIGHT_MS)
    expect(highlightBadge.value).toBeNull()
  })

  it('should keep the highlight when a new pulse restarts the window', async () => {
    vi.useFakeTimers()
    const { highlightBadge, pulseBadge } = newController()
    pulseBadge('branch')
    vi.advanceTimersByTime(HIGHLIGHT_MS - 100)

    // A second change in the same segment restarts the full window.
    pulseBadge('branch')
    expect(highlightBadge.value).toBe('branch')

    // Old timer (which would have fired at this point) must NOT clear it.
    vi.advanceTimersByTime(150)
    expect(highlightBadge.value).toBe('branch')

    // New window ends on schedule.
    vi.advanceTimersByTime(HIGHLIGHT_MS - 50)
    expect(highlightBadge.value).toBeNull()
  })

  it('should let a different source take over the highlight mid-window', async () => {
    vi.useFakeTimers()
    const { highlightBadge, pulseBadge } = newController()
    pulseBadge('branch')
    expect(highlightBadge.value).toBe('branch')

    pulseBadge('file')
    expect(highlightBadge.value).toBe('file')

    vi.advanceTimersByTime(HIGHLIGHT_MS)
    expect(highlightBadge.value).toBeNull()
  })

  it('should clear after the previous highlight ends and re-highlight on a new pulse', async () => {
    vi.useFakeTimers()
    const { highlightBadge, pulseBadge } = newController()
    pulseBadge('branch')
    vi.advanceTimersByTime(HIGHLIGHT_MS)
    expect(highlightBadge.value).toBeNull()

    pulseBadge('branch')
    expect(highlightBadge.value).toBe('branch')
  })

  it('should clear pending timers on dispose', async () => {
    vi.useFakeTimers()
    const { highlightBadge, pulseBadge, cleanup } = newController()
    pulseBadge('branch')
    cleanup()
    vi.advanceTimersByTime(HIGHLIGHT_MS * 2)
    expect(highlightBadge.value).toBe('branch') // still highlighted (timer cancelled)
  })

  it('should use the shorter window under reduced motion', async () => {
    vi.useFakeTimers()
    const { highlightBadge, pulseBadge } = newController({ reducedMotion: true })
    pulseBadge('branch')
    expect(highlightBadge.value).toBe('branch')

    // Clears after the short window, before the full window would end.
    vi.advanceTimersByTime(REDUCED_HIGHLIGHT_MS)
    expect(highlightBadge.value).toBeNull()
  })

  it('should expose badge-highlight class only for the active segment', async () => {
    const { highlightBadge, pulseBadge, segmentClass } = newController()
    expect(segmentClass('branch', true)['badge-highlight']).toBe(false)

    pulseBadge('branch')
    expect(segmentClass('branch', true)['badge-highlight']).toBe(true)
    expect(segmentClass('file', true)['badge-highlight']).toBe(false)
    highlightBadge.value = null
  })

  it('should expose no-file state probe on the file segment when no file is open', async () => {
    const { segmentClass } = newController()
    expect(segmentClass('file', false)['no-file']).toBe(true)
    expect(segmentClass('file', true)['no-file']).toBe(false)
    expect(segmentClass('branch', true)['no-file']).toBe(false)
  })

  it('should highlight the file segment when the current file name changes', async () => {
    const fileName = ref('a.ts')
    const { highlightBadge, pulseBadge } = newController()

    watch(fileName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('file')
    })

    fileName.value = 'b.ts'
    await nextTick()
    expect(highlightBadge.value).toBe('file')
  })

  it('should highlight when opening a file while none was open (undefined → name)', async () => {
    const fileName = ref<string | undefined>(undefined)
    const { highlightBadge, pulseBadge } = newController()

    watch(fileName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('file')
    })

    fileName.value = 'main.ts'
    await nextTick()
    expect(highlightBadge.value).toBe('file')
  })

  it('should highlight the project segment when the project name changes', async () => {
    const projectName = ref('proj-a')
    const { highlightBadge, pulseBadge } = newController()

    watch(projectName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('project')
    })

    projectName.value = 'proj-b'
    await nextTick()
    expect(highlightBadge.value).toBe('project')
  })

  it('should highlight the branch segment when the branch changes', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { highlightBadge, pulseBadge } = newController()

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    expect(highlightBadge.value).toBe('branch')
  })

  it('should not highlight when the branch is set to the same value', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { highlightBadge, pulseBadge } = newController()

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'develop'
    await nextTick()
    expect(highlightBadge.value).toBe('branch')
    highlightBadge.value = null

    branchRef.value = 'develop' // same value → watcher does not fire
    await nextTick()
    expect(highlightBadge.value).toBeNull()
  })

  it('should highlight when the branch switches to empty (detached HEAD)', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { highlightBadge, pulseBadge } = newController()

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'develop'
    await nextTick()
    expect(highlightBadge.value).toBe('branch')
    highlightBadge.value = null

    branchRef.value = ''
    await nextTick()
    expect(highlightBadge.value).toBe('branch')
  })
})
