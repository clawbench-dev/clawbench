/* app.js — 与 index.html 同目录，用相对路径引用。
 *
 * 这个文件存在本身就是测试点：修复前 /api/fs/raw/ 对它返回
 * application/octet-stream。普通 <script src> 仍会执行（该端点未设 nosniff），
 * 但 <script type="module"> 会被拒绝 —— 所以这里用 module 形式，
 * 让「JS 是否真的加载」成为一个有区分度的判据。
 */

/** 把某个资源的加载结果标成 通过 / 失败，而不是只写一行「已加载」。 */
function markResult(id, ok, detail) {
  const el = document.getElementById(id);
  if (!el) return;
  el.className = 'result ' + (ok ? 'pass' : 'fail');
  el.textContent = ok ? '✓ 已加载' : '✗ 失败';
  if (detail) el.title = detail;
}

/** 单项检查抛错不应影响其余检查 —— 否则一个失败会让后面全部停在「检测中」。 */
function safe(fn) {
  try { fn(); } catch (e) { console.error('check failed:', e); }
}

/** 图片：用 naturalWidth 判定。onerror 只说明请求失败，宽度为 0 才是真没解码。 */
function checkImage(imgEl, resultId) {
  const done = () => markResult(resultId, imgEl.naturalWidth > 0,
    `naturalWidth=${imgEl.naturalWidth}`);
  if (imgEl.complete) done();
  else {
    imgEl.addEventListener('load', done, { once: true });
    imgEl.addEventListener('error', () => markResult(resultId, false, 'onerror'), { once: true });
  }
}

/** 外部样式表：读 :root 上的 --accent。该变量只定义在 style.css 里，
 *  所以内联样式表无法伪造它 —— 判据与「文件是否真的被应用」同源。 */
function checkStylesheet() {
  const color = getComputedStyle(document.documentElement)
    .getPropertyValue('--accent').trim();
  markResult('style-status', color === '#2563eb', `--accent=${color || '(空)'}`);
}

/** CSS 里 url() 引用的背景图：用 backgroundImage 判定。
 *  它相对 CSS 自身的 URL 解析，而不是相对 HTML。 */
function checkCssBackground() {
  const el = document.querySelector('.deco');
  const bg = getComputedStyle(el).backgroundImage;
  markResult('css-bg-status', !!bg && bg !== 'none', bg);
}

window.addEventListener('DOMContentLoaded', () => {
  // JS 能跑到这里，说明 module script 加载并执行了。
  markResult('js-status', true, 'module script 已执行');

  document.querySelectorAll('img[data-check]').forEach((img) => {
    safe(() => checkImage(img, img.dataset.check));
  });

  safe(checkStylesheet);
  safe(checkCssBackground);

  const obj = document.querySelector('object[data-check]');
  if (obj) {
    // <object> 没有 naturalWidth；用其文档是否可用作判据，并在超时后判失败，
    // 避免一个永不触发的 load 事件让该行永远停在「检测中」。
    let settled = false;
    const settle = (ok, detail) => {
      if (settled) return;
      settled = true;
      markResult(obj.dataset.check, ok, detail);
    };
    obj.addEventListener('load', () => settle(true, 'object onload'));
    obj.addEventListener('error', () => settle(false, 'object onerror'));
    setTimeout(() => settle(false, '超时：load/error 均未触发'), 3000);
  }

  // 内联 SVG 永远能显示，作为对照组。
  markResult('inline-status', true, '内联，不依赖外部请求');
});

export const DEMO_LOADED = true;
