import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, onUnmounted, ref } from 'vue'
import enMessages from '@/i18n/locales/en'
import MeetingPanel from '../MeetingPanel.vue'
import { _resetWakeLockForTest } from '@/composables/useWakeLock'

/**
 * Every wrapper is torn down after its test.
 *
 * A panel left mounted keeps its `clawbench-open-meeting` window listener and
 * its watchers on the module-level share state, so a later test's event reaches
 * it too — and its re-render then lands in that later test's mount counters.
 */
enableAutoUnmount(afterEach)

/**
 * The shell's state machine. The children are stubbed: this file is only about
 * which view is on screen, and in particular that ENDING a meeting lands on the
 * same detail view a list row opens.
 */

const shareState = ref('idle')
const shareToken = ref('')
vi.mock('@/composables/useLiveShare', () => ({
  useLiveShare: () => ({ state: shareState, token: shareToken }),
}))

/** The back handler the shell registers via useFeatureBackHandler. */
let backHandler: { enabled: () => boolean; goBack: () => void } | null = null
vi.mock('@/composables/useEdgeSwipeBack', () => ({
  useFeatureBackHandler: vi.fn((_id: string, enabled: () => boolean, goBack: () => void) => {
    backHandler = { enabled, goBack }
  }),
  PRIORITY_PAGE: 100,
}))

/**
 * The stage counts its own mounts and unmounts.
 *
 * It holds the live chat, the pending control requests and the viewer it
 * granted control to, and it is subscribed to the viewers' control channel —
 * destroying it while the meeting runs loses all of that silently. "It is still
 * the same instance" is what the panel has to guarantee, and only the component
 * can report it.
 */
const stageLifecycle = vi.hoisted(() => ({ mounts: 0, unmounts: 0 }))

const ListStub = {
  name: 'MeetingList',
  props: ['liveToken'],
  template: `<div data-view="list" :data-live-token="liveToken || ''">
    <button data-stub="create" @click="$emit('create')" />
    <button data-stub="join" @click="$emit('join')" />
    <button data-stub="open" @click="$emit('open', 'tok-1')" />
    <button data-stub="resume" @click="$emit('resume')" />
    <button data-stub="join-live" @click="$emit('join-live', 'tok-live')" />
  </div>`,
}
const CreateStub = {
  name: 'MeetingCreate',
  template: `<div data-view="create"><button data-stub="started" @click="$emit('started')" /></div>`,
}
const JoinStub = { name: 'MeetingJoin', template: '<div data-view="join" />' }
const StageStub = {
  name: 'MeetingStage',
  props: ['active'],
  setup() {
    stageLifecycle.mounts += 1
    onUnmounted(() => {
      stageLifecycle.unmounts += 1
    })
    return {}
  },
  template: `<div data-view="meeting" :data-active="active ? 'true' : 'false'"><button data-stub="ended" @click="$emit('ended', 'tok-9')" /><button data-stub="stage-back" @click="$emit('back')" /></div>`,
}
const DetailStub = {
  name: 'MeetingDetail',
  props: ['token'],
  template: `<div data-view="detail" :data-token="token"><button data-stub="back" @click="$emit('back')" /></div>`,
}

function mountPanel(props: { active?: boolean } = { active: true }) {
  return mount(MeetingPanel, {
    props,
    global: {
      // The panel translates its own error toast (join failure) via useI18n.
      plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })],
      stubs: {
        MeetingList: ListStub,
        MeetingCreate: CreateStub,
        MeetingJoin: JoinStub,
        MeetingStage: StageStub,
        MeetingDetail: DetailStub,
      },
    },
  })
}

/**
 * The view the user is actually looking at.
 *
 * The stage is kept mounted behind the list (see MeetingStage's own note), so
 * "is it in the DOM" no longer answers "is it on screen". `isVisible()` cannot
 * answer it either: VTU mounts detached, and jsdom's `getComputedStyle` reports
 * nothing for a detached tree — the inline style `v-show` writes is the only
 * observable.
 */
function visibleViews(w: ReturnType<typeof mountPanel>): string[] {
  return w
    .findAll('[data-view]')
    .filter((n) => {
      const el = n.element as HTMLElement
      return el.style.display !== 'none' && !el.hasAttribute('hidden')
    })
    .map((n) => n.attributes('data-view'))
}

beforeEach(() => {
  shareState.value = 'idle'
  shareToken.value = ''
  backHandler = null
  stageLifecycle.mounts = 0
  stageLifecycle.unmounts = 0
  _resetWakeLockForTest()
  delete (globalThis as any).ClawBenchNative
})

