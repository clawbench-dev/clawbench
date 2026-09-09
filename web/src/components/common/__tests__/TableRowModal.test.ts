import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import TableRowModal from '@/components/common/TableRowModal.vue'

const CELL_HTML = (src: string, fullSrc: string) =>
  `<div class="image-block-wrapper"><div class="image-block-header"><span class="image-block-header-actions"><button type="button" class="image-block-view-btn" title="View image" aria-label="View image"></button></span></div><span class="lightbox-img-wrap"><img class="lightbox-img" src="${src}" data-full-src="${fullSrc}" alt="a"></span></div>`

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

  it('opens the lightbox with the full-size src when the figure view button is clicked', async () => {
    const { openLightbox } = await mountModal(CELL_HTML('/api/file/thumb?path=img/logo.png&w=800', '/api/local-file/img/logo.png'))
    const btn = document.querySelector('.table-row-value .image-block-view-btn') as HTMLElement | null
    expect(btn).toBeTruthy()
    btn!.click()
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
