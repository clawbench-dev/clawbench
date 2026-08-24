import { describe, expect, it, vi, afterEach } from 'vitest'
import { ref, computed, watch, nextTick } from 'vue'

// ────────────────────────────────────────────────────────────
// Badge capsule highlight animation logic test
//
// Mirrors the implementation in AppHeader.vue. Staged timeline on a badge
// segment content change (branch / file name / project name):
//   1. HIGHLIGHT_PRE_MS  — changed segment highlights (accent background),
//      ALWAYS.
//   2. Only when the capsule is space-constrained (content overflows, i.e.
//      text truncated) does the segment FILL the capsule (others slide shut).
//   3. FILL_MS / HIGHLIGHT_POST_MS — everything expands back.
//   4. finally the highlight fades out.
// ────────────────────────────────────────────────────────────

const HIGHLIGHT_PRE_MS = 200
const FILL_MS = 1000
const HIGHLIGHT_POST_MS = 200
const TOTAL_MS = HIGHLIGHT_PRE_MS + FILL_MS + HIGHLIGHT_POST_MS

function makeController(
  spans: { scrollWidth: number; clientWidth: number }[] | null = null,
  positions: Record<string, { left: number; width: number }> | null = null,
  capsuleWidth = 200,
) {
  const highlightBadge = ref<'project' | 'branch' | 'file' | null>(null)
  const fillBadge = ref<'project' | 'branch' | 'file' | null>(null)
  const highlightRadius = ref<'left' | 'right' | 'none' | null>(null)
  let highlightTimer: ReturnType<typeof setTimeout> | null = null
  let fillTimer: ReturnType<typeof setTimeout> | null = null
  let clearTimer: ReturnType<typeof setTimeout> | null = null

  function clearTimers() {
    if (highlightTimer) { clearTimeout(highlightTimer); highlightTimer = null }
    if (fillTimer) { clearTimeout(fillTimer); fillTimer = null }
    if (clearTimer) { clearTimeout(clearTimer); clearTimer = null }
  }

  function capsuleOverflowing(): boolean {
    if (!spans) return false
    return spans.some(s => s.scrollWidth > s.clientWidth + 1)
  }

  function measureHighlightShape(source: 'project' | 'branch' | 'file') {
    const pos = positions?.[source]
    if (!pos) return
    const right = pos.left + pos.width
    if (pos.left <= 2) highlightRadius.value = 'left'
    else if (right >= capsuleWidth - 2) highlightRadius.value = 'right'
    else highlightRadius.value = 'none'
  }

  function pulseBadge(source: 'project' | 'branch' | 'file') {
    clearTimers()

    highlightBadge.value = source

    nextTick(() => {
      measureHighlightShape(source)
      if (capsuleOverflowing()) {
        fillTimer = setTimeout(() => {
          fillBadge.value = source
          fillTimer = null

          clearTimer = setTimeout(() => {
            fillBadge.value = null
            highlightBadge.value = null
            highlightRadius.value = null
            clearTimer = null
          }, FILL_MS)
        }, HIGHLIGHT_PRE_MS)
      }
    })

    highlightTimer = setTimeout(() => {
      fillBadge.value = null
      highlightBadge.value = null
      highlightRadius.value = null
      highlightTimer = null
    }, TOTAL_MS)
  }

  function cleanup() {
    clearTimers()
  }

  return { highlightBadge, fillBadge, highlightRadius, pulseBadge, cleanup }
}

