import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'

// ── Mocks (hoisted so both the component import and the test body share them)
const bridgeState = vi.hoisted(() => {
  const state = {
    attach: vi.fn(() => {
      const dispose = vi.fn()
      state.disposeFns.push(dispose)
      return dispose
    }),
    disposeFns: [] as Array<ReturnType<typeof vi.fn>>,
    reset() {
      state.disposeFns = []
      state.attach.mockClear()
    },
  }
  return state
})

const quoteState = vi.hoisted(() => {
  const showBar = vi.fn()
  const hideBar = vi.fn()
  return { showBar, hideBar }
})

vi.mock('@/utils/iframeSelectionQuote.ts', () => ({
  attachIframeSelectionQuoteBridge: bridgeState.attach,
  // Real implementation: the component resolves the iframe window through it,
  // and the contract under test is "attach to the live iframe's window".
  resolveIframeWindow: async (live: () => HTMLIFrameElement | null) => live()?.contentWindow ?? null,
}))

vi.mock('@/utils/swaggerHtml.ts', () => ({
  buildSwaggerSrcdoc: () => '<html><body>swagger-doc</body></html>',
}))

vi.mock('@/utils/themeMeta', () => ({
  isDarkTheme: () => false,
}))

// The loading placeholder announces itself through a translated label, so the
// component needs an i18n instance.
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

vi.mock('@/composables/useQuoteQuestion.ts', () => ({
  useQuoteQuestion: () => ({
    showBar: quoteState.showBar,
    hideBar: quoteState.hideBar,
  }),
  // The component now routes its bridge callback through this shared factory
  // (three iframe hosts used to hand-roll the same three decisions, and this one
  // silently dropped `anchor`). The mock must provide it, or the attach path
  // throws and the bridge never registers. It is reproduced rather than
  // imported so this suite still isolates the component from the real
  // composable — the factory's own behaviour is covered where it lives.
  makeIframeQuoteShowHandler: (showBar: (d: unknown, o?: unknown) => void) =>
    (data: unknown, anchor: unknown) =>
      showBar(data, { delay: 0, anchor: anchor ?? undefined, suppressBar: false }),
}))

import OpenApiPreview from '../OpenApiPreview.vue'

function makeFile(overrides: Record<string, unknown> = {}) {
  return {
    name: 'api.yaml',
    path: '/specs/api.yaml',
    content: '{"openapi":"3.0.0"}',
    ...overrides,
  }
}

function mountPreview(props: Record<string, unknown> = {}) {
  return mount(OpenApiPreview, {
    props: {
      file: makeFile(),
      viewMode: 'rendered',
      ...props,
    },
    // jsdom only creates an iframe browsing context (contentWindow) when the
    // iframe is connected to the live document.
    attachTo: document.body,
  })
}

async function waitFor(pred: () => boolean, ms = 200): Promise<boolean> {
  const start = Date.now()
  while (Date.now() - start < ms) {
    if (pred()) return true
    await new Promise((r) => setTimeout(r, 5))
  }
  return pred()
}

/** Fire a load on the current iframe and wait for the bridge to (re)attach. */
async function loadIframeAndAttach(wrapper: ReturnType<typeof mountPreview>) {
  const iframe = wrapper.find('iframe')
  await iframe.trigger('load')
  // The component attaches asynchronously (microtask loop) after the load.
  await waitFor(() => bridgeState.attach.mock.calls.length > 0)
}

