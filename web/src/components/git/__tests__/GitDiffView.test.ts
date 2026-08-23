import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { openFilePath } from '@/composables/useFilePathAnnotation.ts'
import GitDiffView from '@/components/git/GitDiffView.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual: any = await importOriginal()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<div class="loading-stub" />' },
}))

vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  openFilePath: vi.fn(),
}))

describe('GitDiffView', () => {
  beforeEach(() => {
    vi.mocked(openFilePath).mockClear()
  })


  function mountDiff(props: Record<string, unknown> = {}) {
    return mount(GitDiffView, {
      props: { loading: false, empty: false, html: '', noWrap: false, filePath: '', ...props },
    })
  }

  it('renders loading state when loading is true', () => {
    const wrapper = mountDiff({ loading: true })
    expect(wrapper.find('.git-diff-loading').exists()).toBe(true)
  })

  it('renders empty state when empty is true', () => {
    const wrapper = mountDiff({ empty: true })
    expect(wrapper.find('.git-diff-empty').exists()).toBe(true)
    expect(wrapper.find('.git-diff-empty').text()).toBe('git.diffView.noChanges')
  })

  it('renders html content when neither loading nor empty', () => {
    const wrapper = mountDiff({ html: '<div class="x">hello</div>' })
    const scroll = wrapper.find('.git-diff-scroll')
    expect(scroll.exists()).toBe(true)
    expect(scroll.classes()).not.toContain('no-wrap')
    expect(scroll.html()).toContain('hello')
  })

  it('applies no-wrap class when noWrap prop is true', () => {
    const wrapper = mountDiff({ html: '<span>x</span>', noWrap: true })
    expect(wrapper.find('.git-diff-scroll').classes()).toContain('no-wrap')
  })

  it('opens file at line when diff-linum-new is clicked', async () => {
    const wrapper = mountDiff({ html: '<div>scroll</div>', filePath: '/a/b.ts' })
    const target = document.createElement('span')
    target.className = 'diff-linum-new'
    target.setAttribute('data-line', '42')
    const evt = { target, preventDefault: vi.fn(), stopPropagation: vi.fn() } as any
    await wrapper.find('.git-diff-scroll').trigger('click')
    // Component renders v-html; we cannot easily access the inner span tree.
    // Test the onDiffClick logic directly via the wrapper.
    await (wrapper.vm as any).onDiffClick(evt)
    expect(openFilePath).toHaveBeenCalledWith('/a/b.ts', 42)
    expect(evt.preventDefault).toHaveBeenCalled()
    expect(evt.stopPropagation).toHaveBeenCalled()
  })

  it('toggles wrap mode when wrap button is clicked', async () => {
    const wrapper = mountDiff({ html: '<div>x</div>' })
    const hunk = document.createElement('div')
    hunk.className = 'diff-hunk'
    const btn = document.createElement('button')
    btn.className = 'diff-hunk-wrap-btn'
    btn.setAttribute('data-action', 'wrap')
    hunk.appendChild(btn)
    const evt = { target: btn, preventDefault: vi.fn(), stopPropagation: vi.fn() } as any
    await (wrapper.vm as any).onDiffClick(evt)
    expect(hunk.classList.contains('diff-hunk-wrap')).toBe(true)
    expect(btn.classList.contains('is-wrapped')).toBe(true)
  })

  it('toggles line number visibility when linum button is clicked', async () => {
    const wrapper = mountDiff({ html: '<div>x</div>' })
    const hunk = document.createElement('div')
    hunk.className = 'diff-hunk'
    const btn = document.createElement('button')
    btn.className = 'diff-hunk-linum-btn'
    btn.setAttribute('data-action', 'linum')
    hunk.appendChild(btn)
    const evt = { target: btn, preventDefault: vi.fn(), stopPropagation: vi.fn() } as any
    await (wrapper.vm as any).onDiffClick(evt)
    expect(hunk.classList.contains('diff-hunk-no-linum')).toBe(true)
    expect(btn.classList.contains('is-on')).toBeFalsy()

    // Click again - re-enables
    await (wrapper.vm as any).onDiffClick(evt)
    expect(hunk.classList.contains('diff-hunk-no-linum')).toBe(false)
    expect(btn.classList.contains('is-on')).toBe(true)
  })

  it('does not open file path when linum has no data-line', async () => {
    const wrapper = mountDiff({ filePath: '/foo' })
    const target = document.createElement('span')
    target.className = 'diff-linum-new'
    // No data-line attribute
    const evt = { target, preventDefault: vi.fn(), stopPropagation: vi.fn() } as any
    await (wrapper.vm as any).onDiffClick(evt)
    expect(openFilePath).not.toHaveBeenCalled()
  })

  it('does not open file path when filePath is empty', async () => {
    const wrapper = mountDiff({ filePath: '' })
    const target = document.createElement('span')
    target.className = 'diff-linum-new'
    target.setAttribute('data-line', '5')
    const evt = { target, preventDefault: vi.fn(), stopPropagation: vi.fn() } as any
    await (wrapper.vm as any).onDiffClick(evt)
    expect(openFilePath).not.toHaveBeenCalled()
  })

  it('does nothing when click target is not a button or linum-new', async () => {
    const wrapper = mountDiff()
    const evt = {
      target: document.createElement('div'),
      preventDefault: vi.fn(),
      stopPropagation: vi.fn(),
    } as any
    await (wrapper.vm as any).onDiffClick(evt)
    expect(evt.preventDefault).not.toHaveBeenCalled()
  })
})