describe('MeetingPanel state machine', () => {

  it('lands on the record list', () => {
    const w = mountPanel()
    expect(visibleViews(w)).toEqual(['list'])
  })

  it('opens create and join as separate pages', async () => {
    const w = mountPanel()
    await w.find('[data-stub="create"]').trigger('click')
    expect(visibleViews(w)).toEqual(['create'])

    const w2 = mountPanel()
    await w2.find('[data-stub="join"]').trigger('click')
    expect(visibleViews(w2)).toEqual(['join'])
  })

  /**
   * The list's running row advertises a meeting this client is not publishing
   * (another device hosts it). Joining it is the same entry the 加入 page
   * performs with a pasted link, minus the input: open the member view at the
   * record's token — the room is per project, so there is nothing to look up.
   */
  it('opens the member view when joining the running meeting from the list', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const w = mountPanel()

    await w.find('[data-stub="join-live"]').trigger('click')

    expect(open).toHaveBeenCalledWith(
      `${window.location.origin}/share/tok-live`,
      '_blank',
      'noopener',
    )
    // The list stays where it is: the member view is a separate page.
    expect(visibleViews(w)).toEqual(['list'])
  })

  it('enters the meeting when the create page starts one', async () => {
    const w = mountPanel()
    await w.find('[data-stub="create"]').trigger('click')
    await w.find('[data-stub="started"]').trigger('click')
    expect(visibleViews(w)).toEqual(['meeting'])
  })

  /**
   * A phone that dims out mid-meeting ends the meeting: the WebView pauses, the
   * publisher socket closes and the share stops. The lock is keyed on the share
   * state (not on the stage being visible), because the meeting keeps running
   * while the host reads the record list or works in another tab.
   */
  it('holds the screen awake while a meeting runs, and only then', async () => {
    const setKeepScreenOn = vi.fn()
    ;(globalThis as any).ClawBenchNative = { setKeepScreenOn }

    const w = mountPanel()
    // Mounting on the record list must not turn anything on.
    expect(setKeepScreenOn).not.toHaveBeenCalled()

    shareState.value = 'publishing'
    await nextTick()
    expect(setKeepScreenOn).toHaveBeenCalledWith(true)

    // The meeting ended: release.
    shareState.value = 'idle'
    await nextTick()
    expect(setKeepScreenOn).toHaveBeenLastCalledWith(false)

    // Leaving the panel mid-meeting releases it too.
    shareState.value = 'publishing'
    await nextTick()
    setKeepScreenOn.mockClear()
    w.unmount()
    expect(setKeepScreenOn).toHaveBeenCalledWith(false)
  })

  it('opens the record of the meeting that just ended, not a separate report', async () => {
    const w = mountPanel()
    await w.find('[data-stub="create"]').trigger('click')
    await w.find('[data-stub="started"]').trigger('click')
    await w.find('[data-stub="ended"]').trigger('click')

    const detail = w.find('[data-view="detail"]')
    expect(visibleViews(w)).toEqual(['detail'])
    expect(detail.attributes('data-token')).toBe('tok-9')
  })

  it('goes back to the list from the detail view', async () => {
    const w = mountPanel()
    await w.find('[data-stub="open"]').trigger('click')
    await w.find('[data-stub="back"]').trigger('click')
    expect(visibleViews(w)).toEqual(['list'])
  })

  it('opens a record requested by a quote jump', async () => {
    const w = mountPanel()
    window.dispatchEvent(new CustomEvent('clawbench-open-meeting', { detail: { token: 'tok-7' } }))
    await nextTick()

    expect(w.find('[data-view="detail"]').attributes('data-token')).toBe('tok-7')
    w.unmount()
  })

  it('returns to the stage when the tab is re-entered mid-meeting', async () => {
    const w = mountPanel({ active: false })
    shareState.value = 'publishing'
    await nextTick()
    // The watcher already moved to the stage; leaving and re-entering must keep
    // it there rather than resetting to the list.
    await w.setProps({ active: true })
    await nextTick()
    expect(visibleViews(w)).toEqual(['meeting'])
  })

  /**
   * The reported bug: a host who leaves the stage for the record list and comes
   * back loses the live chat, the pending control request and the viewer it had
   * granted control to — all of it component state, and the subscription that
   * fills it is torn down on unmount.
   */
  it('keeps the stage alive while the host reads the record list', async () => {
    shareState.value = 'publishing'
    // The list row the stub opens; `openDetail` sends a running meeting back to
    // its stage only when the token is this client's own.
    shareToken.value = 'tok-1'
    const w = mountPanel()
    await nextTick()
    expect(visibleViews(w)).toEqual(['meeting'])
    expect(stageLifecycle.mounts).toBe(1)

    backHandler?.goBack()
    await nextTick()
    expect(visibleViews(w)).toEqual(['list'])
    // Still mounted, just hidden — and told it is no longer the visible surface
    // (it must not throw its full-screen picture over the list).
    expect(w.find('[data-view="meeting"]').exists()).toBe(true)
    expect(w.find('[data-view="meeting"]').attributes('data-active')).toBe('false')

    await w.find('[data-stub="open"]').trigger('click')
    expect(visibleViews(w)).toEqual(['meeting'])
    expect(stageLifecycle.unmounts).toBe(0)
    expect(stageLifecycle.mounts).toBe(1)
    expect(w.find('[data-view="meeting"]').attributes('data-active')).toBe('true')
  })
})

