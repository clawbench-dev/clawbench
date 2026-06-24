import { describe, expect, it } from 'vitest'
import { ref, watch, nextTick } from 'vue'

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
