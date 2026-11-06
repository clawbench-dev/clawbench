(() => {
  'use strict';
  const config = __HARMONY_CONFIG__;
  if (window !== window.top || location.origin !== config.origin ||
      !['/', '/login'].includes(location.pathname)) return;
  let port;
  let closed = false;
  let nextId = 0;
  const pending = new Map();
  const queue = new Map();
  const nativeEvents = new Set(['clawbench-download-progress', 'clawbench-native-notification-status',
    'clawbench-open-session', 'clawbench-open-task', 'clawbench-open-forge',
    'clawbench-open-usage-meter', 'clawbench-open-meeting', 'clawbench-native-error',
    'clawbench-selection-action', 'clawbench-volume-key', 'clawbench-share-imported',
    'clawbench-port-rebound']);
  function invoke(method, ...args) {
    return new Promise((resolve, reject) => {
      if (closed || pending.size >= 64) {
        reject(new Error(closed ? 'HarmonyOS page closed' : 'Too many native operations'));
        return;
      }
      const id = ++nextId;
      const timer = setTimeout(() => {
        pending.delete(id);
        queue.delete(id);
        reject(new Error('HarmonyOS bridge timed out: ' + method));
      }, ['downloadBlob', 'shareFile', 'shareFiles', 'shareText', 'readClipboardText',
        'showSelectionMenu', 'reconnectTunnel', 'reconnectTunnelAsync',
        'addForwardedPort', 'addReverseForwardedPort', 'openInBrowser',
        'openInSandbox'].includes(method) ? 300000 : 20000);
      pending.set(id, { resolve, reject, timer });
      const message = JSON.stringify({ id, method, args: args.map(arg => JSON.stringify(arg)) });
      if (port) port.postMessage(message);
      else queue.set(id, message);
    });
  }
  function fire(method, ...args) {
    void invoke(method, ...args).catch(error => {
      if (!closed) window.dispatchEvent(new CustomEvent('clawbench-native-error', {
        detail: { method, message: error.message }
      }));
    });
  }
  window.addEventListener('message', event => {
    if (closed || event.data !== '__clawbench_harmony_port__:' + config.channel || !event.ports[0] || port) return;
    port = event.ports[0];
    port.onmessage = message => {
      if (closed) return;
      let response;
      try { response = JSON.parse(message.data); } catch { return; }
      if (!response || typeof response !== 'object' || Array.isArray(response)) return;
      if (response.event) {
        if (!nativeEvents.has(response.event) || !response.detail ||
            typeof response.detail !== 'object' || Array.isArray(response.detail)) return;
        if (response.event === 'clawbench-selection-action') {
          window.__clawbenchSelectionAction?.(response.detail.action);
        } else if (response.event === 'clawbench-volume-key') {
          window.__onVolumeKey?.(response.detail.direction);
        } else {
          window.dispatchEvent(new CustomEvent(response.event, { detail: response.detail }));
        }
        return;
      }
      const request = pending.get(response.id);
      if (!request) return;
      clearTimeout(request.timer);
      pending.delete(response.id);
      if (response.error) request.reject(new Error(response.error));
      else {
        try { request.resolve(JSON.parse(response.result)); }
        catch { request.reject(new Error('Invalid native response')); }
      }
    };
    port.start();
    queue.forEach(message => port.postMessage(message));
    queue.clear();
  });
  const bridge = {
    isNativeApp: () => true,
    isDesktopApp: () => false,
    getHostPlatform: () => 'harmonyos',
    getCapabilities: () => ({
      serverManagement: true,
      appearance: true,
      keepScreenOn: true,
      fileChooser: true,
      nativeDownload: true,
      nativeShare: true,
      nativePush: true,
      floatingStatus: false,
      liveUpdate: false,
      tunnel: true,
      screenCapture: typeof navigator !== 'undefined' && typeof navigator.mediaDevices?.getDisplayMedia === 'function',
      mediaCapture: typeof navigator !== 'undefined' && typeof navigator.mediaDevices?.getUserMedia === 'function',
      selectionMenu: true,
      clipboard: true,
      systemBrowser: true,
      volumeKeys: true,
      backgroundDelivery: 'while-process-running'
    }),
    getLanguage: () => config.language,
    getTheme: () => config.theme,
    setSystemBarAppearance: (top, bottom) => invoke('setSystemBarAppearance', top, bottom),
    setLanguage: language => {
      config.language = language === 'en' ? 'en' : 'zh';
      void invoke('setAppearance', config.language, config.theme).catch(() => {});
    },
    setTheme: theme => {
      config.theme = theme;
      void invoke('setAppearance', config.language, config.theme).catch(() => {});
    },
    getAppVersion: async () => config.version,
    // Authentication remains in the native host; remote pages never receive saved passwords.
    getPassword: async () => '',
    setSSHPassword: password => {
      const operation = invoke('setSSHPassword', password);
      // Shared login callers may fire and forget; awaited callers still receive rejection.
      void operation.catch(error => {
        if (!closed) window.dispatchEvent(new CustomEvent('clawbench-native-error', {
          detail: { method: 'setSSHPassword', message: error.message }
        }));
      });
      return operation;
    },
    isScreenCaptureSupported: async () => bridge.getCapabilities().screenCapture,
    isFloatingWindowEnabled: () => false,
    canPostPromotedNotifications: () => false,
    isChineseOem: () => false,
    getOemName: () => '',
    isOemAutoStartPrompted: () => true,
    openOemAutoStartSettings: () => false,
    openOemBatterySettings: () => false,
    setNativePushEnabled: enabled => fire('setNativePushEnabled', enabled),
    setFloatingWindowEnabled: enabled => {
      if (enabled) throw new Error('HarmonyOS floating status is not implemented');
    },
    setLiveUpdateEnabled: enabled => {
      if (enabled) throw new Error('Android Live Updates are unavailable on HarmonyOS');
    },
    updateLastSeenEventId: id => fire('updateLastSeenEventId', id),
    stopBackgroundService: () => fire('stopBackgroundService'),
    setVolumeKeyMode: enabled => fire('setVolumeKeyMode', enabled),
    setTerminalSessionCount: () => {},
    setOemAutoStartPrompted: () => {},
    log: (level, tag, message) => fire('log', level, tag, message),
    // This method acknowledges acceptance through RPC; completion is exclusively an event.
    // Returning its Promise would make the shared download bar finish before the save picker.
    downloadFileWithProgress: (path, name, id) => {
      void invoke('downloadFileWithProgress', path, name, id).catch(() => {
        if (!closed) window.dispatchEvent(new CustomEvent('clawbench-download-progress', {
          detail: { id, received: 0, total: 0, done: true, error: true }
        }));
      });
    },
    showSelectionMenu: (...args) => fire('showSelectionMenu', ...args),
    openSession: sessionId => fire('openSession', sessionId),
    dismissEventNotification: (taskId, sessionId) => fire('dismissEventNotification', taskId, sessionId)
  };
  for (const method of ['getServerList', 'getSavedServerConfig', 'getServerUrl',
    'saveServer', 'saveServerNamed', 'removeServer', 'connectToServer',
    'openExternalUrl', 'setKeepScreenOn', 'reloadApp', 'getPendingNavigation',
    'getNativePushStatus', 'cancelDownload', 'downloadFile', 'downloadUrl', 'downloadBlob',
    'shareText', 'shareFile', 'shareFiles', 'readClipboardText', 'writeClipboardText',
    'openInBrowser', 'openInSandbox', 'startLogCapture', 'stopLogCapture',
    'getForwardedPorts', 'isTunnelConnected', 'testPortReachable', 'getTunnelError', 'getTunnelErrorType',
    'getTunnelTransport', 'getActiveTunnelTransport', 'getTunnelTransportH2Enabled',
    'setTunnelTransportH2Enabled', 'reconnectTunnel', 'reconnectTunnelAsync', 'addForwardedPort',
    'addReverseForwardedPort', 'removeForwardedPort', 'removeReverseForwardedPort', 'forgetForwardedPorts']) {
    bridge[method] = (...args) => invoke(method, ...args);
  }
  for (const method of ['openInBrowser', 'openInSandbox']) {
    bridge[method] = (...args) => invoke(method, ...args).catch(error => {
      if (!closed) window.dispatchEvent(new CustomEvent('clawbench-native-error', {
        detail: { method, message: error.message }
      }));
      throw error;
    });
  }
  for (const method of ['dismissSplash', 'rendererReady', 'showServerDialog']) {
    bridge[method] = (...args) => { void invoke(method, ...args).catch(() => {}); };
  }
  window.addEventListener('pagehide', () => {
    closed = true;
    for (const request of pending.values()) {
      clearTimeout(request.timer);
      request.reject(new Error('HarmonyOS page closed'));
    }
    pending.clear();
    queue.clear();
    port?.close();
  }, { once: true });
  // Only a frontend with explicit HarmonyOS support reads this name.
  Object.defineProperty(window, 'ClawBenchHarmony', { value: bridge, configurable: false });
})();