describe('MeetingPanel back navigation', () => {

  it('returns from every subpage to the list', async () => {
    // create
    let w = mountPanel()
    await w.find('[data-stub="create"]').trigger('click')
    expect(backHandler?.enabled()).toBe(true)
    backHandler?.goBack()
    await nextTick()
    expect(visibleViews(w)).toEqual(['list'])
    w.unmount()

    // join
    w = mountPanel()
    await w.find('[data-stub="join"]').trigger('click')
    expect(backHandler?.enabled()).toBe(true)
    backHandler?.goBack()
    await nextTick()
    expect(visibleViews(w)).toEqual(['list'])
    w.unmount()

    // detail
    w = mountPanel()
    await w.find('[data-stub="open"]').trigger('click')
    expect(backHandler?.enabled()).toBe(true)
    backHandler?.goBack()
    await nextTick()
    expect(visibleViews(w)).toEqual(['list'])
    w.unmount()
  })

  it('leaves a running meeting without ending it', async () => {
    shareState.value = 'publishing'
    shareToken.value = 'tok-live'
    const w = mountPanel()
    await nextTick()
    expect(visibleViews(w)).toEqual(['meeting'])

    expect(backHandler?.enabled()).toBe(true)
    backHandler?.goBack()
    await nextTick()

    // Back leaves the stage but does not stop the meeting: the state machine
    // still knows it is publishing.
    expect(visibleViews(w)).toEqual(['list'])
    expect(shareState.value).toBe('publishing')
  })

  it('reopens the stage from the running record, so back is not a trapdoor', async () => {
    shareState.value = 'publishing'
    shareToken.value = 'tok-1'
    const w = mountPanel()
    await nextTick()

    backHandler?.goBack()
    await nextTick()
    expect(visibleViews(w)).toEqual(['list'])

    await w.find('[data-stub="open"]').trigger('click')

    expect(visibleViews(w)).toEqual(['meeting'])
  })

  it('does not intercept back on the list itself', () => {
    mountPanel()
    expect(backHandler?.enabled()).toBe(false)
  })

  /**
   * The stage used to be a dead end on a desktop: the hardware/edge gesture
   * existed, but nothing on screen did, so a host could not go read another
   * meeting's minutes without ending this one.
   */
  it('leaves the stage for the list from the stage back control', async () => {
    shareState.value = 'publishing'
    shareToken.value = 'tok-live'
    const w = mountPanel()
    await nextTick()
    expect(visibleViews(w)).toEqual(['meeting'])

    await w.find('[data-stub="stage-back"]').trigger('click')

    expect(visibleViews(w)).toEqual(['list'])
    // Navigation, not an exit: the meeting is still running and the stage is
    // still mounted with its chat and pending approvals.
    expect(shareState.value).toBe('publishing')
    expect(stageLifecycle.unmounts).toBe(0)
  })

  it('hands the list the live token so it can offer the way back', async () => {
    shareState.value = 'publishing'
    shareToken.value = 'tok-live'
    const w = mountPanel()
    await nextTick()
    await w.find('[data-stub="stage-back"]').trigger('click')

    expect(w.find('[data-view="list"]').attributes('data-live-token')).toBe('tok-live')

    await w.find('[data-stub="resume"]').trigger('click')
    expect(visibleViews(w)).toEqual(['meeting'])
  })

  it('offers no way back when nothing is running', async () => {
    const w = mountPanel()
    expect(w.find('[data-view="list"]').attributes('data-live-token')).toBe('')
  })

  /**
   * Reading another record while a meeting runs must survive a trip to another
   * dock tab: the old `active` watcher forced the stage on every re-entry, so
   * the record the host had opened was replaced mid-read.
   */
  it('keeps the record the host opened when the tab is re-entered', async () => {
    shareState.value = 'publishing'
    shareToken.value = 'tok-live'
    const w = mountPanel()
    await nextTick()

    window.dispatchEvent(new CustomEvent('clawbench-open-meeting', { detail: { token: 'tok-7' } }))
    await nextTick()
    expect(w.find('[data-view="detail"]').attributes('data-token')).toBe('tok-7')

    await w.setProps({ active: false })
    await w.setProps({ active: true })
    await nextTick()

    expect(visibleViews(w)).toEqual(['detail'])
    expect(w.find('[data-view="detail"]').attributes('data-token')).toBe('tok-7')
  })

  it('does not intercept back while the tab is inactive', async () => {
    const w = mountPanel({ active: false })
    await w.find('[data-stub="open"]').trigger('click')
    // A hidden panel must never swallow the gesture meant for what is on screen.
    expect(backHandler?.enabled()).toBe(false)
  })
})