const controllers: ReturnType<typeof makeController>[] = []
function newController(
  spans?: { scrollWidth: number; clientWidth: number }[] | null,
  positions?: Record<string, { left: number; width: number }> | null,
  capsuleWidth?: number,
) {
  const c = makeController(spans, positions, capsuleWidth)
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
    const { highlightBadge, fillBadge, pulseBadge } = newController([{ scrollWidth: 300, clientWidth: 200 }])

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    await nextTick()
    // Stage 1: highlighted immediately
    expect(highlightBadge.value).toBe('branch')
    expect(fillBadge.value).toBeNull()

    // Stage 2: fills after the pre-delay (because overflowing)
    vi.advanceTimersByTime(HIGHLIGHT_PRE_MS)
    expect(fillBadge.value).toBe('branch')

    // Stage 3+4: expands back and highlight drops
    vi.advanceTimersByTime(FILL_MS)
    expect(fillBadge.value).toBeNull()
    expect(highlightBadge.value).toBeNull()
  })

  it('should round the left side when the highlighted segment touches the capsule left edge', async () => {
    const fileName = ref('a.ts')
    // Segment at the left edge of a 200px capsule
    const { highlightBadge, highlightRadius, pulseBadge } = newController(
      null,
      { file: { left: 0, width: 60 } },
      200,
    )

    watch(fileName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('file')
    })

    fileName.value = 'b.ts'
    await nextTick()
    await nextTick()
    expect(highlightRadius.value).toBe('left')
  })

  it('should round the right side when the highlighted segment touches the capsule right edge', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    // Segment flush against the right edge of a 200px capsule
    const { highlightBadge, highlightRadius, pulseBadge } = newController(
      null,
      { branch: { left: 140, width: 60 } },
      200,
    )

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    await nextTick()
    expect(highlightRadius.value).toBe('right')
  })

  it('should stay rectangular when the highlighted segment is in the middle', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    // Segment centered between both edges of a 200px capsule
    const { highlightBadge, highlightRadius, pulseBadge } = newController(
      null,
      { branch: { left: 70, width: 60 } },
      200,
    )

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    await nextTick()
    expect(highlightRadius.value).toBe('none')
  })

  it('should reset the highlight radius when the highlight ends', async () => {
    vi.useFakeTimers()
    const fileName = ref('a.ts')
    const { highlightBadge, highlightRadius, pulseBadge } = newController(
      null,
      { file: { left: 0, width: 60 } },
      200,
    )

    watch(fileName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('file')
    })

    fileName.value = 'b.ts'
    await nextTick()
    await nextTick()
    expect(highlightRadius.value).toBe('left')

    vi.advanceTimersByTime(TOTAL_MS)
    expect(highlightRadius.value).toBeNull()
  })

  it('should highlight but NOT fill when the capsule has free space', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    // No overflow → never fills
    const { highlightBadge, fillBadge, pulseBadge } = newController([{ scrollWidth: 100, clientWidth: 200 }])

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    await nextTick()
    expect(highlightBadge.value).toBe('branch')
    expect(fillBadge.value).toBeNull()

    // Even after the pre-delay + fill window, no fill happens
    vi.advanceTimersByTime(HIGHLIGHT_PRE_MS + FILL_MS)
    expect(fillBadge.value).toBeNull()

    // Highlight still clears via the safety net
    vi.advanceTimersByTime(HIGHLIGHT_POST_MS)
    expect(highlightBadge.value).toBeNull()
  })

  it('should highlight without capsule measurement when no capsule is provided (jsdom)', async () => {
    const fileName = ref('a.ts')
    const { highlightBadge, fillBadge, pulseBadge } = newController()

    watch(fileName, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('file')
    })

    fileName.value = 'b.ts'
    await nextTick()
    await nextTick()
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
    const { highlightBadge, fillBadge, pulseBadge } = newController([{ scrollWidth: 300, clientWidth: 200 }])

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
    const { highlightBadge, fillBadge, pulseBadge } = newController([{ scrollWidth: 300, clientWidth: 200 }])

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'develop'
    await nextTick()
    await nextTick()
    expect(highlightBadge.value).toBe('branch')

    // Mid-fill a new change arrives — timeline restarts
    vi.advanceTimersByTime(HIGHLIGHT_PRE_MS + 300)
    branchRef.value = 'feature/abc'
    await nextTick()
    await nextTick()
    expect(highlightBadge.value).toBe('branch')
    expect(fillBadge.value).toBe('branch')

    vi.advanceTimersByTime(TOTAL_MS)
    expect(highlightBadge.value).toBeNull()
    expect(fillBadge.value).toBeNull()
  })

  it('should re-highlight after the previous highlight ends', async () => {
    vi.useFakeTimers()
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { highlightBadge, pulseBadge } = newController([{ scrollWidth: 300, clientWidth: 200 }])

    watch(gitBranch, (newVal, oldVal) => {
      if (newVal !== oldVal) pulseBadge('branch')
    })

    branchRef.value = 'develop'
    await nextTick()
    await nextTick()
    expect(highlightBadge.value).toBe('branch')

    vi.advanceTimersByTime(TOTAL_MS)
    expect(highlightBadge.value).toBeNull()

    branchRef.value = 'feature/abc'
    await nextTick()
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
    highlightBadge.value = null
    await nextTick()

    branchRef.value = 'develop'
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
    highlightBadge.value = null
    await nextTick()

    branchRef.value = ''
    await nextTick()
    expect(highlightBadge.value).toBe('branch')
  })
})
