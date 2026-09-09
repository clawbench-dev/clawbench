import { describe, it, expect } from 'vitest'

/**
 * Source-inspection tests (mirroring the ChatMessageList CodeLinkPreview
 * integration suite): the scheduled-task prompt (TaskOverviewTab) and the
 * execution detail (TaskExecDetail) must both wire up the code-link preview
 * for annotated file paths — instantiate the composable bound to the content
 * container, render CodeLinkPreview, and intercept verified .chat-file-path
 * clicks. The DOM mount would pull many unmocked deps, so we assert on the
 * component source instead.
 */
describe('TaskOverviewTab — CodeLinkPreview integration', () => {
  const getSource = async () => {
    const mod = await import('@/components/task/TaskOverviewTab.vue?raw')
    return typeof mod.default === 'string' ? mod.default : ''
  }

  it('imports CodeLinkPreview and useCodeLinkPreview', async () => {
    const source = await getSource()
    expect(source).toContain("import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'")
    expect(source).toContain('import { useCodeLinkPreview, handleVerifiedFilePathClick } from')
  })

  it('instantiates useCodeLinkPreview with containerRef bound to promptBodyRef', async () => {
    const source = await getSource()
    expect(source).toContain("const codeLinkPreview = useCodeLinkPreview({ containerRef: promptBodyRef, source: 'task' })")
  })

  it('renders CodeLinkPreview conditioned on codeLinkPreview.enabled.value', async () => {
    const source = await getSource()
    expect(source).toContain('<CodeLinkPreview')
    expect(source).toContain('v-if="codeLinkPreview.enabled.value"')
    expect(source).toContain(':preview="codeLinkPreview"')
  })

  it('delegates verified file-path clicks to the shared interceptor in handlePromptClick', async () => {
    const source = await getSource()
    expect(source).toContain('handleVerifiedFilePathClick(event, codeLinkPreview)')
  })
})

describe('TaskExecDetail — CodeLinkPreview integration', () => {
  const getSource = async () => {
    const mod = await import('@/components/task/TaskExecDetail.vue?raw')
    return typeof mod.default === 'string' ? mod.default : ''
  }

  it('imports CodeLinkPreview and useCodeLinkPreview', async () => {
    const source = await getSource()
    expect(source).toContain("import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'")
    expect(source).toContain('import { useCodeLinkPreview, handleVerifiedFilePathClick } from')
  })

  it('instantiates useCodeLinkPreview with containerRef bound to contentRef', async () => {
    const source = await getSource()
    expect(source).toContain("const codeLinkPreview = useCodeLinkPreview({ containerRef: contentRef, source: 'task' })")
  })

  it('renders CodeLinkPreview conditioned on codeLinkPreview.enabled.value', async () => {
    const source = await getSource()
    expect(source).toContain('<CodeLinkPreview')
    expect(source).toContain('v-if="codeLinkPreview.enabled.value"')
    expect(source).toContain(':preview="codeLinkPreview"')
  })

  it('delegates verified file-path clicks to the shared interceptor in handleContentClick', async () => {
    const source = await getSource()
    expect(source).toContain('handleVerifiedFilePathClick(event, codeLinkPreview)')
  })
})
