import { common, Want, wantAgent } from '@kit.AbilityKit';
import { preferences } from '@kit.ArkData';
import { notificationManager } from '@kit.NotificationKit';
import { http, webSocket } from '@kit.NetworkKit';
import { webview } from '@kit.ArkWeb';
import { util } from '@kit.ArkTS';
import { parseServerAddress } from '../models/ServerAddress';
import { nativeCookieHeader } from '../models/NativeCookies';
import { NativeFailure, nativeMessage } from '../models/NativeMessages';
import {
  EventState, NativeEvent, NativeNavigation, NotificationLaunch, PendingEvent,
  isAllowedNotificationLaunch, nativeEventDecision, navigationForEvent, newestEventId,
  notificationIdentity, notificationSubject, notificationText, parseNativeEvent, recordNativeEvent,
  validEventId
} from '../models/NativeEvents';

interface SavedEvents {
  origin: string;
  enabled: boolean;
  state: EventState;
}

interface SavedLaunch {
  token: string;
  expiresAt: number;
  launch: NotificationLaunch;
}

interface PendingResponse {
  events: PendingEvent[];
}

interface NativePushConfig {
  push_mode?: string;
}

interface NativeNotificationStatus {
  enabled: boolean;
  connected: boolean;
  permission: string;
  error: string;
  backgroundDelivery: string;
}

export class NativeEventService {
  private context?: common.UIAbilityContext;
  private prefs?: preferences.Preferences;
  private records: SavedEvents[] = [];
  private launches: SavedLaunch[] = [];
  private active?: SavedEvents;
  private language: string = 'zh';
  private foreground: boolean = true;
  private generation: number = 0;
  private settingEpoch: number = 0;
  private permissionEpoch: number = 0;
  private permission: string = 'unknown';
  private permissionRequest?: Promise<boolean>;
  private startupPermissionRequested: boolean = false;
  private socket?: webSocket.WebSocket;
  private requests: http.HttpRequest[] = [];
  private retryTimer: number = -1;
  private recoveryTimer: number = -1;
  private recovery?: Promise<void>;
  private attempt: number = 0;
  private queue: Promise<void> = Promise.resolve();
  private writes: Promise<void> = Promise.resolve();
  private pending?: NotificationLaunch;
  private connected: boolean = false;
  private lastError: string = '';
  private clientId: string = '';
  private emit: (name: string, detail: string) => void;

  constructor(emit: (name: string, detail: string) => void = () => {}) {
    this.emit = emit;
  }

  static receiveWant(want: Want): void {
    const token = want.parameters?.clawbenchNotificationToken;
    if (typeof token === 'string' && /^[a-f0-9-]{32,36}$/i.test(token)) {
      AppStorage.setOrCreate('clawbenchNotificationToken', token);
    }
  }

  async init(context: common.UIAbilityContext): Promise<void> {
    this.context = context;
    this.prefs = await preferences.getPreferences(context, { name: 'clawbench_native_events' });
    try {
      const stored = JSON.parse(String(await this.prefs.get('servers', '[]'))) as SavedEvents[];
      this.records = stored.filter((record: SavedEvents) => {
        try {
          return parseServerAddress(record.origin).url === record.origin && typeof record.enabled === 'boolean' &&
            record.state && typeof record.state.cursor === 'string' && Array.isArray(record.state.recentIds);
        } catch (_) {
          return false;
        }
      }).slice(-100);
      this.records.forEach((record: SavedEvents) => {
        record.state.recentIds = record.state.recentIds.filter((id: string) => validEventId(id)).slice(-512);
        record.state.recentMessages = Array.isArray(record.state.recentMessages) ?
          record.state.recentMessages.filter((key: string) =>
            typeof key === 'string' && /^[a-zA-Z0-9_-]{1,160}:[0-9]{1,16}$/.test(key)).slice(-512) : [];
        if (!validEventId(record.state.cursor)) {
          record.state.cursor = '';
        }
        record.state.baselineInitialized = record.state.baselineInitialized === true || record.state.cursor.length > 0;
      });
      const launches = JSON.parse(String(await this.prefs.get('launches', '[]'))) as SavedLaunch[];
      this.launches = launches.filter((launch: SavedLaunch) => launch && launch.expiresAt > Date.now() &&
        typeof launch.token === 'string').slice(-100);
    } catch (_) {
      this.records = [];
      this.launches = [];
    }
    this.clientId = String(await this.prefs.get('clientId', ''));
    if (!/^harmony-[a-f0-9-]{32,36}$/i.test(this.clientId)) {
      this.clientId = 'harmony-' + util.generateRandomUUID(true);
      await this.prefs.put('clientId', this.clientId);
      await this.prefs.flush();
    }
  }

