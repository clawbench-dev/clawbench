import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PlanPanel from '@/components/chat/PlanPanel.vue'
import type { PlanEntry } from '@/composables/usePlanProgress'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        plan: {
          title: 'Execution Plan',
          completedCount: '{completed}/{total} done',
        },
      },
    },
  },
})

const entries: PlanEntry[] = [
  { content: 'Step one', status: 'completed', priority: 'high' },
  { content: 'Step two', status: 'in_progress', priority: 'medium' },
  { content: 'Step three', status: 'pending', priority: 'low' },
]

function mountPanel(props: Record<string, unknown>) {
  return mount(PlanPanel, {
    props: { entries, collapsed: false, hasUpdate: false, ...props },
    global: { plugins: [i18n] },
  })
}

describe('PlanPanel', () => {
  it('emits toggle-collapse when clicking anywhere on the expanded header', async () => {
    const wrapper = mountPanel({ collapsed: false })
    await wrapper.get('.plan-expanded__header').trigger('click')
    expect(wrapper.emitted('toggle-collapse')).toHaveLength(1)
  })

  it('emits toggle-collapse when clicking the header title (not just the chevron)', async () => {
    const wrapper = mountPanel({ collapsed: false })
    await wrapper.get('.plan-expanded__title').trigger('click')
    expect(wrapper.emitted('toggle-collapse')).toHaveLength(1)
  })

  it('emits toggle-collapse when clicking the collapsed chip', async () => {
    const wrapper = mountPanel({ collapsed: true })
    await wrapper.get('.plan-chip').trigger('click')
    expect(wrapper.emitted('toggle-collapse')).toHaveLength(1)
  })

  describe('active-entry centering', () => {
    /** Attach fake DOM geometry to the .plan-expanded__timeline element. */
    function stubTimelineGeometry(wrapper: ReturnType<typeof mountPanel>) {
      const el = wrapper.get('.plan-expanded__timeline').element as HTMLElement & {
        __scrollTop: number
      }
      // Visible by default — the visibility guard (offsetHeight === 0) in
      // centerActiveEntry must not short-circuit jsdom tests, which have no
      // real layout.
      Object.defineProperty(el, 'offsetHeight', { configurable: true, value: 120 })
      Object.defineProperty(el, 'clientHeight', { configurable: true, value: 120 })
      Object.defineProperty(el, 'scrollHeight', { configurable: true, value: 400 })
      el.__scrollTop = 0
      Object.defineProperty(el, 'scrollTop', {
        configurable: true,
        get() { return this.__scrollTop },
        set(v: number) { this.__scrollTop = v },
      })
      // Rows report a viewport frame. Stub getBoundingClientRect per row so the
      // component's rect math (rowRect − elRect) yields document y = i*28.
      const elRect = { top: 0, height: 120 }
      Object.defineProperty(el, 'getBoundingClientRect', {
        configurable: true,
        value: () => ({ ...elRect, bottom: elRect.height, width: 0, left: 0, right: 0, x: 0, y: 0, toJSON: () => ({}) }),
      })
      const rows = wrapper.findAll('.plan-entry')
      rows.forEach((row, i) => {
        const r = row.element as HTMLElement
        const rect = { top: i * 28, height: 28 }
        Object.defineProperty(r, 'getBoundingClientRect', {
          configurable: true,
          value: () => ({ ...rect, bottom: rect.top + rect.height, width: 0, left: 0, right: 0, x: 0, y: rect.top, toJSON: () => ({}) }),
        })
      })
      return el
    }

    // Set the active row's viewport top to `topPx` (e.g. a late-added entry).
    function setRowTop(row: ReturnType<ReturnType<typeof mountPanel>['findAll']>[number], topPx: number) {
      const r = row.element as HTMLElement
      Object.defineProperty(r, 'getBoundingClientRect', {
        configurable: true,
        value: () => ({ top: topPx, bottom: topPx + 28, height: 28, width: 0, left: 0, right: 0, x: 0, y: topPx, toJSON: () => ({}) }),
      })
    }
    it('centers the in_progress entry when the panel first renders expanded', async () => {
      const wrapper = mountPanel({ collapsed: false })
      const el = stubTimelineGeometry(wrapper)
      // In_progress is entry #1 → push its row to offsetTop 200; target
      // scrollTop = 200 + 14 − 120/2 = 154.
      const active = wrapper.findAll('.plan-entry')[1]
      setRowTop(active, 200)
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(154)
    })

    it('centers when the user expands the collapsed chip', async () => {
      const wrapper = mountPanel({ collapsed: true })
      expect(wrapper.find('.plan-expanded__timeline').exists()).toBe(false)
      // Expand first — the timeline only exists in the expanded branch, and the
      // centering pass is queued on the collapsed→expanded transition.
      await wrapper.setProps({ collapsed: false })
      const el = stubTimelineGeometry(wrapper)
      const active = wrapper.findAll('.plan-entry')[1]
      setRowTop(active, 200)
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(154)
    })

    it('re-centers when execution advances to a different in_progress entry', async () => {
      const wrapper = mountPanel({ collapsed: false })
      const el = stubTimelineGeometry(wrapper)
      const active = wrapper.findAll('.plan-entry')[1]
      setRowTop(active, 200)
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(154)

      // Advance: entry #1 completes, entry #3 becomes in_progress.
      const nextEntries = entries.map(e => ({ ...e }))
      nextEntries[1] = { ...nextEntries[1], status: 'completed' }
      nextEntries[2] = { ...nextEntries[2], status: 'in_progress' }
      const newActive = wrapper.findAll('.plan-entry')[2]
      setRowTop(newActive, 300)
      await wrapper.setProps({ entries: nextEntries })
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      // From scrollTop 154: target = 154 + 314 − 60 = 408 → clamped to max 280.
      expect(el.__scrollTop).toBe(280)
    })

    it('re-centers on any entries mutation, even when the in_progress entry is unchanged', async () => {
      const wrapper = mountPanel({ collapsed: false })
      const el = stubTimelineGeometry(wrapper)
      const active = wrapper.findAll('.plan-entry')[1]
      setRowTop(active, 200)
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(154)

      // User scrolls away; a content-level replacement (identical statuses)
      // still re-centers on the live step.
      el.__scrollTop = 10
      const same = entries.map(e => ({ ...e }))
      await wrapper.setProps({ entries: same })
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(164) // 10 + 214 − 60
    })

    it('does not force-scroll when no entry is in_progress', async () => {
      const done = entries.map(e => ({ ...e, status: 'completed' as const }))
      const wrapper = mountPanel({ collapsed: false })
      const el = stubTimelineGeometry(wrapper)
      const first = wrapper.findAll('.plan-entry')[0]
      setRowTop(first, 200)
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(0)

      await wrapper.setProps({ entries: done })
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(0)
    })

    it('re-centers when a pending step is appended mid-run', async () => {
      const wrapper = mountPanel({ collapsed: false })
      const el = stubTimelineGeometry(wrapper)
      const active = wrapper.findAll('.plan-entry')[1]
      setRowTop(active, 200)
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(154)

      // User scrolls away; appending a further step re-centers the live step.
      el.__scrollTop = 10
      const appended: PlanEntry[] = [
        ...entries.map(e => ({ ...e })),
        { content: 'Step four', status: 'pending', priority: 'low' },
      ]
      await wrapper.setProps({ entries: appended })
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(164) // 10 + 214 − 60
    })

    it('re-centers after entries are cleared and a new plan appears', async () => {
      const wrapper = mountPanel({ collapsed: false })
      const el = stubTimelineGeometry(wrapper)
      const active = wrapper.findAll('.plan-entry')[1]
      setRowTop(active, 200)
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(154)

      // Session switch clears all plan state (panel unmounts its timeline).
      await wrapper.setProps({ entries: [] })
      expect(wrapper.find('.plan-expanded__timeline').exists()).toBe(false)

      // A new session streams a fresh plan → the panel becomes visible again
      // and must center on the new active step, not stay wherever it was.
      await wrapper.setProps({ entries })
      const el2 = stubTimelineGeometry(wrapper)
      const active2 = wrapper.findAll('.plan-entry')[1]
      setRowTop(active2, 200)
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el2.__scrollTop).toBe(154)
    })

    it('does not scroll while the timeline is hidden (background plan updates)', async () => {
      const wrapper = mountPanel({ collapsed: false })
      const el = stubTimelineGeometry(wrapper)
      // Hide the timeline (e.g. user switched to another tab on a narrow
      // screen) while the agent keeps streaming plan_updates in the background.
      Object.defineProperty(el, 'offsetHeight', { configurable: true, value: 0 })

      // A hidden container measures all geometry as 0 — centering must be
      // skipped so the clamp cannot reset scrollTop and corrupt the position.
      const nextEntries = entries.map(e => ({ ...e }))
      nextEntries[1] = { ...nextEntries[1], status: 'completed' }
      nextEntries[2] = { ...nextEntries[2], status: 'in_progress' }
      await wrapper.setProps({ entries: nextEntries })
      await wrapper.vm.$nextTick()
      await new Promise(r => requestAnimationFrame(r))
      expect(el.__scrollTop).toBe(0)
    })
  })
})
