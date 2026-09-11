import { describe, expect, it, vi } from 'vitest'
import { ref, nextTick } from 'vue'
import { useCompletionMenu } from '@/composables/useCompletionMenu.ts'
import { parseAtQuery, type CompletionItem } from '@/utils/completionMatch.ts'

function item(key: string): CompletionItem {
  return { key, label: key, description: '', source: 'current-dir' }
}

function keyEvent(key: string, extra: Record<string, unknown> = {}) {
  return {
    key,
    isComposing: false,
    keyCode: 0,
    preventDefault: vi.fn(),
    ...extra,
  } as unknown as KeyboardEvent & { preventDefault: ReturnType<typeof vi.fn> }
}

/** Build a menu over a mutable text/trigger, mirroring how ChatInputBar wires it. */
function setup(overrides: Partial<Parameters<typeof useCompletionMenu>[0]> = {}) {
  const text = ref('@')
  const caret = ref(1)
  const items = ref<CompletionItem[]>([item('a.ts'), item('b.ts'), item('c.ts')])
  const onSelect = vi.fn()

  const menu = useCompletionMenu({
    items,
    getTrigger: () => parseAtQuery(text.value, caret.value),
    onSelect,
    closeOnSelect: false,
    getText: () => text.value,
    ...overrides,
  })

  return { menu, text, caret, items, onSelect }
}

