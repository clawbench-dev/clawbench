import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import MilkdownEditor from '../MilkdownEditor.vue'

const i18n = createI18n({
    legacy: false,
    locale: 'en',
    messages: {
        en: {
            file: {
                editor: {
                    save: 'Save',
                    cancel: 'Cancel',
                    undo: 'Undo',
                    redo: 'Redo',
                    exitEdit: 'Exit edit',
                    confirmExit: 'Save changes?',
                    dontSave: "Don't save",
                },
            },
            common: { cancel: 'Cancel', confirm: 'Confirm' },
        },
    },
})

function mountEditor(content: string, file: Record<string, unknown> | null = null) {
    return mount(MilkdownEditor, {
        props: { content, file },
        global: { plugins: [i18n] },
    })
}

/** The editor is created asynchronously in onMounted. */
async function waitForEditor(wrapper: ReturnType<typeof mount>) {
    for (let i = 0; i < 40; i++) {
        await flushPromises()
        if (wrapper.find('.ProseMirror').exists()) return
    }
    throw new Error('Milkdown editor did not mount')
}

describe('MilkdownEditor', () => {
    beforeEach(() => {
        document.body.innerHTML = ''
    })
    afterEach(() => {
        document.body.innerHTML = ''
    })

    it('renders the markdown as an editable document', async () => {
        const wrapper = mountEditor('# 标题\n\n- 第一项\n- 第二项\n')
        await waitForEditor(wrapper)

        const prose = wrapper.find('.ProseMirror')
        expect(prose.exists()).toBe(true)
        expect(prose.attributes('contenteditable')).toBe('true')
        expect(wrapper.text()).toContain('标题')
        expect(wrapper.text()).toContain('第一项')

        wrapper.unmount()
    })

    it('starts clean and serializes the document back to markdown', async () => {
        const source = '# Title\n\nSome **bold** text.\n'
        const wrapper = mountEditor(source)
        await waitForEditor(wrapper)

        expect(wrapper.vm.isDirty()).toBe(false)
        const value = wrapper.vm.getValue()
        expect(value).toContain('# Title')
        expect(value).toContain('**bold**')

        wrapper.unmount()
    })

    it('exits immediately when nothing changed', async () => {
        const wrapper = mountEditor('# Title\n')
        await waitForEditor(wrapper)

        const exited = await wrapper.vm.handleExit()
        expect(exited).toBe(true)
        expect(wrapper.emitted('exitEdit')).toBeTruthy()

        wrapper.unmount()
    })

    it('starts clean even when the serializer rewrites the source formatting', async () => {
        // Milkdown pads table columns and escapes `_` inside cells. The editor
        // must treat that normalization as the baseline, otherwise opening such
        // a file looks like "unsaved changes" before the user touched anything.
        const source = '| 列一 | 列二 |\n|---|---|\n| a_b | >10 |\n'
        const wrapper = mountEditor(source)
        await waitForEditor(wrapper)

        expect(wrapper.vm.getValue()).not.toBe(source)
        expect(wrapper.vm.isDirty()).toBe(false)

        wrapper.unmount()
    })

    it('rewrites a relative image src to a served URL without touching the document', async () => {
        const source = '# Title\n\n![shot](screenshots/a.png)\n'
        const wrapper = mountEditor(source, { name: 'app.md', path: 'docs/user-guide/app.md' })
        await waitForEditor(wrapper)

        // ProseMirror adds its own invisible separator <img>; target ours.
        const img = wrapper.find('.ProseMirror img.md-editor-image')
        expect(img.exists()).toBe(true)
        expect(img.attributes('src')).toMatch(/^\/api\/fs\/raw\/docs\/user-guide\/screenshots\/a\.png\?t=/)
        // The markdown must still carry the original relative path.
        expect(wrapper.vm.getValue()).toContain('![shot](screenshots/a.png)')

        wrapper.unmount()
    })

    it('renders inline and display formulas and keeps their source', async () => {
        const source = '行内 $a_1 + b_2$ 公式\n\n$$\n\\int_0^1 x^2 dx\n$$\n'
        const wrapper = mountEditor(source)
        await waitForEditor(wrapper)

        expect(wrapper.findAll('.ProseMirror .katex').length).toBeGreaterThan(0)
        expect(wrapper.find('.ProseMirror .md-math-inline').exists()).toBe(true)
        expect(wrapper.find('.ProseMirror .md-math-block').exists()).toBe(true)

        const value = wrapper.vm.getValue()
        expect(value).toContain('$a_1 + b_2$')
        expect(value).toContain('\\int_0^1 x^2 dx')

        wrapper.unmount()
    })

    it('colours a fenced code block with the shared highlight classes', async () => {
        const source = '```js\nconst answer = 42\n```\n'
        const wrapper = mountEditor(source)
        await waitForEditor(wrapper)

        const code = wrapper.find('.ProseMirror .md-code-block code')
        expect(code.exists()).toBe(true)
        // Decorations are computed when the editor state is created.
        expect(code.findAll('span[class*="hljs-"]').length).toBeGreaterThan(0)
        // The document still holds plain text (decorations never rewrite it).
        expect(wrapper.vm.getValue()).toContain('const answer = 42')

        wrapper.unmount()
    })

    it('keeps a mermaid fence editable and mounts a preview slot for it', async () => {
        const source = '```mermaid\ngraph TD\n  A --> B\n```\n'
        const wrapper = mountEditor(source)
        await waitForEditor(wrapper)

        expect(wrapper.find('.ProseMirror .md-code-block .md-code-preview').exists()).toBe(true)
        expect(wrapper.find('.ProseMirror .md-code-block pre code').text()).toContain('graph TD')
        expect(wrapper.vm.getValue()).toContain('```mermaid')

        // The preview slot is revealed after the debounce, even if the diagram
        // itself cannot be measured in jsdom (the util paints its own card).
        await new Promise((resolve) => setTimeout(resolve, 600))
        const preview = wrapper.find('.ProseMirror .md-code-preview')
        expect(preview.attributes('hidden')).toBeUndefined()

        wrapper.unmount()
    })
})
