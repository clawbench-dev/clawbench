import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import TableRowModal from '@/components/common/TableRowModal.vue'

const CELL_HTML = (src: string, fullSrc: string) =>
  `<span class="lightbox-img-wrap"><img class="lightbox-img" src="${src}" data-full-src="${fullSrc}" alt="a"><span class="lightbox-expand-icon"></span></span>`

async function mountModal(cellHtml: string) {
  // Fresh i18n per mount — avoids vue-i18n devtools install flake on repeat mounts.
  const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh: {}, en: {} } })
  const openLightbox = vi.fn()
  const openMdImages = vi.fn()
  mount(TableRowModal, {
    props: {
      data: { headers: ['图片'], rows: [[cellHtml]], currentIndex: 0 },
    },
    global: { plugins: [i18n], provide: { openLightbox, openMdImages } },
  })
  await nextTick()
  await nextTick()
  return { openLightbox, openMdImages }
}

describe('TableRowModal image lightbox', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('opens the lightbox with the full-size src when the image body is clicked', async () => {
    const { openLightbox } = await mountModal(CELL_HTML('/api/file/thumb?path=img/logo.png&w=800', '/api/local-file/img/logo.png'))
    const img = document.querySelector('.table-row-value .lightbox-img') as HTMLElement | null
    expect(img).toBeTruthy()
    img!.click()
    await nextTick()
    expect(openLightbox).toHaveBeenCalledTimes(1)
    expect(openLightbox).toHaveBeenCalledWith('/api/local-file/img/logo.png')
  })

  it('opens the lightbox with the full-size src when the expand icon is clicked', async () => {
    const { openLightbox } = await mountModal(CELL_HTML('/api/file/thumb?path=img/logo.png&w=800', '/api/local-file/img/logo.png'))
    const icon = document.querySelector('.table-row-value .lightbox-expand-icon') as HTMLElement | null
    expect(icon).toBeTruthy()
    icon!.click()
    await nextTick()
    expect(openLightbox).toHaveBeenCalledTimes(1)
    expect(openLightbox).toHaveBeenCalledWith('/api/local-file/img/logo.png')
  })

  it('falls back to the inline src when there is no data-full-src', async () => {
    const { openLightbox } = await mountModal('<span class="lightbox-img-wrap"><img class="lightbox-img" src="/api/local-file/img/logo.png" alt="a"></span>')
    const img = document.querySelector('.table-row-value .lightbox-img') as HTMLElement | null
    img!.click()
    await nextTick()
    expect(openLightbox).toHaveBeenCalledTimes(1)
    expect(openLightbox).toHaveBeenCalledWith(expect.stringContaining('/api/local-file/img/logo.png'))
  })
})
