/*
 * ClawBench 用户手册 —— 页面交互。
 * 与落地页一样是零依赖的原生脚本：没有构建步骤，直接由浏览器加载。
 */
(function () {
  'use strict'

  /* ── 移动端目录抽屉 ──────────────────────────────────────── */
  var side = document.getElementById('docsSide')
  var menuBtn = document.getElementById('docsMenuBtn')
  var scrim = document.getElementById('docsScrim')

  function setMenu(open) {
    if (!side || !menuBtn) return
    side.classList.toggle('is-open', open)
    menuBtn.setAttribute('aria-expanded', open ? 'true' : 'false')
    if (scrim) scrim.hidden = !open
    document.body.classList.toggle('docs-menu-open', open)
  }

  if (menuBtn) {
    menuBtn.addEventListener('click', function () {
      setMenu(!side.classList.contains('is-open'))
    })
  }
  if (scrim) {
    scrim.addEventListener('click', function () {
      setMenu(false)
    })
  }

  /* ── 侧栏筛选 ────────────────────────────────────────────── */
  var filter = document.getElementById('docsFilter')
  var groups = Array.prototype.slice.call(document.querySelectorAll('.docs-nav .docs-group'))

  function applyFilter() {
    var q = (filter.value || '').trim().toLowerCase()
    groups.forEach(function (group) {
      var visible = 0
      Array.prototype.forEach.call(group.querySelectorAll('li'), function (li) {
        var hit = !q || li.textContent.toLowerCase().indexOf(q) !== -1
        li.hidden = !hit
        if (hit) visible++
      })
      group.classList.toggle('is-empty', visible === 0)
    })
  }

  if (filter) {
    filter.addEventListener('input', applyFilter)
    filter.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') {
        filter.value = ''
        applyFilter()
        filter.blur()
      }
    })
  }

  /* 按 / 聚焦筛选框（沿用 GitHub / MDN 的手感） */
  document.addEventListener('keydown', function (e) {
    if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return
    var tag = (e.target.tagName || '').toLowerCase()
    if (tag === 'input' || tag === 'textarea' || e.target.isContentEditable) return
    if (!filter) return
    e.preventDefault()
    filter.focus()
    filter.select()
  })

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') setMenu(false)
  })

  /* ── 本页目录跟随滚动 ────────────────────────────────────── */
  var tocLinks = Array.prototype.slice.call(document.querySelectorAll('.docs-toc a'))
  var headings = tocLinks
    .map(function (a) {
      var id = (a.getAttribute('href') || '').slice(1)
      return id ? document.getElementById(id) : null
    })
    .filter(Boolean)

  function syncToc() {
    if (!headings.length) return
    var active = headings[0]
    for (var i = 0; i < headings.length; i++) {
      if (headings[i].getBoundingClientRect().top <= 110) active = headings[i]
      else break
    }
    tocLinks.forEach(function (a) {
      a.classList.toggle('is-active', a.getAttribute('href') === '#' + active.id)
    })
  }

  if (headings.length) {
    var ticking = false
    window.addEventListener(
      'scroll',
      function () {
        if (ticking) return
        ticking = true
        window.requestAnimationFrame(function () {
          syncToc()
          ticking = false
        })
      },
      { passive: true }
    )
    syncToc()
  }

  /* ── 代码块复制 ──────────────────────────────────────────── */
  Array.prototype.forEach.call(document.querySelectorAll('pre.code-block'), function (pre) {
    var btn = document.createElement('button')
    btn.type = 'button'
    btn.className = 'code-copy'
    btn.textContent = '复制'
    btn.addEventListener('click', function () {
      var code = pre.querySelector('code')
      var text = code ? code.textContent : ''
      var done = function () {
        btn.textContent = '已复制'
        btn.classList.add('is-done')
        window.setTimeout(function () {
          btn.textContent = '复制'
          btn.classList.remove('is-done')
        }, 1600)
      }
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done, function () {})
      } else {
        // 非安全上下文（http 访问）没有 clipboard API，退回选中文本
        var range = document.createRange()
        range.selectNodeContents(code)
        var sel = window.getSelection()
        sel.removeAllRanges()
        sel.addRange(range)
        done()
      }
    })
    pre.appendChild(btn)
  })

  /* ── 截图放大 ────────────────────────────────────────────── */
  var lightbox = document.getElementById('lightbox')
  var lightboxImg = document.getElementById('lightboxImg')

  function openLightbox(src, alt) {
    if (!lightbox || !lightboxImg) return
    lightboxImg.src = src
    lightboxImg.alt = alt || ''
    lightbox.hidden = false
  }

  function closeLightbox() {
    if (!lightbox || lightbox.hidden) return
    lightbox.hidden = true
    lightboxImg.removeAttribute('src')
  }

  Array.prototype.forEach.call(document.querySelectorAll('.prose img'), function (img) {
    img.addEventListener('click', function () {
      openLightbox(img.currentSrc || img.src, img.alt)
    })
  })

  if (lightbox) {
    lightbox.addEventListener('click', function (e) {
      if (e.target === lightbox || e.target.closest('.lightbox-close')) closeLightbox()
    })
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') closeLightbox()
    })
  }

  /* ── 流程图 ──────────────────────────────────────────────── */
  var diagrams = Array.prototype.slice.call(document.querySelectorAll('.mermaid'))
  if (diagrams.length && window.mermaid) {
    diagrams.forEach(function (el) {
      el.setAttribute('data-source', el.textContent)
    })
    window.mermaid.initialize({
      startOnLoad: false,
      theme: 'dark',
      themeVariables: { background: '#0c0a07', fontSize: '14px' },
      flowchart: { curve: 'basis' },
    })
    window.mermaid.run({ nodes: diagrams }).catch(function () {
      // 渲染失败就退回源码，至少内容不丢
      diagrams.forEach(function (el) {
        if (!el.querySelector('svg')) el.textContent = el.getAttribute('data-source')
      })
    })
  }
})()