  async bind(origin: string, language: string, foreground: boolean): Promise<void> {
    this.stop();
    const normalized = parseServerAddress(origin).url;
    this.active = this.records.find((record: SavedEvents) => record.origin === normalized);
    if (!this.active) {
      this.active = { origin: normalized, enabled: false, state: { cursor: '', recentIds: [] } };
      this.records.push(this.active);
    }
    this.setLanguage(language);
    this.foreground = foreground;
    this.lastError = '';
    this.reportStatus();
    await this.syncServerSettings();
  }

  async syncServerSettings(): Promise<void> {
    const record = this.active;
    if (!record || !this.context) {
      return;
    }
    const generation = this.generation;
    const settingEpoch = ++this.settingEpoch;
    const current = (): boolean => generation === this.generation && settingEpoch === this.settingEpoch &&
      record === this.active;
    let request: http.HttpRequest | undefined;
    try {
      request = http.createHttp();
      this.requests.push(request);
      const url = record.origin + '/api/config';
      const cookie = nativeCookieHeader(record.origin, webview.WebCookieManager.fetchCookieSync(url));
      const response = await request.request(url, {
        method: http.RequestMethod.GET, header: { Cookie: cookie }, maxRedirects: 0,
        connectTimeout: 5000, readTimeout: 5000, maxLimit: 262144,
        usingCache: false, expectDataType: http.HttpDataType.STRING
      });
      if (!current()) {
        return;
      }
      if (response.responseCode !== 200) {
        throw new Error('Notification settings request failed');
      }
      const config = JSON.parse(String(response.result)) as NativePushConfig;
      if (!config || typeof config !== 'object' || Array.isArray(config) ||
        (config.push_mode !== undefined && typeof config.push_mode !== 'string')) {
        throw new Error('Invalid notification settings');
      }
      const mode = config.push_mode || 'native';
      if (!['native', 'disabled', 'dingtalk', 'feishu', 'wechat'].includes(mode)) {
        throw new Error('Invalid notification mode');
      }
      record.enabled = mode === 'native';
      this.lastError = '';
      await this.persist();
    } catch (_) {
      if (current()) {
        this.lastError = 'notification_settings_failed';
      }
    } finally {
      this.requests = this.requests.filter((item: http.HttpRequest) => item !== request);
      request?.destroy();
    }
    if (!current()) {
      return;
    }
    await this.refreshStatus();
    if (!current()) {
      return;
    }
    if (!record.enabled || this.permission !== 'granted') {
      this.stopTransport();
      if (!record.enabled) {
        await notificationManager.cancelGroup(this.group(record.origin)).catch(() => {});
      }
    } else {
      await this.startDelivery();
      await this.enqueueRecovery();
    }
    this.reportStatus();
  }

  async requestStartupPermission(): Promise<void> {
    if (!this.context || !this.foreground || this.startupPermissionRequested) {
      return;
    }
    this.startupPermissionRequested = true;
    try {
      await this.requestPermission();
    } catch (_) {
      // Permission service failures must not block the local login page.
    }
    await this.refreshStatus();
  }

  private requestPermission(): Promise<boolean> {
    if (!this.permissionRequest) {
      this.permissionRequest = this.enablePermission().finally(() => {
        this.permissionRequest = undefined;
      });
    }
    return this.permissionRequest;
  }

  private async enablePermission(): Promise<boolean> {
    const context = this.context;
    if (!context) {
      return false;
    }
    if (await notificationManager.isNotificationEnabled()) {
      return true;
    }
    if (context !== this.context) {
      return false;
    }
    await notificationManager.requestEnableNotification(context).catch(() => {});
    return notificationManager.isNotificationEnabled();
  }

