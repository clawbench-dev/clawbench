import { describe, expect, it, vi, afterEach } from 'vitest'
import { ref, computed, watch, nextTick } from 'vue'
import { useBadgeHighlight, decideHighlightShape, HIGHLIGHT_PRE_MS, FILL_MS, HIGHLIGHT_POST_MS, HIGHLIGHT_NO_FILL_MS } from '@/composables/useBadgeHighlight'

// ────────────────────────────────────────────────────────────
// Badge capsule highlight animation logic — tests the REAL
// useBadgeHighlight composable used by AppHeader.vue.
//
// Staged timeline on a badge segment content change (branch / file name /
// project name):
//   1. HIGHLIGHT_PRE_MS  — changed segment highlights (accent background),
//      ALWAYS.
//   2. Only when the capsule is space-constrained (content overflows, i.e.
//      text truncated) does the segment FILL the capsule (others slide shut).
//   3. FILL_MS / HIGHLIGHT_POST_MS — everything expands back.
//   4. finally the highlight fades out.
//
// The measurement step normally runs inside a single requestAnimationFrame
// slot (which avoids forcing a synchronous reflow in the DOM-write frame).
// These tests drive the same single-slot contract deterministically via
// flushPendingMeasurement() — one measurement per pulse, seq-guarded, exactly
// like the rAF callback in the composable. Reduced-motion fast-path: no fill,
// short static highlight only.
// ────────────────────────────────────────────────────────────

const TOTAL_MS = HIGHLIGHT_PRE_MS + FILL_MS + HIGHLIGHT_POST_MS

interface SpanInfo { scrollWidth: number; clientWidth: number }
interface PosInfo { left: number; width: number }

/**
 * Build a controller backed by the real composable with injectable
 * measurement dependencies (spans / positions / capsule width), so the
 * tests can run without DOM layout.
 */
function makeController(
  spans: Record<string, SpanInfo> | null = null,
  positions: Record<string, PosInfo> | null = null,
  capsuleWidth = 200,
  opts: { reducedMotion?: boolean } = {},
) {
  const prefersReducedMotion = ref(opts.reducedMotion ?? false)
  const capsuleRef = ref<HTMLElement | null>(null)
  const overflowing = (src: 'project' | 'branch' | 'file'): boolean => {
    const s = spans?.[src]
    if (!s) return false
    return s.scrollWidth > s.clientWidth + 1
  }
  const measureShape = (src: 'project' | 'branch' | 'file'): 'left' | 'right' | 'none' => {
    const pos = positions?.[src]
    if (!pos) return 'none'
    const right = pos.left + pos.width
    if (pos.left <= 2) return 'left'
    if (right >= capsuleWidth - 2) return 'right'
    return 'none'
  }
  const c = useBadgeHighlight({
    capsuleRef,
    prefersReducedMotion,
    overflowing: (_cap, source) => overflowing(source),
    measureShape: (_cap, source) => measureShape(source),
  })

  function cleanup() {
    c.dispose()
  }

  return {
    highlightBadge: c.highlightBadge,
    fillBadge: c.fillBadge,
    highlightRadius: c.highlightRadius,
    pulseBadge: c.pulseBadge,
    flushPendingMeasurement: c.flushPendingMeasurement,
    cleanup,
  }
}

const controllers: ReturnType<typeof makeController>[] = []
function newController(
  spans?: Record<string, SpanInfo> | null,
  positions?: Record<string, PosInfo> | null,
  capsuleWidth?: number,
  opts?: { reducedMotion?: boolean },
) {
  const c = makeController(spans, positions, capsuleWidth, opts)
  controllers.push(c)
  return c
}

