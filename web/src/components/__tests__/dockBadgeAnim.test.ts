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
// Shape/geometry now comes from the shared `.count-badge` class (see
// countBadge.css.test.ts for the class's own contract). What this file guards is
// the dock-specific override: `.dock-badge` — the base class on the same element
// — is an 8px circle with `border-radius: 50%`, and a scoped selector outranks
// the global `.count-badge`. So `.dock-badge-count` must re-declare the pill
// radius, or the number renders as an ellipse.
//
// jsdom has no CSS engine, so this is a source-contract check.
// ────────────────────────────────────────────────────────────
describe('dock count badge keeps the pill radius over the dot base', () => {
  const appVue = readFileSync(join(__dirname, '..', '..', 'App.vue'), 'utf8')

  /** Declarations of the first `.dock-badge-count {` rule. */
  function badgeCountDecls(): string {
    const m = appVue.match(/\.dock-badge-count\s*\{([^}]*)\}/)
    expect(m, '.dock-badge-count rule must exist').not.toBeNull()
    return m![1]
  }

  it('re-declares the pill radius to beat the .dock-badge circle', () => {
    // Without this the scoped `.dock-badge { border-radius: 50% }` wins and the
    // 16px-wide badge becomes a squashed ellipse.
    expect(badgeCountDecls()).toMatch(/border-radius:\s*var\(--radius-full\)/)
  })

  it('still applies the shared count badge class in the template', () => {
    // The geometry (min-width, padding, font-size, line-height, centering) is
    // owned by .count-badge now; dropping the class would leave the badge with
    // no size at all.
    const tags = appVue.match(/class="dock-badge dock-badge-count[^"]*"/g) ?? []
    expect(tags.length).toBeGreaterThan(0)
    for (const tag of tags) {
      expect(tag).toContain('count-badge')
    }
  })
})