  async setEnabled(enabled: boolean): Promise<void> {
    if (!this.context || !this.active) {
      throw new NativeFailure('notification_not_connected', this.language);
    }
    const record = this.active;
    const generation = this.generation;
    const settingEpoch = ++this.settingEpoch;
    try {
      let allowed = await notificationManager.isNotificationEnabled();
      if (generation !== this.generation || settingEpoch !== this.settingEpoch) {
        return;
      }
      if (enabled && !allowed) {
        allowed = await this.requestPermission();
      }
      if (generation !== this.generation || settingEpoch !== this.settingEpoch) {
        return;
      }
      this.permissionEpoch++;
      this.permission = allowed ? 'granted' : 'denied';
      // Retain the requested mode when the OS blocks it. A later grant can
      // resume delivery without changing the server's push_mode.
      record.enabled = enabled;
      this.lastError = enabled && !allowed ? 'notification_permission_denied' : '';
      await this.persist();
      if (generation !== this.generation || settingEpoch !== this.settingEpoch) {
        return;
      }
      if (!enabled || !allowed) {
        this.stopTransport();
        await notificationManager.cancelGroup(this.group(record.origin)).catch(() => {});
      } else {
        await this.startDelivery();
      }
      this.reportStatus();
    } catch (_) {
      if (generation === this.generation && settingEpoch === this.settingEpoch) {
        this.permissionEpoch++;
        this.permission = 'unknown';
        this.lastError = 'notification_status_failed';
        this.stopTransport();
        this.reportStatus();
        throw new NativeFailure(this.lastError, this.language);
      }
      return;
    }
    if (generation === this.generation && settingEpoch === this.settingEpoch &&
      enabled && this.permission === 'denied') {
      throw new NativeFailure('notification_permission_denied', this.language);
    }
  }

  getStatus(): string {
    const status: NativeNotificationStatus = {
      enabled: this.active?.enabled === true && this.permission === 'granted', connected: this.connected,
      permission: this.permission, error: this.lastError ? nativeMessage(this.lastError, this.language) : '',
      backgroundDelivery: 'while-process-running'
    };
    return JSON.stringify(status);
  }

  async refreshStatus(): Promise<string> {
    const record = this.active;
    if (!this.context) {
      return this.getStatus();
    }
    const generation = this.generation;
    const settingEpoch = this.settingEpoch;
    const permissionEpoch = ++this.permissionEpoch;
    try {
      const allowed = await notificationManager.isNotificationEnabled();
      if (generation !== this.generation || settingEpoch !== this.settingEpoch ||
        permissionEpoch !== this.permissionEpoch || record !== this.active) {
        return this.getStatus();
      }
      const previous = this.permission;
      this.permission = allowed ? 'granted' : 'denied';
      if (!allowed) {
        this.stopTransport();
        this.lastError = record?.enabled ? 'notification_permission_denied' : '';
      } else {
        if (this.lastError === 'notification_permission_denied' ||
          this.lastError === 'notification_status_failed') {
          this.lastError = '';
        }
        if (record?.enabled && previous !== 'granted') {
          await this.startDelivery();
        }
      }
      this.reportStatus();
    } catch (_) {
      if (generation === this.generation && settingEpoch === this.settingEpoch &&
        permissionEpoch === this.permissionEpoch) {
        this.permission = 'unknown';
        this.lastError = 'notification_status_failed';
        this.stopTransport();
        this.reportStatus();
      }
    }
    return this.getStatus();
  }

  setForeground(foreground: boolean): void {
    if (this.foreground === foreground) {
      return;
    }
    this.foreground = foreground;
    this.scheduleRecovery();
    this.reportStatus();
    void this.syncServerSettings();
  }

  setLanguage(language: string): void {
    this.language = language === 'en' ? 'en' : 'zh';
    this.reportStatus();
  }

  async updateLastSeenEventId(id: string): Promise<void> {
    if (!this.active || !this.foreground || !validEventId(id)) {
      return;
    }
    this.active.state.recentIds = this.active.state.recentIds.filter((value: string) => value !== id).slice(-511);
    this.active.state.recentIds.push(id);
    await this.persist();
  }

  async dismiss(taskId: string, sessionId: string): Promise<void> {
    if (!this.active) {
      return;
    }
    const subject = taskId && /^[0-9]{1,20}$/.test(taskId) ? 'task:' + taskId :
      /^[a-zA-Z0-9_-]{1,160}$/.test(sessionId) ? 'session:' + sessionId : '';
    if (subject) {
      await this.dismissAt(this.active.origin, subject);
    }
  }

