import { describe, expect, it } from 'vitest'
import { ref, watch, nextTick } from 'vue'
import { readFileSync } from 'fs'
import { join } from 'path'

// ────────────────────────────────────────────────────────────
// Dock badge change animation logic test
// Verifies that triggerBadgeAnim correctly toggles the
// animation ref when badge count sources change.
// ────────────────────────────────────────────────────────────

describe('dock badge change animation', () => {
  it('should animate when chatUnreadCount changes', async () => {
    const countRef = ref(0)
    const animRef = ref(false)

    function triggerBadgeAnim(ar) {
      ar.value = false
      nextTick(() => { ar.value = true })
    }

    watch(countRef, (n, o) => {
      if (o !== undefined && n !== o) triggerBadgeAnim(animRef)
    })

    countRef.value = 3
    await nextTick()
    await nextTick()
    expect(animRef.value).toBe(true)
  })

  it('should not animate on initial value', async () => {
    const countRef = ref(5)
    const animRef = ref(false)

    function triggerBadgeAnim(ar) {
      ar.value = false
      nextTick(() => { ar.value = true })
    }

    watch(countRef, (n, o) => {
      if (o !== undefined && n !== o) triggerBadgeAnim(animRef)
    })

    await nextTick()
    await nextTick()
    expect(animRef.value).toBe(false)
  })

  it('should animate when count decreases', async () => {
    const countRef = ref(5)
    const animRef = ref(false)

    function triggerBadgeAnim(ar) {
      ar.value = false
      nextTick(() => { ar.value = true })
    }

    watch(countRef, (n, o) => {
      if (o !== undefined && n !== o) triggerBadgeAnim(animRef)
    })

    countRef.value = 6
    await nextTick()
    await nextTick()
    animRef.value = false
    await nextTick()

    countRef.value = 2
    await nextTick()
    await nextTick()
    expect(animRef.value).toBe(true)
  })

  it('should trigger overflow animation alongside primary badge', async () => {
    const taskCount = ref(0)
    const taskAnim = ref(false)
    const overflowAnim = ref(false)

    function triggerBadgeAnim(ar) {
      ar.value = false
      nextTick(() => { ar.value = true })
    }

    watch(taskCount, (n, o) => {
      if (o !== undefined && n !== o) {
        triggerBadgeAnim(taskAnim)
        triggerBadgeAnim(overflowAnim)
      }
    })

    taskCount.value = 1
    await nextTick()
    await nextTick()
    expect(taskAnim.value).toBe(true)
    expect(overflowAnim.value).toBe(true)
  })

  it('should re-trigger on subsequent changes', async () => {
    const countRef = ref(0)
    const animRef = ref(false)

    function triggerBadgeAnim(ar) {
      ar.value = false
      nextTick(() => { ar.value = true })
    }

    watch(countRef, (n, o) => {
      if (o !== undefined && n !== o) triggerBadgeAnim(animRef)
    })

    countRef.value = 1
    await nextTick()
    await nextTick()
    expect(animRef.value).toBe(true)

    animRef.value = false
    await nextTick()

    countRef.value = 2
    await nextTick()
    await nextTick()
    expect(animRef.value).toBe(true)
  })

  it('should not animate when set to same value', async () => {
    const countRef = ref(0)
    const animRef = ref(false)

    function triggerBadgeAnim(ar) {
      ar.value = false
      nextTick(() => { ar.value = true })
    }

    watch(countRef, (n, o) => {
      if (o !== undefined && n !== o) triggerBadgeAnim(animRef)
    })

    countRef.value = 3
    await nextTick()
    await nextTick()
    animRef.value = false
    await nextTick()

    countRef.value = 3
    await nextTick()
    await nextTick()
    expect(animRef.value).toBe(false)
  })
})

// ────────────────────────────────────────────────────────────
// Dock badge shape contract
//
// The count badge is a 16px-tall box (min-width 16px / line-height 16px).
// A square-ish radius (the old `--radius-sm` = 6px) read as a blocky chip;
// it must be a pill — radius at least half the box height, i.e. the two
// vertical edges are full semicircles. jsdom has no CSS engine, so this is
// a source-contract check.
// ────────────────────────────────────────────────────────────
describe('dock count badge is a pill', () => {
  const appVue = readFileSync(join(__dirname, '..', '..', 'App.vue'), 'utf8')
  const variables = readFileSync(
    join(__dirname, '..', '..', '..', 'css', 'variables.css'),
    'utf8',
  )

  /** Declarations of the first `.dock-badge-count {` rule. */
  function badgeCountDecls(): string {
    const m = appVue.match(/\.dock-badge-count\s*\{([^}]*)\}/)
    expect(m, '.dock-badge-count rule must exist').not.toBeNull()
    return m![1]
  }

  it('uses the pill radius token, not a fixed corner radius', () => {
    expect(badgeCountDecls()).toMatch(/border-radius:\s*var\(--radius-full\)/)
  })

  it('radius token is at least half the badge height', () => {
    // Guards the shape, not the name: redefining --radius-full to something
    // small would silently square the badge off again.
    const radius = Number(
      variables.match(/--radius-full:\s*(\d+)px/)?.[1] ?? NaN,
    )
    const height = Number(badgeCountDecls().match(/line-height:\s*(\d+)px/)?.[1])
    expect(height).toBeGreaterThan(0)
    expect(radius).toBeGreaterThanOrEqual(height / 2)
  })
})
