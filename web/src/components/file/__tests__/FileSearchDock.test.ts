import { describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h } from 'vue'

vi.mock('lucide-vue-next', () => ({
  FolderTree: { name: 'FolderTree', render: () => h('span', { class: 'i-tree' }) },
  Globe: { name: 'Globe', render: () => h('span', { class: 'i-globe' }) },
  WholeWord: { name: 'WholeWord', render: () => h('span', { class: 'i-word' }) },
  Search: { name: 'Search', render: () => h('span', { class: 'i-search' }) },
  X: { name: 'X', render: () => h('span', { class: 'i-x' }) },
}))

vi.mock('@/components/common/SearchInput.vue', () => ({
  default: {
    name: 'SearchInput',
    props: { modelValue: String, placeholder: String },
    emits: ['update:modelValue', 'enter', 'down', 'up'],
    template: '<input class="search-stub" :value="modelValue" :placeholder="placeholder" @input="$emit(\'update:modelValue\', $event.target.value)" />',
  },
}))

import FileSearchDock from '../FileSearchDock.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  missingWarn: false,
  fallbackWarn: false,
  messages: {
    en: {
      file: {
        search: { exact: 'Exact', recursive: 'Recursive', scopeGlobal: 'Global' },
      },
    },
  },
})

function mountDock(props: Record<string, unknown> = {}) {
  return mount(FileSearchDock, {
    props: { query: '', exact: false, recursiveActive: false, globalActive: false, ...props },
    global: { plugins: [i18n] },
  })
}

describe('FileSearchDock', () => {
  it('renders the field plus the three toggles in order', () => {
    const wrapper = mountDock()
    expect(wrapper.find('.search-stub').exists()).toBe(true)
    const toggles = wrapper.findAll('.fs-toggle-btn')
    expect(toggles).toHaveLength(3)
    expect(toggles.map(t => t.attributes('title'))).toEqual(['Exact', 'Recursive', 'Global'])
  })

  it('marks the active toggles from props', () => {
    const wrapper = mountDock({ exact: true, recursiveActive: true, globalActive: false })
    const toggles = wrapper.findAll('.fs-toggle-btn')
    expect(toggles[0].classes()).toContain('active')
    expect(toggles[1].classes()).toContain('active')
    expect(toggles[2].classes()).not.toContain('active')
  })

  it('emits toggle events for each button', async () => {
    const wrapper = mountDock()
    const toggles = wrapper.findAll('.fs-toggle-btn')
    await toggles[0].trigger('click')
    await toggles[1].trigger('click')
    await toggles[2].trigger('click')
    expect(wrapper.emitted('toggle-exact')).toHaveLength(1)
    expect(wrapper.emitted('toggle-recursive')).toHaveLength(1)
    expect(wrapper.emitted('toggle-global')).toHaveLength(1)
  })

  it('disables the recursive toggle when global scope forces it on', async () => {
    // Global implies recursive: the toggle shows as on but must not be operable,
    // or turning it "off" would silently contradict the active global scope.
    const wrapper = mountDock({ recursiveActive: true, recursiveDisabled: true, globalActive: true })
    const recursive = wrapper.findAll('.fs-toggle-btn')[1]
    expect(recursive.attributes('disabled')).toBeDefined()
    await recursive.trigger('click')
    expect(wrapper.emitted('toggle-recursive')).toBeFalsy()
  })

  it('forwards typing and navigation keys', async () => {
    const wrapper = mountDock()
    await wrapper.find('.search-stub').setValue('main')
    expect(wrapper.emitted('update:query')![0]).toEqual(['main'])

    const input = wrapper.findComponent({ name: 'SearchInput' })
    input.vm.$emit('enter')
    input.vm.$emit('down')
    input.vm.$emit('up')
    expect(wrapper.emitted('enter')).toHaveLength(1)
    expect(wrapper.emitted('down')).toHaveLength(1)
    expect(wrapper.emitted('up')).toHaveLength(1)
  })

  it('keeps Escape inside the dock', async () => {
    // The dock sits inside surfaces that close on Escape (drawer, sheet); the
    // key must stop at the dock instead of reaching an ancestor handler.
    const parentEsc = vi.fn()
    const Parent = defineComponent({
      components: { FileSearchDock },
      setup: () => ({ parentEsc }),
      template: `<div class="outer" @keydown="parentEsc">
        <FileSearchDock query="x" :exact="false" :recursive-active="false" :global-active="false" />
      </div>`,
    })
    const wrapper = mount(Parent, { global: { plugins: [i18n] } })
    await wrapper.find('.search-stub').trigger('keydown', { key: 'Escape' })
    expect(wrapper.findComponent(FileSearchDock).emitted('escape')).toHaveLength(1)
    expect(parentEsc).not.toHaveBeenCalled()
  })
})

/**
 * The dock is the ONE place these styles live now (the file manager and the
 * picker both render this component), so the appearance guards live here too.
 * jsdom has no CSS engine — assert against the source.
 */
describe('FileSearchDock — appearance contract', () => {
  const src = readFileSync(join(__dirname, '..', 'FileSearchDock.vue'), 'utf8')

  it('renders the field flat on the dock material', () => {
    const pill = src.match(/\.fs-input-row :deep\(\.search-pill\)\s*\{([^}]*)\}/)
    expect(pill).toBeTruthy()
    expect(pill![1]).toMatch(/background:\s*transparent/)
    expect(pill![1]).toMatch(/border:\s*none/)
  })

  it('leaves the field with no fill or border in ANY state, including focus', () => {
    // SearchInput's own treatment paints the pill (--bg-primary + 1px border)
    // and adds an accent border + glow on focus. On a flat dock field both read
    // as a nested box, so every state must resolve to no fill and no outline —
    // the caret is the only focus indicator.
    const focused = src.match(/\.fs-input-row :deep\(\.search-pill\.focused\)\s*\{([^}]*)\}/)
    expect(focused).toBeTruthy()
    expect(focused![1]).toMatch(/border:\s*none/)
    expect(focused![1]).toMatch(/box-shadow:\s*none/)
    expect(focused![1]).toMatch(/background:\s*transparent/)
    // A tint would reintroduce the box this field exists to avoid.
    expect(focused![1]).not.toMatch(/--bg-hover/)
    expect(focused![1]).not.toMatch(/--accent-color/)
  })

  it('keeps the dock compact — one 26px band for field and toggles', () => {
    const row = src.match(/\.fs-input-row\s*\{([^}]*)\}/)
    expect(row).toBeTruthy()
    expect(row![1]).toMatch(/align-items:\s*center/)
    const toggle = src.match(/\.fs-toggle-btn\s*\{([^}]*)\}/)
    expect(toggle).toBeTruthy()
    expect(toggle![1]).toMatch(/width:\s*calc\(\s*26\s*\*\s*var\(--dp\)\s*\)/)
    expect(toggle![1]).toMatch(/height:\s*calc\(\s*26\s*\*\s*var\(--dp\)\s*\)/)
  })
})