  async takeLaunch(allowedOrigins: string[]): Promise<NotificationLaunch | undefined> {
    const token = AppStorage.get<string>('clawbenchNotificationToken') || '';
    AppStorage.setOrCreate('clawbenchNotificationToken', '');
    if (!token) {
      return undefined;
    }
    const saved = this.launches.find((entry: SavedLaunch) => entry.token === token && entry.expiresAt > Date.now());
    this.launches = this.launches.filter((entry: SavedLaunch) => entry.token !== token && entry.expiresAt > Date.now());
    await this.persist();
    if (!saved || !isAllowedNotificationLaunch(saved.launch, allowedOrigins)) {
      return undefined;
    }
    return saved.launch;
  }

  queueNavigation(launch: NotificationLaunch): void {
    this.pending = launch;
  }

  takePendingNavigation(): string {
    if (!this.pending || !this.active || this.pending.origin !== this.active.origin) {
      return '';
    }
    const result = JSON.stringify(this.pending.navigation);
    this.pending = undefined;
    return result;
  }

  stop(): void {
    this.generation++;
    this.settingEpoch++;
    this.permissionEpoch++;
    this.permission = 'unknown';
    this.stopTransport();
    this.requests.forEach((request: http.HttpRequest) => request.destroy());
    this.requests = [];
    this.active = undefined;
    this.queue = Promise.resolve();
    this.recovery = undefined;
  }

  destroy(): void {
    this.stop();
    this.context = undefined;
    this.pending = undefined;
  }

  private reportStatus(): void {
    this.emit('clawbench-native-notification-status', this.getStatus());
  }

  private group(origin: string): string {
    return 'clawbench-' + notificationIdentity(origin, 'server');
  }

  private persist(): Promise<void> {
    const records = JSON.stringify(this.records);
    const launches = JSON.stringify(this.launches.filter((entry: SavedLaunch) => entry.expiresAt > Date.now()).slice(-100));
    this.writes = this.writes.catch(() => {}).then(async () => {
      if (!this.prefs) {
        return;
      }
      await this.prefs.put('servers', records);
      await this.prefs.put('launches', launches);
      await this.prefs.flush();
    });
    return this.writes;
  }

  private stopTransport(): void {
    clearTimeout(this.retryTimer);
    this.retryTimer = -1;
    clearTimeout(this.recoveryTimer);
    this.recoveryTimer = -1;
    const socket = this.socket;
    this.socket = undefined;
    this.connected = false;
    if (socket) {
      socket.off('open');
      socket.off('message');
      socket.off('close');
      socket.off('error');
      socket.close().catch(() => {});
    }
  }

  private async startDelivery(): Promise<void> {
    const record = this.active;
    const generation = this.generation;
    if (!record?.enabled || this.permission !== 'granted') {
      return;
    }
    if (record.state.baselineInitialized !== true && !record.state.cursor) {
      await this.enqueueRecovery();
    }
    if (generation !== this.generation || record !== this.active) {
      return;
    }
    this.connect();
    this.scheduleRecovery();
  }

  private scheduleRecovery(): void {
    clearTimeout(this.recoveryTimer);
    this.recoveryTimer = -1;
    if (!this.active?.enabled || this.permission !== 'granted' || this.foreground) {
      return;
    }
    const generation = this.generation;
    this.recoveryTimer = setTimeout(() => {
      clearTimeout(this.recoveryTimer);
      this.recoveryTimer = -1;
      if (generation !== this.generation) {
        return;
      }
      void this.enqueueRecovery().then(() => {
        if (generation === this.generation) {
          this.scheduleRecovery();
        }
      });
    }, 15000);
  }

