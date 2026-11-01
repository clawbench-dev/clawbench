import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import MeetingPanel from '../MeetingPanel.vue'

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

const ListStub = {
  name: 'MeetingList',
  template: `<div data-view="list">
    <button data-stub="create" @click="$emit('create')" />
    <button data-stub="join" @click="$emit('join')" />
    <button data-stub="open" @click="$emit('open', 'tok-1')" />
  </div>`,
}
const CreateStub = {
  name: 'MeetingCreate',
  template: `<div data-view="create"><button data-stub="started" @click="$emit('started')" /></div>`,
}
const JoinStub = { name: 'MeetingJoin', template: '<div data-view="join" />' }
const StageStub = {
  name: 'MeetingStage',
  template: `<div data-view="meeting"><button data-stub="ended" @click="$emit('ended', 'tok-9')" /></div>`,
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

beforeEach(() => {
  shareState.value = 'idle'
  shareToken.value = ''
})

describe('MeetingPanel state machine', () => {
  it('lands on the record list', () => {
    const w = mountPanel()
    expect(w.find('[data-view="list"]').exists()).toBe(true)
  })

  it('opens create and join as separate pages', async () => {
    const w = mountPanel()
    await w.find('[data-stub="create"]').trigger('click')
    expect(w.find('[data-view="create"]').exists()).toBe(true)

    const w2 = mountPanel()
    await w2.find('[data-stub="join"]').trigger('click')
    expect(w2.find('[data-view="join"]').exists()).toBe(true)
  })

  it('enters the meeting when the create page starts one', async () => {
    const w = mountPanel()
    await w.find('[data-stub="create"]').trigger('click')
    await w.find('[data-stub="started"]').trigger('click')
    expect(w.find('[data-view="meeting"]').exists()).toBe(true)
  })

  it('opens the record of the meeting that just ended, not a separate report', async () => {
    const w = mountPanel()
    await w.find('[data-stub="create"]').trigger('click')
    await w.find('[data-stub="started"]').trigger('click')
    await w.find('[data-stub="ended"]').trigger('click')

    const detail = w.find('[data-view="detail"]')
    expect(detail.exists()).toBe(true)
    expect(detail.attributes('data-token')).toBe('tok-9')
  })

  it('goes back to the list from the detail view', async () => {
    const w = mountPanel()
    await w.find('[data-stub="open"]').trigger('click')
    await w.find('[data-stub="back"]').trigger('click')
    expect(w.find('[data-view="list"]').exists()).toBe(true)
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
    expect(w.find('[data-view="meeting"]').exists()).toBe(true)
  })
})