describe('useCompletionMenu', () => {
  it('starts hidden with no active index', () => {
    const { menu } = setup()
    expect(menu.show.value).toBe(false)
    expect(menu.activeIndex.value).toBe(-1)
  })

  it('opens and preselects the first item when items are present', async () => {
    const { menu } = setup()
    menu.refresh()
    await nextTick()
    expect(menu.show.value).toBe(true)
    expect(menu.activeIndex.value).toBe(0)
  })

  it('stays hidden when there are no items', () => {
    const { menu, items } = setup()
    items.value = []
    menu.refresh()
    expect(menu.show.value).toBe(false)
  })

  it('wraps ArrowDown and ArrowUp around the list', async () => {
    const { menu } = setup()
    menu.refresh()
    await nextTick()

    menu.handleKeydown(keyEvent('ArrowDown'))
    expect(menu.activeIndex.value).toBe(1)
    menu.handleKeydown(keyEvent('ArrowDown'))
    expect(menu.activeIndex.value).toBe(2)
    menu.handleKeydown(keyEvent('ArrowDown'))
    expect(menu.activeIndex.value).toBe(0)

    menu.handleKeydown(keyEvent('ArrowUp'))
    expect(menu.activeIndex.value).toBe(2)
  })

  it('selects the active item on Enter', async () => {
    const { menu, onSelect } = setup()
    menu.refresh()
    await nextTick()
    menu.handleKeydown(keyEvent('ArrowDown'))

    const handled = menu.handleKeydown(keyEvent('Enter'))
    expect(handled).toBe(true)
    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ key: 'b.ts' }))
  })

  it('selects the active item on Tab', async () => {
    const { menu, onSelect } = setup()
    menu.refresh()
    await nextTick()
    const handled = menu.handleKeydown(keyEvent('Tab'))
    expect(handled).toBe(true)
    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ key: 'a.ts' }))
  })

  it('lets IME composition events through untouched', async () => {
    const { menu, onSelect } = setup()
    menu.refresh()
    await nextTick()
    const handled = menu.handleKeydown(keyEvent('Enter', { isComposing: true }))
    expect(handled).toBe(false)
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('does not handle keys while hidden', () => {
    const { menu, onSelect } = setup()
    expect(menu.handleKeydown(keyEvent('Enter'))).toBe(false)
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('Esc closes the menu and latches dismissal', async () => {
    const { menu } = setup()
    menu.refresh()
    await nextTick()

    const handled = menu.handleKeydown(keyEvent('Escape'))
    expect(handled).toBe(true)
    expect(menu.show.value).toBe(false)

    // further refreshes within the same trigger must not reopen
    menu.refresh()
    await nextTick()
    expect(menu.show.value).toBe(false)
  })

  it('clears the dismissal latch once the trigger disappears', async () => {
    const { menu, text, caret } = setup()
    menu.refresh()
    await nextTick()
    menu.handleKeydown(keyEvent('Escape'))
    expect(menu.show.value).toBe(false)

    // query ends (space) -> trigger gone -> latch clears
    text.value = '@a '
    caret.value = 3
    menu.refresh()
    await nextTick()

    // a fresh @ trigger reopens
    text.value = '@b'
    caret.value = 2
    menu.refresh()
    await nextTick()
    expect(menu.show.value).toBe(true)
  })

  it('deletes the trigger range from the text on select', async () => {
    const text = ref('hello @ab')
    const caret = ref(9)
    const items = ref<CompletionItem[]>([item('a.ts')])
    const onSelect = vi.fn()
    const menu = useCompletionMenu({
      items,
      getTrigger: () => ({ start: 6, end: caret.value, query: 'ab' }),
      onSelect,
      closeOnSelect: false,
      getText: () => text.value,
      applyText: (v) => { text.value = v },
    })
    menu.refresh()
    await nextTick()

    menu.handleKeydown(keyEvent('Enter'))
    expect(onSelect).toHaveBeenCalled()
    expect(text.value).toBe('hello ')
  })

  it('closes after select when closeOnSelect is true', async () => {
    const { menu } = setup({ closeOnSelect: true })
    menu.refresh()
    await nextTick()
    menu.handleKeydown(keyEvent('Enter'))
    expect(menu.show.value).toBe(false)
  })

  it('keeps the menu open and resets the index when closeOnSelect is false', async () => {
    const { menu } = setup({ closeOnSelect: false })
    menu.refresh()
    await nextTick()
    menu.handleKeydown(keyEvent('ArrowDown'))
    menu.handleKeydown(keyEvent('Enter'))
    expect(menu.show.value).toBe(true)
    expect(menu.activeIndex.value).toBe(0)
  })

  it('invokes the onSelect hook before applying the text change', async () => {
    const order: string[] = []
    const text = ref('@a')
    const items = ref<CompletionItem[]>([item('a.ts')])
    const menu = useCompletionMenu({
      items,
      getTrigger: () => ({ start: 0, end: 2, query: 'a' }),
      onSelect: () => { order.push('hook') },
      closeOnSelect: true,
      getText: () => text.value,
      applyText: (v) => { order.push('text:' + v); text.value = v },
    })
    menu.refresh()
    await nextTick()
    menu.handleKeydown(keyEvent('Enter'))
    expect(order).toEqual(['hook', 'text:'])
  })

  it('recomputes the active index when items shrink', async () => {
    const { menu, items } = setup()
    menu.refresh()
    await nextTick()
    menu.handleKeydown(keyEvent('ArrowDown'))
    menu.handleKeydown(keyEvent('ArrowDown'))
    expect(menu.activeIndex.value).toBe(2)

    items.value = [item('a.ts')]
    menu.refresh()
    await nextTick()
    expect(menu.activeIndex.value).toBe(0)
  })

  it('closes when items become empty', async () => {
    const { menu, items } = setup()
    menu.refresh()
    await nextTick()
    items.value = []
    menu.refresh()
    await nextTick()
    expect(menu.show.value).toBe(false)
    expect(menu.activeIndex.value).toBe(-1)
  })

  it('stays open in browse mode after a non-closing select when sticky', async () => {
    const text = ref('@a')
    const items = ref<CompletionItem[]>([item('a.ts'), item('b.ts')])
    const menu = useCompletionMenu({
      items,
      getTrigger: () => parseAtQuery(text.value, text.value.length),
      onSelect: vi.fn(),
      closeOnSelect: false,
      stickyAfterSelect: true,
      getText: () => text.value,
      applyText: (v) => { text.value = v },
    })
    menu.refresh()
    await nextTick()

    menu.handleKeydown(keyEvent('Enter'))
    // trigger removed from the text, but the menu keeps browsing
    expect(text.value).toBe('')
    expect(menu.show.value).toBe(true)
    expect(menu.activeIndex.value).toBe(0)
    expect(menu.sticky.value).toBe(true)

    // refresh() with no trigger must not close it while sticky
    menu.refresh()
    await nextTick()
    expect(menu.show.value).toBe(true)
  })

  it('Esc clears browse mode', async () => {
    const text = ref('@a')
    const items = ref<CompletionItem[]>([item('a.ts')])
    const menu = useCompletionMenu({
      items,
      getTrigger: () => parseAtQuery(text.value, text.value.length),
      onSelect: vi.fn(),
      closeOnSelect: false,
      stickyAfterSelect: true,
      getText: () => text.value,
      applyText: (v) => { text.value = v },
    })
    menu.refresh()
    await nextTick()
    menu.handleKeydown(keyEvent('Enter'))
    expect(menu.sticky.value).toBe(true)

    menu.handleKeydown(keyEvent('Escape'))
    expect(menu.sticky.value).toBe(false)
    expect(menu.show.value).toBe(false)
  })

  it('a fresh trigger overrides browse mode', async () => {
    const text = ref('@a')
    const items = ref<CompletionItem[]>([item('a.ts'), item('b.ts')])
    const menu = useCompletionMenu({
      items,
      getTrigger: () => parseAtQuery(text.value, text.value.length),
      onSelect: vi.fn(),
      closeOnSelect: false,
      stickyAfterSelect: true,
      getText: () => text.value,
      applyText: (v) => { text.value = v },
    })
    menu.refresh()
    await nextTick()
    menu.handleKeydown(keyEvent('Enter'))
    expect(menu.sticky.value).toBe(true)

    text.value = '@b'
    menu.refresh()
    await nextTick()
    expect(menu.sticky.value).toBe(false)
    expect(menu.show.value).toBe(true)
  })

  it('browse mode ends when the user edits the text', async () => {
    const text = ref('@a')
    const items = ref<CompletionItem[]>([item('a.ts')])
    const menu = useCompletionMenu({
      items,
      getTrigger: () => parseAtQuery(text.value, text.value.length),
      onSelect: vi.fn(),
      closeOnSelect: false,
      stickyAfterSelect: true,
      getText: () => text.value,
      applyText: (v) => { text.value = v },
    })
    menu.refresh()
    await nextTick()
    menu.handleKeydown(keyEvent('Enter'))
    expect(menu.sticky.value).toBe(true)
    expect(menu.show.value).toBe(true)

    // User starts typing a real message — the menu must disappear.
    text.value = 'hello '
    menu.refresh()
    await nextTick()
    expect(menu.sticky.value).toBe(false)
    expect(menu.show.value).toBe(false)
  })

  it('clearSticky leaves browse mode without closing', async () => {
    const text = ref('@a')
    const items = ref<CompletionItem[]>([item('a.ts')])
    const menu = useCompletionMenu({
      items,
      getTrigger: () => parseAtQuery(text.value, text.value.length),
      onSelect: vi.fn(),
      closeOnSelect: false,
      stickyAfterSelect: true,
      getText: () => text.value,
      applyText: (v) => { text.value = v },
    })
    menu.refresh()
    await nextTick()
    menu.handleKeydown(keyEvent('Enter'))
    menu.clearSticky()
    expect(menu.sticky.value).toBe(false)
    menu.refresh()
    await nextTick()
    expect(menu.show.value).toBe(false)
  })

  it('close() keeps the Esc dismissal latch (reopening the same trigger stays closed)', async () => {
    const text = ref('@a')
    const items = ref<CompletionItem[]>([item('a.ts')])
    const menu = useCompletionMenu({
      items,
      getTrigger: () => parseAtQuery(text.value, text.value.length),
      onSelect: vi.fn(),
      closeOnSelect: false,
      getText: () => text.value,
    })
    menu.refresh()
    await nextTick()
    expect(menu.show.value).toBe(true)

    menu.handleKeydown(keyEvent('Escape'))
    expect(menu.show.value).toBe(false)

    // An outside click closes the popup via the same close() path.
    menu.close()
    // Same trigger context must stay closed — close() must not release the latch.
    menu.refresh()
    await nextTick()
    expect(menu.show.value).toBe(false)

    // A genuinely new trigger context releases it.
    text.value = '@a '
    menu.refresh()
    await nextTick()
    text.value = '@b'
    menu.refresh()
    await nextTick()
    expect(menu.show.value).toBe(true)
  })

  it('selectByKey targets the exact item when keys repeat across sources', async () => {
    const text = ref('@a')
    const items = ref<CompletionItem[]>([
      { key: '/cb-task', label: '/cb-task', description: 'built-in', source: 'clawbench' },
      { key: '/cb-task', label: '/cb-task', description: 'agent', source: 'agent' },
    ])
    const onSelect = vi.fn()
    const menu = useCompletionMenu({
      items,
      getTrigger: () => parseAtQuery(text.value, text.value.length),
      onSelect,
      closeOnSelect: true,
      getText: () => text.value,
      applyText: (v) => { text.value = v },
    })
    menu.refresh()
    await nextTick()

    // Selecting the second entry must select the AGENT command, not the first.
    menu.selectByKey('/cb-task', 'agent')
    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(onSelect.mock.calls[0][0].source).toBe('agent')
  })
})
