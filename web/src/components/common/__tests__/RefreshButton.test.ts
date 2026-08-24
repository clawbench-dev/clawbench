/**
 * RefreshButton rotation tests.
 *
 * Rotation is driven by the Web Animations API (svg.animate) with a
 * "finish to the next whole revolution" policy: when `loading` flips to false
 * the animation keeps running until a 360° boundary, then is cancelled — it
 * never freezes mid-turn.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import RefreshButton from '@/components/common/RefreshButton.vue'

// ── WAAPI stub ──
// jsdom has no Web Animations API. Fake Element.prototype.animate so the
// component can start/read/cancel animations.
interface MockAnimation {
  currentTime: number
  cancel: ReturnType<typeof vi.fn>
  duration: number
}

const animateMock = vi.fn()
const animations: MockAnimation[] = []
// The svg element each animation was started on (the `this` of svg.animate).
const animatedEls: (Element | null)[] = []

function installAnimateMock() {
  animateMock.mockReset()
  animations.length = 0
  animatedEls.length = 0
  ;(Element.prototype as any).animate = animateMock.mockImplementation(function (this: Element, _keyframes: any, opts: any) {
    const anim: MockAnimation = {
      currentTime: 0,
      cancel: vi.fn(),
      duration: opts?.duration ?? 0,
    }
    animations.push(anim)
    animatedEls.push(this)
    return anim
  })
}

function latestAnim(): MockAnimation | undefined {
  return animations[animations.length - 1]
}

describe('RefreshButton', () => {
  beforeEach(() => {
    installAnimateMock()
  })

  afterEach(() => {
    vi.useRealTimers()
    delete (Element.prototype as any).animate
  })

  function mountBtn(props = {}) {
    return mount(RefreshButton, {
      props: { icon: 'RefreshCw', ...props },
    })
  }

  it('starts an infinite 0.5s-per-revolution animation while loading', async () => {
    const wrapper = mountBtn()
    expect(animateMock).not.toHaveBeenCalled()

    await wrapper.setProps({ loading: true })
    await nextTick()

    expect(animateMock).toHaveBeenCalledTimes(1)
    const [keyframes, opts] = animateMock.mock.calls[0] as any[]
    expect(keyframes).toHaveLength(2)
    expect(keyframes[0].transform).toBe('rotate(0deg)')
    expect(keyframes[1].transform).toBe('rotate(360deg)')
    expect(opts.duration).toBe(500)
    expect(opts.iterations).toBe(Infinity)
    expect(opts.easing).toBe('linear')
  })

  it('finishes the current turn before cancelling, landing on a whole revolution', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    await wrapper.setProps({ loading: true })
    await nextTick()

    const anim = latestAnim()
    expect(anim).toBeTruthy()

    // Load stops after 200ms of a 500ms turn (40% — mid-rotation)
    anim!.currentTime = 200
    await wrapper.setProps({ loading: false })
    await nextTick()

    // Not cancelled yet — it must finish the remaining 60% of the turn first
    expect(anim!.cancel).not.toHaveBeenCalled()
    vi.advanceTimersByTime(299)
    expect(anim!.cancel).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1) // 300ms total: exactly the remaining time
    expect(anim!.cancel).toHaveBeenCalledTimes(1)
  })

  it('guarantees a full revolution even for an instant load', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    await wrapper.setProps({ loading: true })
    await nextTick()

    const anim = latestAnim()
    // Load was essentially instant — the animation barely progressed
    anim!.currentTime = 5
    await wrapper.setProps({ loading: false })
    await nextTick()

    // Remaining ~495ms (not the raw ~5ms-to-boundary), no early cancel
    expect(anim!.cancel).not.toHaveBeenCalled()
    vi.advanceTimersByTime(494)
    expect(anim!.cancel).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(anim!.cancel).toHaveBeenCalledTimes(1)
  })

  it('reuses one animation across a rapid re-toggle without leaking timers', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    await wrapper.setProps({ loading: true })
    await nextTick()
    expect(animateMock).toHaveBeenCalledTimes(1)

    // loading false → true again quickly, before the finish timer fires
    await wrapper.setProps({ loading: false })
    await nextTick()
    await wrapper.setProps({ loading: true })
    await nextTick()

    // Same animation reused — no second svg.animate call, no premature cancel
    expect(animateMock).toHaveBeenCalledTimes(1)
    expect(latestAnim()!.cancel).not.toHaveBeenCalled()

    // Final stop still finishes the turn then cancels exactly once
    await wrapper.setProps({ loading: false })
    await nextTick()
    vi.advanceTimersByTime(600)
    expect(latestAnim()!.cancel).toHaveBeenCalledTimes(1)
  })

  it('shows a green circled check confirmation after the spin finishes', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    const confirmIcon = () => wrapper.find('svg[data-confirm="true"]')
    const originalIcon = () => wrapper.find('svg:not([data-confirm])')

    await wrapper.setProps({ loading: true })
    await nextTick()
    expect(confirmIcon().exists()).toBe(false)
    expect(originalIcon().exists()).toBe(true)

    // Load stops mid-turn; spin finishes the whole revolution then cancels
    latestAnim()!.currentTime = 200
    await wrapper.setProps({ loading: false })
    await nextTick()
    expect(confirmIcon().exists()).toBe(false) // still spinning to the boundary

    vi.advanceTimersByTime(300) // reach the revolution boundary → cancel + confirm
    await nextTick()
    expect(confirmIcon().exists()).toBe(true)

    // Confirm icon is the green circled check (CheckCircle2 → a circle with a
    // check path) with the bounce-in animation
    const iconSvg = confirmIcon().element as SVGElement
    expect(iconSvg.style.color).toBe('var(--color-green, #16a34a)')
    expect(iconSvg.style.animation).toContain('check-in')
    // CircleCheck renders a <circle> plus the check <path>, unlike the bare
    // Check icon which has no circle
    expect(iconSvg.querySelector('circle')).not.toBeNull()

    // After the confirmation window the original icon comes back
    vi.advanceTimersByTime(400)
    await nextTick()
    expect(confirmIcon().exists()).toBe(false)
    expect(originalIcon().exists()).toBe(true)
  })

  it('skips the Check confirmation when a new refresh starts before the spin finishes', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    const confirmIcon = () => wrapper.find('svg[data-confirm="true"]')

    await wrapper.setProps({ loading: true })
    await nextTick()
    latestAnim()!.currentTime = 250
    await wrapper.setProps({ loading: false })
    await nextTick()

    // Immediately start a new refresh before the finish timer fired
    await wrapper.setProps({ loading: true })
    await nextTick()
    expect(confirmIcon().exists()).toBe(false)

    // Spin finishes at the boundary; but loading is true again → no confirm
    vi.advanceTimersByTime(300)
    await nextTick()
    expect(confirmIcon().exists()).toBe(false)
  })

  it('starts spinning when mounted with loading=true (in-flight refresh)', async () => {
    const wrapper = mountBtn({ loading: true })
    await nextTick()
    await nextTick() // watch's DOM-update await resolves after mount

    expect(animateMock).toHaveBeenCalledTimes(1)
  })

  it('animates the visible icon when restarting during the confirm window', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    const confirmIcon = () => wrapper.find('svg[data-confirm="true"]')
    const originalIcon = () => wrapper.find('svg:not([data-confirm])')

    // First refresh completes → confirm Check shows
    await wrapper.setProps({ loading: true })
    await nextTick()
    latestAnim()!.currentTime = 200
    await wrapper.setProps({ loading: false })
    await nextTick()
    vi.advanceTimersByTime(300)
    await nextTick()
    expect(confirmIcon().exists()).toBe(true)

    // Restart during the confirm window: the new spin must run on the icon the
    // user sees (the refresh icon), not on the soon-to-be-replaced Check.
    const animsBefore = animateMock.mock.calls.length
    await wrapper.setProps({ loading: true })
    await nextTick()
    await nextTick()
    expect(confirmIcon().exists()).toBe(false) // check swapped back to refresh icon
    expect(animateMock.mock.calls.length).toBe(animsBefore + 1) // new spin started
    // The animated element is the currently-rendered (non-confirm) svg
    const animatedSvg = animatedEls[animsBefore]
    const renderedSvg = originalIcon().element
    expect(animatedSvg).toBe(renderedSvg)
  })

  it('emits click and stays disabled while loading', async () => {
    const wrapper = mountBtn()
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)

    await wrapper.setProps({ loading: true })
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1) // blocked while loading
    expect((wrapper.find('button').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('re-targets the spin animation when the icon prop swaps mid-load', async () => {
    const wrapper = mountBtn()
    await wrapper.setProps({ loading: true })
    await nextTick()
    expect(animateMock).toHaveBeenCalledTimes(1)
    // The animation is running on the original RefreshCw svg.
    const firstAnimSvg = animatedEls[0]
    expect(firstAnimSvg).toBe(wrapper.find('svg').element)

    // Swap the icon while still loading: the old animation targets a detached
    // node, so a fresh spin must start on the newly-rendered RotateCcw svg.
    await wrapper.setProps({ icon: 'RotateCcw' })
    await nextTick()
    await nextTick()

    expect(animateMock).toHaveBeenCalledTimes(2)
    // The new animation is bound to the currently-rendered (RotateCcw) svg.
    const secondAnimSvg = animatedEls[1]
    const renderedSvg = wrapper.find('svg').element
    expect(secondAnimSvg).toBe(renderedSvg)
    // The superseded animation was cancelled.
    expect(animations[0].cancel).toHaveBeenCalledTimes(1)
  })
})