describe('OpenApiPreview quote bridge', () => {
  beforeEach(() => {
    bridgeState.reset()
    quoteState.showBar.mockClear()
    quoteState.hideBar.mockClear()
    document.body.innerHTML = ''
  })

  afterEach(() => {
    bridgeState.reset()
  })

  it('does NOT attach a quote bridge when chatQuote is false (share-mode default)', async () => {
    const wrapper = mountPreview()
    // Even after loads fire, no bridge may be created without chatQuote.
    await wrapper.find('iframe').trigger('load')
    await new Promise((r) => setTimeout(r, 30))
    expect(bridgeState.attach).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('attaches a quote bridge to the iframe content window when chatQuote is true', async () => {
    const wrapper = mountPreview({ chatQuote: true })
    await loadIframeAndAttach(wrapper)

    expect(bridgeState.attach).toHaveBeenCalledTimes(1)
    const opts = bridgeState.attach.mock.calls[0][0]
    // The bridge receives the sandboxed iframe window (allow-same-origin).
    const iframeEl = document.querySelector('.openapi-iframe') as HTMLIFrameElement | null
    expect(opts.win).toBe(iframeEl?.contentWindow)
    wrapper.unmount()
  })

  it('surfaces selections through the shared handler (delay 0) and clears via hideBar', async () => {
    const wrapper = mountPreview({ chatQuote: true })
    await loadIframeAndAttach(wrapper)
    const opts = bridgeState.attach.mock.calls[0][0]

    opts.show({ text: 'GET /pets', filePath: '/specs/api.yaml', language: '', startLine: 0, endLine: 0 })
    // The bridge now hands over the selection's HOST-viewport anchor as a second
    // argument. Losing it was this host's bug: without an anchor `showBar` falls
    // back to its pinned top position, which is the reported "menu pops up at the
    // top of the page". Passing it through is the contract, so assert it is
    // forwarded rather than ignored.
    const [payload, optsArg] = quoteState.showBar.mock.calls[0] as [unknown, Record<string, unknown>]
    expect(payload).toEqual({
      text: 'GET /pets', filePath: '/specs/api.yaml', language: '', startLine: 0, endLine: 0,
    })
    expect(optsArg.delay).toBe(0)
    expect(optsArg).toHaveProperty('anchor')
    // Android draws its own menu; the in-page bar must be suppressed there. This
    // host never passed the flag, so it double-popped.
    expect(optsArg).toHaveProperty('suppressBar')

    opts.hide()
    expect(quoteState.hideBar).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('resolves filePath from the current file at selection time', async () => {
    const wrapper = mountPreview({ chatQuote: true })
    await loadIframeAndAttach(wrapper)
    const opts = bridgeState.attach.mock.calls[0][0]

    expect(opts.filePath()).toBe('/specs/api.yaml')

    await wrapper.setProps({ file: makeFile({ path: '/specs/v2.yaml' }) })
    expect(opts.filePath()).toBe('/specs/v2.yaml')
    wrapper.unmount()
  })

  it('disposes the previous bridge when the spec reloads, then re-attaches on the next load', async () => {
    const wrapper = mountPreview({ chatQuote: true })
    await loadIframeAndAttach(wrapper)
    expect(bridgeState.attach).toHaveBeenCalledTimes(1)
    expect(bridgeState.disposeFns).toHaveLength(1)

    // Spec change → loading flips → srcdoc replaced → old bridge disposed.
    await wrapper.setProps({ file: makeFile({ content: '{"openapi":"3.0.0","paths":{}}' }) })
    expect(bridgeState.disposeFns[0]).toHaveBeenCalledTimes(1)

    // New document loads → a fresh bridge is attached.
    const attachCountBefore = bridgeState.attach.mock.calls.length
    await wrapper.find('iframe').trigger('load')
    await waitFor(() => bridgeState.attach.mock.calls.length > attachCountBefore)
    expect(bridgeState.attach.mock.calls.length).toBe(attachCountBefore + 1)
    expect(bridgeState.disposeFns).toHaveLength(2)
    wrapper.unmount()
  })

  it('disposes the bridge on unmount', async () => {
    const wrapper = mountPreview({ chatQuote: true })
    await loadIframeAndAttach(wrapper)
    expect(bridgeState.disposeFns).toHaveLength(1)

    wrapper.unmount()
    expect(bridgeState.disposeFns[0]).toHaveBeenCalledTimes(1)
  })
})
