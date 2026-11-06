(() => {
  'use strict';
  const config = __HARMONY_CONFIG__;
  if (window !== window.top || location.origin !== config.origin ||
      !['/', '/login'].includes(location.pathname)) return;
  let port;
  let nextId = 0;
  const pending = new Map();
  const queue = new Map();
  function invoke(method, ...args) {
    return new Promise((resolve, reject) => {
      const id = ++nextId;
      const timer = setTimeout(() => {
        pending.delete(id);
        queue.delete(id);
        reject(new Error('HarmonyOS bridge timed out: ' + method));
      }, 20000);
      pending.set(id, { resolve, reject, timer });
      const message = JSON.stringify({ id, method, args: args.map(arg => JSON.stringify(arg)) });
      if (port) port.postMessage(message);
      else queue.set(id, message);
    });
  }
  window.addEventListener('message', event => {
    if (event.data !== '__clawbench_harmony_port__:' + config.channel || !event.ports[0] || port) return;
    port = event.ports[0];
    port.onmessage = message => {
      const response = JSON.parse(message.data);
      const request = pending.get(response.id);
      if (!request) return;
      clearTimeout(request.timer);
      pending.delete(response.id);
      if (response.error) request.reject(new Error(response.error));
      else request.resolve(JSON.parse(response.result));
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
      nativeDownload: false,
      nativeShare: false,
      nativePush: false,
      floatingStatus: false,
      liveUpdate: false,
      tunnel: false,
      screenCapture: false
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
    getPendingNavigation: async () => '',
    getForwardedPorts: async () => '[]',
    isTunnelConnected: async () => false,
    testPortReachable: async () => false,
    getTunnelError: async () => 'HarmonyOS tunnel is not implemented',
    getTunnelErrorType: async () => 'unsupported',
    isScreenCaptureSupported: async () => false,
    isFloatingWindowEnabled: () => false,
    canPostPromotedNotifications: () => false,
    isChineseOem: () => false,
    getOemName: () => '',
    isOemAutoStartPrompted: () => true,
    openOemAutoStartSettings: () => false,
    openOemBatterySettings: () => false,
    reconnectTunnel: async () => false,
    reconnectTunnelAsync: async () => false,
    addForwardedPort: async () => ({ ok: false, reason: 'unreachable' }),
    addReverseForwardedPort: async () => false,
    setNativePushEnabled: enabled => {
      if (enabled) throw new Error('HarmonyOS native notifications are not implemented');
    },
    setFloatingWindowEnabled: enabled => {
      if (enabled) throw new Error('HarmonyOS floating status is not implemented');
    },
    setLiveUpdateEnabled: enabled => {
      if (enabled) throw new Error('Android Live Updates are unavailable on HarmonyOS');
    },
    updateLastSeenEventId: () => {},
    stopBackgroundService: () => {},
    setVolumeKeyMode: enabled => {
      if (enabled) throw new Error('HarmonyOS volume key mode is not implemented');
    },
    setTerminalSessionCount: () => {},
    setOemAutoStartPrompted: () => {},
    log: () => {}
  };
  for (const method of ['getServerList', 'getSavedServerConfig', 'getServerUrl',
    'saveServer', 'saveServerNamed', 'removeServer', 'connectToServer',
    'openExternalUrl', 'setKeepScreenOn', 'reloadApp']) {
    bridge[method] = (...args) => invoke(method, ...args);
  }
  for (const method of ['dismissSplash', 'rendererReady', 'showServerDialog']) {
    bridge[method] = (...args) => { void invoke(method, ...args).catch(() => {}); };
  }
  window.addEventListener('pagehide', () => {
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
