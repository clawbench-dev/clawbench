(() => {
  'use strict';
  if (window !== window.top || location.protocol !== 'resource:' ||
      location.hostname.toLowerCase() !== 'rawfile' || location.pathname !== '/login/index.html') return;
  const config = JSON.parse(window.ClawBenchLoginInit.getConfig());
  let state = config.state;
  let selectedUrl = state.activeUrl || '';
  let port;
  let nextId = 0;
  const pending = new Map();
  const queued = new Map();
  let closed = false;

  function invoke(method, ...args) {
    if (closed) return Promise.reject(new Error('Login page closed'));
    return new Promise((resolve, reject) => {
      const id = ++nextId;
      const timer = setTimeout(() => {
        pending.delete(id);
        queued.delete(id);
        const error = new Error(state.language === 'en' ?
          'The local operation timed out. Please try again.' : '本地操作超时，请重试');
        error.code = 'local_timeout';
        reject(error);
      }, 45000);
      pending.set(id, { resolve, reject, timer });
      const request = JSON.stringify({ id, method, args: args.map(value => JSON.stringify(value)) });
      if (port) port.postMessage(request);
      else queued.set(id, request);
    });
  }

  window.addEventListener('message', event => {
    if (closed || port || event.data !== '__clawbench_login_port__:' + config.channel || !event.ports[0]) return;
    port = event.ports[0];
    port.onmessage = event => {
      let response;
      try { response = JSON.parse(event.data); } catch (_) { return; }
      const request = pending.get(response.id);
      if (!request) return;
      pending.delete(response.id);
      clearTimeout(request.timer);
      if (response.error) {
        const error = new Error(response.error);
        error.code = response.code || '';
        request.reject(error);
      } else {
        try {
          const result = JSON.parse(response.result);
          if (result.snapshot) {
            state = result.snapshot;
            if (!state.servers.some(server => server.url === selectedUrl)) {
              selectedUrl = state.activeUrl || '';
            }
          }
          request.resolve(result.value);
        } catch (error) {
          request.reject(error);
        }
      }
    };
    port.start();
    queued.forEach(message => port.postMessage(message));
    queued.clear();
  });

  const bridge = {
    getLanguage: () => state.language,
    getTheme: () => state.theme,
    getAppVersion: () => state.version,
    getServerList: () => JSON.stringify(state.servers),
    getActiveServer: () => selectedUrl,
    areSavedCredentialsIgnored: () => state.credentialsIgnored === true,
    selectServer: url => {
      selectedUrl = url;
      return invoke('selectServer', url);
    },
    ignoreSavedCredentials: () => invoke('ignoreSavedCredentials'),
    setSystemBarAppearance: (top, bottom) => invoke('setSystemBarAppearance', top, bottom),
    setTheme: theme => {
      state.theme = theme;
      void invoke('setAppearance', state.language, theme).catch(() => {});
    },
    saveServer: (url, credential) => invoke('saveServer', url, credential),
    saveServerNamed: (url, credential, name) => invoke('saveServerNamed', url, credential, name),
    removeServer: url => invoke('removeServer', url),
    connectToServer: (url, credential) => invoke('connectToServer', url, credential)
  };
  Object.defineProperty(window, 'ClawBenchNative', { value: bridge, configurable: false });
  Object.defineProperty(window, '__clawbenchLoginBack', {
    value: () => {
      if (window.connecting) return false;
      if (document.getElementById('dlgOverlay')?.style.display !== 'none') {
        document.getElementById('dlgCancel')?.click();
        return true;
      }
      if (document.getElementById('addServerForm')?.style.display !== 'none' && state.servers.length > 0) {
        window.hideAddForm();
        return true;
      }
      return false;
    }
  });
  window.addEventListener('pagehide', () => {
    closed = true;
    for (const request of pending.values()) {
      clearTimeout(request.timer);
      request.reject(new Error('Login page closed'));
    }
    pending.clear();
    queued.clear();
    port?.close();
    state = { ...state, servers: [] };
  }, { once: true });
})();
