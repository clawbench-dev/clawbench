import { describe, expect, it } from 'vitest'
import { ref, computed, watch, nextTick } from 'vue'

// ────────────────────────────────────────────────────────────
// Branch switch animation logic test
// Verifies that the watcher correctly toggles branchAnimating
// when gitBranch changes, and skips the initial value.
// ────────────────────────────────────────────────────────────

describe('branch switch animation', () => {
  it('should set branchAnimating to true when gitBranch changes', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const branchAnimating = ref(false)

    watch(gitBranch, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) {
        branchAnimating.value = false
        nextTick(() => { branchAnimating.value = true })
      }
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    await nextTick()
    expect(branchAnimating.value).toBe(true)
  })

  it('should not animate on initial value', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const branchAnimating = ref(false)

    watch(gitBranch, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) {
        branchAnimating.value = false
        nextTick(() => { branchAnimating.value = true })
      }
    })

    await nextTick()
    expect(branchAnimating.value).toBe(false)
  })

  it('should re-trigger animation on subsequent branch changes', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const branchAnimating = ref(false)

    watch(gitBranch, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) {
        branchAnimating.value = false
        nextTick(() => { branchAnimating.value = true })
      }
    })

    branchRef.value = 'develop'
    await nextTick()
    await nextTick()
    expect(branchAnimating.value).toBe(true)

    branchAnimating.value = false
    await nextTick()

    branchRef.value = 'feature/abc'
    await nextTick()
    await nextTick()
    expect(branchAnimating.value).toBe(true)
  })

  it('should not animate when branch is set to the same value', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const branchAnimating = ref(false)

    watch(gitBranch, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) {
        branchAnimating.value = false
        nextTick(() => { branchAnimating.value = true })
      }
    })

    branchRef.value = 'develop'
    await nextTick()
    await nextTick()
    branchAnimating.value = false
    await nextTick()

    branchRef.value = 'develop'
    await nextTick()
    await nextTick()
    expect(branchAnimating.value).toBe(false)
  })

  it('should animate when branch switches to empty (detached HEAD)', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const branchAnimating = ref(false)

    watch(gitBranch, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) {
        branchAnimating.value = false
        nextTick(() => { branchAnimating.value = true })
      }
    })

    branchRef.value = 'develop'
    await nextTick()
    await nextTick()
    branchAnimating.value = false
    await nextTick()

    branchRef.value = ''
    await nextTick()
    await nextTick()
    expect(branchAnimating.value).toBe(true)
  })
})