  private connect(): void {
    const record = this.active;
    if (!record?.enabled || this.permission !== 'granted' || this.socket) {
      return;
    }
    clearTimeout(this.retryTimer);
    const generation = this.generation;
    const socket = webSocket.createWebSocket();
    this.socket = socket;
    socket.on('open', () => {
      if (generation !== this.generation || this.socket !== socket) {
        return;
      }
      this.connected = true;
      this.lastError = '';
      this.attempt = 0;
      this.reportStatus();
      this.enqueueRecovery();
    });
    socket.on('message', (_, data: string | ArrayBuffer) => {
      if (generation !== this.generation || this.socket !== socket || typeof data !== 'string' || data.length > 262144) {
        return;
      }
      try {
        const message = JSON.parse(data) as NativeEvent;
        if (message?.type === 'ping') {
          socket.send('{"type":"pong"}').catch(() => {});
          return;
        }
      } catch (_) {
        return;
      }
      const event = parseNativeEvent(data);
      if (event) {
        this.queue = this.queue.catch(() => {}).then(() => this.process(record, event, generation, false))
          .catch(() => this.reportFailure(generation));
      }
    });
    socket.on('close', () => this.reconnect(socket, generation));
    socket.on('error', () => this.reconnect(socket, generation));
    try {
      const cookie = nativeCookieHeader(record.origin,
        webview.WebCookieManager.fetchCookieSync(record.origin + '/api/ai/events/ws'));
      if (!cookie) {
        throw new Error('Authentication is unavailable');
      }
      const url = record.origin.replace(/^http/, 'ws') + '/api/ai/events/ws?client_id=' + encodeURIComponent(this.clientId);
      socket.connect(url, { header: { Cookie: cookie, Origin: record.origin }, pingInterval: 30, pongTimeout: 15 })
        .catch(() => this.reconnect(socket, generation));
    } catch (_) {
      this.reconnect(socket, generation);
    }
  }

  private reconnect(socket: webSocket.WebSocket, generation: number): void {
    if (generation !== this.generation || this.socket !== socket) {
      return;
    }
    this.stopTransport();
    this.reportFailure(generation);
    if (this.active?.enabled && this.permission === 'granted') {
      this.scheduleRecovery();
      const delay = Math.min(30000, 1000 * Math.pow(2, Math.min(this.attempt++, 5)));
      this.retryTimer = setTimeout(() => {
        if (generation === this.generation) {
          this.connect();
        }
      }, delay);
    }
  }

  private reportFailure(generation: number): void {
    if (generation === this.generation) {
      this.lastError = 'notification_connection_failed';
      this.reportStatus();
    }
  }

  private enqueueRecovery(): Promise<void> {
    const record = this.active;
    const generation = this.generation;
    if (!record?.enabled || this.permission !== 'granted') {
      return Promise.resolve();
    }
    if (this.recovery) {
      return this.recovery;
    }
    const recovery: Promise<void> = this.queue.catch(() => {}).then(() => this.recover(record, generation))
      .catch(() => this.reportFailure(generation)).finally(() => {
        if (this.recovery === recovery) {
          this.recovery = undefined;
        }
      });
    this.recovery = recovery;
    this.queue = recovery;
    return recovery;
  }

  private async fetchPending(origin: string, cursor: string, generation: number): Promise<PendingEvent[]> {
    const request = http.createHttp();
    this.requests.push(request);
    try {
      const url = origin + '/api/ai/events/pending' + (cursor ? '?after=' + encodeURIComponent(cursor) : '');
      const cookie = nativeCookieHeader(origin, webview.WebCookieManager.fetchCookieSync(url));
      if (generation !== this.generation) {
        return [];
      }
      if (!cookie) {
        throw new Error('Authentication is unavailable');
      }
      const response = await request.request(url, {
        method: http.RequestMethod.GET, header: { Cookie: cookie }, maxRedirects: 0,
        connectTimeout: 10000, readTimeout: 15000, maxLimit: 2097152,
        usingCache: false, expectDataType: http.HttpDataType.STRING
      });
      if (generation !== this.generation) {
        return [];
      }
      if (response.responseCode !== 200) {
        throw new Error('Pending event request failed');
      }
      const payload = JSON.parse(String(response.result)) as PendingResponse;
      if (!payload || !Array.isArray(payload.events)) {
        throw new Error('Invalid pending event response');
      }
      return payload.events.slice(0, 1000);
    } finally {
      this.requests = this.requests.filter((item: http.HttpRequest) => item !== request);
      request.destroy();
    }
  }

  private async recover(record: SavedEvents, generation: number): Promise<void> {
    if (generation !== this.generation || !record.enabled || this.permission !== 'granted') {
      return;
    }
    const cursor = record.state.cursor;
    let events = await this.fetchPending(record.origin, cursor, generation);
    const fresh = record.state.baselineInitialized !== true && cursor.length === 0;
    if (events.length === 0 && cursor) {
      // Missing/expired cursors also return []. Preserve unread newer rows
      // during recovery, including cursors written by older WS-only clients.
      events = await this.fetchPending(record.origin, '', generation);
      if (events.some((item: PendingEvent) => item.event_id === cursor)) {
        events = events.slice(events.findIndex((item: PendingEvent) => item.event_id === cursor) + 1);
      } else {
        events = events.filter((item: PendingEvent) => item && item.event_id !== cursor &&
          newestEventId(cursor, item.event_id) === item.event_id);
      }
    }
    for (const pending of events) {
      if (generation !== this.generation || !record.enabled || this.permission !== 'granted') {
        return;
      }
      if (!pending || !validEventId(pending.event_id) || typeof pending.payload !== 'string') {
        continue;
      }
      const event = parseNativeEvent(pending.payload);
      if (!event) {
        continue;
      }
      event.id = pending.event_id;
      event.suppress_notification = pending.suppress_notification === true;
      await this.process(record, event, generation, fresh, false, true);
    }
    if (generation === this.generation && record.enabled && this.permission === 'granted') {
      record.state.baselineInitialized = true;
      await this.persist();
    }
  }

