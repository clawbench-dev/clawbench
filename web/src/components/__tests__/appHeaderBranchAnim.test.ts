import { describe, expect, it } from 'vitest'
import { ref, computed, watch, nextTick } from 'vue'

// ────────────────────────────────────────────────────────────
// Badge capsule pulse animation logic test
// Verifies that the watchers correctly toggle badgePulse when
// badge content (git branch / current file name / project name)
// changes, and skip the initial value.
// ────────────────────────────────────────────────────────────

// Mirrors the pulseBadge implementation in AppHeader.vue:
// set false → nextTick → true re-arms the CSS animation, and the
// @animationend handler resets it so a later change can re-trigger.
function makePulseController() {
  const badgePulse = ref(false)
  function pulseBadge() {
    badgePulse.value = false
    nextTick(() => { badgePulse.value = true })
  }
  return { badgePulse, pulseBadge }
}

describe('badge pulse animation', () => {
  it('should set badgePulse to true when gitBranch changes', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { badgePulse, pulseBadge } = makePulseController()

    watch(gitBranch, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) pulseBadge()
    })

    branchRef.value = 'feature/xyz'
    await nextTick()
    await nextTick()
    expect(badgePulse.value).toBe(true)
  })

  it('should set badgePulse to true when the current file name changes', async () => {
    const fileName = ref('a.ts')
    const { badgePulse, pulseBadge } = makePulseController()

    watch(fileName, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) pulseBadge()
    })

    fileName.value = 'b.ts'
    await nextTick()
    await nextTick()
    expect(badgePulse.value).toBe(true)
  })

  it('should set badgePulse to true when the project name changes', async () => {
    const projectName = ref('proj-a')
    const { badgePulse, pulseBadge } = makePulseController()

    watch(projectName, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) pulseBadge()
    })

    projectName.value = 'proj-b'
    await nextTick()
    await nextTick()
    expect(badgePulse.value).toBe(true)
  })

  it('should not pulse on initial value', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { badgePulse, pulseBadge } = makePulseController()

    watch(gitBranch, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) pulseBadge()
    })

    await nextTick()
    expect(badgePulse.value).toBe(false)
  })

  it('should re-trigger after the animation resets (animationend)', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { badgePulse, pulseBadge } = makePulseController()

    watch(gitBranch, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) pulseBadge()
    })

    branchRef.value = 'develop'
    await nextTick()
    await nextTick()
    expect(badgePulse.value).toBe(true)

    // @animationend resets the flag to false
    badgePulse.value = false
    await nextTick()

    branchRef.value = 'feature/abc'
    await nextTick()
    await nextTick()
    expect(badgePulse.value).toBe(true)
  })

  it('should not pulse when the branch is set to the same value', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { badgePulse, pulseBadge } = makePulseController()

    watch(gitBranch, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) pulseBadge()
    })

    branchRef.value = 'develop'
    await nextTick()
    await nextTick()
    badgePulse.value = false
    await nextTick()

    branchRef.value = 'develop'
    await nextTick()
    await nextTick()
    expect(badgePulse.value).toBe(false)
  })

  it('should pulse when the branch switches to empty (detached HEAD)', async () => {
    const branchRef = ref('main')
    const gitBranch = computed(() => branchRef.value)
    const { badgePulse, pulseBadge } = makePulseController()

    watch(gitBranch, (newVal, oldVal) => {
      if (oldVal !== undefined && newVal !== oldVal) pulseBadge()
    })

    branchRef.value = 'develop'
    await nextTick()
    await nextTick()
    badgePulse.value = false
    await nextTick()

    branchRef.value = ''
    await nextTick()
    await nextTick()
    expect(badgePulse.value).toBe(true)
  })
})
