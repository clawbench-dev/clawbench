import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskScriptResultCard from '../TaskScriptResultCard.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('lucide-vue-next', () => ({
  Terminal: { name: 'Terminal', props: { size: Number }, template: '<svg data-icon="Terminal" />' },
}))
// format.ts pulls in the app i18n instance; the duration formatting under test
// is its own concern, so use the real implementation without the i18n import.
vi.mock('@/utils/format.ts', () => ({
  formatDuration: (ms: number) => (ms < 1000 ? `${Math.round(ms)}ms` : `${(ms / 1000).toFixed(1)}s`),
}))

function mountCard(script: Record<string, unknown>) {
  return mount(TaskScriptResultCard, { props: { script } })
}

describe('TaskScriptResultCard', () => {
  it('renders the exit code and both output streams', () => {
    const wrapper = mountCard({ exitCode: 0, stdout: 'hello out', stderr: 'warn err', durationMs: 1234 })
    expect(wrapper.find('.src-exit').text()).toContain('0')
    expect(wrapper.text()).toContain('hello out')
    expect(wrapper.text()).toContain('warn err')
    // A clean exit reads as passing, not as a warning.
    expect(wrapper.find('.src-exit').classes()).toContain('ok')
    expect(wrapper.classes()).not.toContain('is-failed')
  })

  it('marks a non-zero exit as failed', () => {
    const wrapper = mountCard({ exitCode: 1, stdout: '', stderr: 'boom', durationMs: 10 })
    expect(wrapper.find('.src-exit').text()).toContain('1')
    expect(wrapper.find('.src-exit').classes()).toContain('bad')
    expect(wrapper.classes()).toContain('is-failed')
  })

  it('shows a placeholder when the script produced no output', () => {
    // A silent exit 0 is a legitimate outcome, so the card must explain the
    // empty body rather than render two blank blocks.
    const wrapper = mountCard({ exitCode: 0, stdout: '', stderr: '', durationMs: 5 })
    expect(wrapper.find('.src-empty').exists()).toBe(true)
    expect(wrapper.findAll('.src-stream')).toHaveLength(0)
  })

  it('renders one stream when only stderr is present', () => {
    const wrapper = mountCard({ exitCode: 2, stdout: '', stderr: 'only stderr', durationMs: 5 })
    expect(wrapper.findAll('.src-stream')).toHaveLength(1)
    expect(wrapper.text()).toContain('only stderr')
    expect(wrapper.find('.src-empty').exists()).toBe(false)
  })

  it('formats the duration only when it is positive', () => {
    expect(mountCard({ exitCode: 0, stdout: 'x', stderr: '', durationMs: 1500 }).find('.src-duration').text()).toBe('1.5s')
    expect(mountCard({ exitCode: 0, stdout: 'x', stderr: '', durationMs: 0 }).find('.src-duration').exists()).toBe(false)
  })
})