describe('badge highlight animation', () => {
  afterEach(() => {
    for (const c of controllers) c.cleanup()
    controllers.length = 0
    vi.useRealTimers()
  })

  it('should highlight ALWAYS and fill only when the capsule overflows', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    // Overflowing capsule
    const { highlightBadge, fillBadge, pulseBadge, flushPendingMeasurement } = newController({ branch: { scrollWidth: 300, clientWidth: 200 } })

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    // Stage 1: highlighted immediately (before the deferred rAF measurement)
    expect(highlightBadge.value).toBe('branch')
    expect(fillBadge.value).toBeNull()

    // rAF frame fires → measurement schedules the fill after the pre-delay
    flushPendingMeasurement()

    // Stage 2: fills after the pre-delay (because overflowing)
    vi.advanceTimersByTime(HIGHLIGHT_PRE_MS)
    expect(fillBadge.value).toBe('branch')

    // Stage 3: expands back after the fill window...
    vi.advanceTimersByTime(FILL_MS)
    expect(fillBadge.value).toBeNull()
    // ...highlight still on for HIGHLIGHT_POST_MS
    expect(highlightBadge.value).toBe('branch')

    // Stage 4: highlight finally drops
    vi.advanceTimersByTime(HIGHLIGHT_POST_MS)
    expect(highlightBadge.value).toBeNull()
  })

  it('should round the left side when the highlighted segment touches the capsule left edge', async () => {
    const fileName = ref('a.ts')
    // Segment at the left edge of a 200px capsule
    const { highlightRadius, pulseBadge, flushPendingMeasurement } = newController(
      null,
      { file: { left: 0, width: 60 } },
      200,
    )

    watch(fileName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('file')
    })

    fileName.value = 'b.ts'
    await nextTick()
    flushPendingMeasurement() // deferred rAF measurement
    expect(highlightRadius.value).toBe('left')
  })

  it('should round the right side when the highlighted segment touches the capsule right edge', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    // Segment flush against the right edge of a 200px capsule
    const { highlightRadius, pulseBadge, flushPendingMeasurement } = newController(
      null,
      { branch: { left: 140, width: 60 } },
      200,
    )

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    flushPendingMeasurement()
    expect(highlightRadius.value).toBe('right')
  })

  it('should stay rectangular when the highlighted segment is in the middle', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    // Segment centered between both edges of a 200px capsule
    const { highlightRadius, pulseBadge, flushPendingMeasurement } = newController(
      null,
      { branch: { left: 70, width: 60 } },
      200,
    )

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    flushPendingMeasurement()
    expect(highlightRadius.value).toBe('none')
  })

  it('should reset the highlight radius when the highlight ends', async () => {
    vi.useFakeTimers()
    const fileName = ref('a.ts')
    const { highlightRadius, pulseBadge, flushPendingMeasurement } = newController(
      null,
      { file: { left: 0, width: 60 } },
      200,
    )

    watch(fileName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('file')
    })

    fileName.value = 'b.ts'
    await nextTick()
    flushPendingMeasurement()
    expect(highlightRadius.value).toBe('left')

    vi.advanceTimersByTime(TOTAL_MS)
    expect(highlightRadius.value).toBeNull()
  })

  it('should highlight but NOT fill when the capsule has free space', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    // No overflow → never fills
    const { highlightBadge, fillBadge, pulseBadge, flushPendingMeasurement } = newController({ branch: { scrollWidth: 100, clientWidth: 200 } })

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    flushPendingMeasurement() // measurement sees no overflow → no fill scheduled
    expect(highlightBadge.value).toBe('branch')
    expect(fillBadge.value).toBeNull()

    // Even after the pre-delay + fill window, no fill happens
    vi.advanceTimersByTime(HIGHLIGHT_PRE_MS + FILL_MS)
    expect(fillBadge.value).toBeNull()

    // Highlight clears via the short no-fill window (already elapsed above)
    expect(highlightBadge.value).toBeNull()
  })

  it('should highlight without capsule measurement when no capsule is provided (jsdom)', async () => {
    const fileName = ref('a.ts')
    const { highlightBadge, fillBadge, pulseBadge, flushPendingMeasurement } = newController()

    watch(fileName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('file')
    })

    fileName.value = 'b.ts'
    await nextTick()
    flushPendingMeasurement() // no spans → nothing overflows → no fill
    expect(highlightBadge.value).toBe('file')
    expect(fillBadge.value).toBeNull()
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

  it('should not highlight on initial value', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { highlightBadge, fillBadge, pulseBadge } = newController({ branch: { scrollWidth: 300, clientWidth: 200 } })

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    await nextTick()
    expect(highlightBadge.value).toBeNull()
    expect(fillBadge.value).toBeNull()
  })

  it('should reset the staged timeline when a new change arrives mid-window', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { highlightBadge, fillBadge, pulseBadge, flushPendingMeasurement } = newController({ branch: { scrollWidth: 300, clientWidth: 200 } })

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'develop'
    await nextTick()
    flushPendingMeasurement()
    expect(highlightBadge.value).toBe('branch')

    // Mid-fill a new change arrives — timeline restarts; the old fill clears
    vi.advanceTimersByTime(HIGHLIGHT_PRE_MS + 300)
    branchRef.value = 'feature/abc'
    await nextTick()
    flushPendingMeasurement() // the new pulse's rAF measurement schedules its own fill
    expect(highlightBadge.value).toBe('branch')
    // Old fill was reset; new animation hasn't reached its fill stage yet
    expect(fillBadge.value).toBeNull()

    // New animation fills after its own pre-delay
    vi.advanceTimersByTime(HIGHLIGHT_PRE_MS)
    expect(fillBadge.value).toBe('branch')

    vi.advanceTimersByTime(FILL_MS + HIGHLIGHT_POST_MS)
    expect(highlightBadge.value).toBeNull()
    expect(fillBadge.value).toBeNull()
  })

  it('should re-highlight after the previous highlight ends', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { highlightBadge, pulseBadge, flushPendingMeasurement } = newController({ branch: { scrollWidth: 300, clientWidth: 200 } })

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'develop'
    await nextTick()
    flushPendingMeasurement()
    expect(highlightBadge.value).toBe('branch')

    vi.advanceTimersByTime(TOTAL_MS)
    expect(highlightBadge.value).toBeNull()

    branchRef.value = 'feature/abc'
    await nextTick()
    flushPendingMeasurement()
    expect(highlightBadge.value).toBe('branch')
  })

  it('should not highlight when the branch is set to the same value', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { highlightBadge, pulseBadge, flushPendingMeasurement } = newController()

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'develop'
    await nextTick()
    // Let the first pulse's highlight end naturally (short no-fill window).
    flushPendingMeasurement()
    vi.advanceTimersByTime(HIGHLIGHT_NO_FILL_MS)
    expect(highlightBadge.value).toBeNull()

    branchRef.value = 'develop' // same value → watcher does not fire
    await nextTick()
    expect(highlightBadge.value).toBeNull()
  })

  it('should highlight when the branch switches to empty (detached HEAD)', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { highlightBadge, pulseBadge, flushPendingMeasurement } = newController()

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'develop'
    await nextTick()
    // Let the first pulse's highlight end naturally (short no-fill window).
    flushPendingMeasurement()
    vi.advanceTimersByTime(HIGHLIGHT_NO_FILL_MS)
    expect(highlightBadge.value).toBeNull()

    branchRef.value = ''
    await nextTick()
    expect(highlightBadge.value).toBe('branch')
  })

  it('should clear a stale fill when a DIFFERENT source changes mid-fill', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const fileName = ref('a.ts')
    const { highlightBadge, fillBadge, pulseBadge, flushPendingMeasurement } = newController({ branch: { scrollWidth: 300, clientWidth: 200 }, file: { scrollWidth: 100, clientWidth: 200 } })

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })
    watch(fileName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('file')
    })

    // branch fills the capsule
    branchRef.value = 'feature/xyz'
    await nextTick()
    flushPendingMeasurement()
    vi.advanceTimersByTime(HIGHLIGHT_PRE_MS)
    expect(fillBadge.value).toBe('branch')

    // file changes mid-fill → the old branch fill must clear immediately
    fileName.value = 'b.ts'
    await nextTick()
    flushPendingMeasurement()
    expect(highlightBadge.value).toBe('file')
    expect(fillBadge.value).toBeNull()
  })

  it('should not schedule a stale fill when a later change supersedes a pending measurement', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const fileName = ref('a.ts')
    // branch overflows → would fill; file has free space → must not fill
    const { highlightBadge, fillBadge, pulseBadge, flushPendingMeasurement } = newController({
      branch: { scrollWidth: 300, clientWidth: 200 },
      file: { scrollWidth: 100, clientWidth: 200 },
    })

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })
    watch(fileName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('file')
    })

    // Both change in the same flush: branch (would fill) then file (won't).
    // The second pulse clears the branch measurement before the rAF frame —
    // exactly the case where the deferred read must NOT resurrect a stale fill.
    branchRef.value = 'feature/xyz'
    await nextTick()
    fileName.value = 'b.ts' // supersedes the branch change before the frame
    await nextTick()
    flushPendingMeasurement() // only the file measurement runs (file has free space)

    expect(highlightBadge.value).toBe('file')
    // The stale branch fill must NOT appear
    vi.advanceTimersByTime(HIGHLIGHT_PRE_MS)
    expect(fillBadge.value).toBeNull()
    vi.advanceTimersByTime(HIGHLIGHT_NO_FILL_MS)
    expect(highlightBadge.value).toBeNull()
  })

  it('should only show a short static highlight (no fill) under reduced motion', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    // Even with an overflowing capsule, reduced motion must NOT fill
    const { highlightBadge, fillBadge, pulseBadge, flushPendingMeasurement } = newController(
      { branch: { scrollWidth: 300, clientWidth: 200 } },
      null,
      200,
      { reducedMotion: true },
    )

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    // No deferred measurement is scheduled at all under reduced motion.
    flushPendingMeasurement() // no-op
    expect(highlightBadge.value).toBe('branch')
    expect(fillBadge.value).toBeNull()

    // No fill ever starts
    vi.advanceTimersByTime(HIGHLIGHT_PRE_MS + FILL_MS)
    expect(fillBadge.value).toBeNull()

    // Highlight drops after the short no-fill window
    vi.advanceTimersByTime(HIGHLIGHT_NO_FILL_MS)
    expect(highlightBadge.value).toBeNull()
  })
})

describe('decideHighlightShape', () => {
  it('returns left when touching the capsule left edge', () => {
    expect(decideHighlightShape(0, 60, 200)).toBe('left')
    expect(decideHighlightShape(2, 62, 200)).toBe('left') // threshold inclusive
  })
  it('returns right when touching the capsule right edge', () => {
    expect(decideHighlightShape(140, 200, 200)).toBe('right')
    expect(decideHighlightShape(138, 198, 200)).toBe('right') // threshold inclusive
  })
  it('returns none when in the middle', () => {
    expect(decideHighlightShape(70, 130, 200)).toBe('none')
  })
})
