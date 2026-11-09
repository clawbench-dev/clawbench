(() => {
  'use strict';
  if (window !== window.top || !window.ClawBenchHarmony) return;
  let hardwareEnter;
  const markHardwareEnter = () => {
    hardwareEnter = { target: document.activeElement, time: performance.now() };
  };
  window.__clawbenchHardwareEnter = markHardwareEnter;
  function reset() {
    hardwareEnter = undefined;
  }
  function keydown(event) {
    const hardware = hardwareEnter;
    reset();
    if (event.key !== 'Enter' || event.isComposing || event.keyCode === 229 ||
        event.shiftKey || event.ctrlKey || event.metaKey || event.altKey ||
        !event.target?.matches?.('textarea.chat-textarea')) return;
    if (hardware?.target === event.target && performance.now() - hardware.time < 1000) return;
    // ArkWeb's IME Enter has code="Enter", just like a hardware key. Let the
    // browser insert its line break, without reaching the server's send handler.
    event.stopImmediatePropagation();
  }
  document.addEventListener('keydown', keydown, true);
  window.addEventListener('blur', reset);
  window.addEventListener('pagehide', () => {
    reset();
    document.removeEventListener('keydown', keydown, true);
    window.removeEventListener('blur', reset);
    if (window.__clawbenchHardwareEnter === markHardwareEnter) {
      delete window.__clawbenchHardwareEnter;
    }
  }, { once: true });
})();
