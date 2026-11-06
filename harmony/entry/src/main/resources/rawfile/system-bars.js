(() => {
  'use strict';
  if (window !== window.top) return;
  const host = window.ClawBenchHarmony || window.ClawBenchNative;
  if (!host?.setSystemBarAppearance) return;
  let scheduled = false;
  let closed = false;
  let lastColors = '';

  function background(element) {
    for (let node = element; node; node = node.parentElement) {
      const color = getComputedStyle(node).backgroundColor;
      const match = /^rgba?\(\s*(\d+)[,\s]+(\d+)[,\s]+(\d+)(?:\s*[,/]\s*([\d.]+))?\s*\)$/.exec(color);
      if (match && (match[4] === undefined || Number(match[4]) === 1)) {
        return '#' + match.slice(1, 4).map(value => Number(value).toString(16).padStart(2, '0')).join('');
      }
    }
    return '#17191d';
  }

  function sync() {
    scheduled = false;
    if (closed || !document.body) return;
    const top = background(document.querySelector('.header') || document.body);
    const dock = document.querySelector('.bottom-dock');
    const bottom = background(dock && dock.getBoundingClientRect().height > 0 ? dock : document.body);
    const key = top + bottom;
    if (key === lastColors) return;
    lastColors = key;
    Promise.resolve(host.setSystemBarAppearance(top, bottom)).catch(() => { lastColors = ''; });
  }

  function schedule() {
    if (closed || scheduled) return;
    scheduled = true;
    requestAnimationFrame(sync);
  }

  const observer = new MutationObserver(schedule);
  observer.observe(document.documentElement, {
    attributes: true, attributeFilter: ['data-theme', 'data-theme-base', 'class', 'style'], childList: true, subtree: true
  });
  window.addEventListener('DOMContentLoaded', schedule, { once: true });
  window.addEventListener('load', schedule, { once: true });
  window.addEventListener('resize', schedule);
  window.addEventListener('pagehide', () => {
    closed = true;
    observer.disconnect();
    window.removeEventListener('resize', schedule);
  }, { once: true });
  schedule();
})();