  private async process(record: SavedEvents, event: NativeEvent, generation: number,
    fresh: boolean, persist: boolean = true, fromPending: boolean = false): Promise<void> {
    if (generation !== this.generation || !record.enabled || this.permission !== 'granted') {
      return;
    }
    const observedInForeground = this.foreground;
    const decision = nativeEventDecision(event, record.state, observedInForeground, fresh);
    if (!fromPending && !decision.advance && !decision.notify && !decision.dismiss) {
      return;
    }
    const navigation = navigationForEvent(event);
    if (decision.dismiss && navigation) {
      await this.dismissAt(record.origin, notificationSubject(navigation));
    }
    if (decision.notify && navigation) {
      await this.publish(record, event, navigation, generation);
    }
    if (generation !== this.generation || !record.enabled || this.permission !== 'granted') {
      return;
    }
    // A background WS replay is not evidence that its pending reply was seen.
    // Leave both ID and message dedup available after a failed HTTP recovery.
    if (!fromPending && event.replayed === true && !observedInForeground && !decision.notify) {
      return;
    }
    record.state = recordNativeEvent(record.state, event, fromPending);
    if (persist) {
      await this.persist();
    }
  }

  private async dismissAt(origin: string, subject: string): Promise<void> {
    await notificationManager.cancel(notificationIdentity(origin, subject), this.group(origin)).catch(() => {});
  }

  private async publish(record: SavedEvents, event: NativeEvent, navigation: NativeNavigation,
    generation: number): Promise<void> {
    if (!this.context || generation !== this.generation || this.foreground) {
      return;
    }
    const allowed = await notificationManager.isNotificationEnabled();
    if (generation !== this.generation) {
      return;
    }
    if (!allowed) {
      this.permissionEpoch++;
      this.permission = 'denied';
      this.lastError = 'notification_permission_denied';
      this.stopTransport();
      this.reportStatus();
      return;
    }
    if (this.foreground || !record.enabled) {
      return;
    }
    const subject = notificationSubject(navigation);
    const id = notificationIdentity(record.origin, subject);
    const token = util.generateRandomUUID(true);
    const launch: NotificationLaunch = { origin: record.origin, navigation };
    this.launches = this.launches.filter((entry: SavedLaunch) => entry.expiresAt > Date.now() &&
      !(entry.launch.origin === record.origin && notificationSubject(entry.launch.navigation) === subject)).slice(-99);
    this.launches.push({ token, expiresAt: Date.now() + 7 * 24 * 3600000, launch });
    await this.persist();
    const agent = await wantAgent.getWantAgent({
      wants: [{
        bundleName: this.context.abilityInfo.bundleName,
        abilityName: this.context.abilityInfo.name,
        parameters: { clawbenchNotificationToken: token }
      }],
      actionType: wantAgent.OperationType.START_ABILITY,
      requestCode: id,
      actionFlags: [wantAgent.WantAgentFlags.UPDATE_PRESENT_FLAG, wantAgent.WantAgentFlags.CONSTANT_FLAG]
    });
    if (generation !== this.generation || this.foreground || !record.enabled) {
      return;
    }
    const text = notificationText(event, this.language);
    await notificationManager.publish({
      id, label: this.group(record.origin), groupName: this.group(record.origin), wantAgent: agent,
      tapDismissed: true, autoDeletedTime: Date.now() + 24 * 3600000,
      notificationSlotType: notificationManager.SlotType.SERVICE_INFORMATION,
      content: {
        notificationContentType: notificationManager.ContentType.NOTIFICATION_CONTENT_BASIC_TEXT,
        normal: { title: text.title, text: text.body }
      }
    });
  }
}
