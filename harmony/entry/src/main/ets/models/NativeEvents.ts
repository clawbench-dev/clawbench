export interface NativeForgeEvent {
  owner?: string;
  repo?: string;
  item_type?: string;
  number?: number;
  run_id?: number;
  event_type?: string;
  project_path?: string;
}

export interface NativeForgeItem {
  type?: string;
  number?: number;
  title?: string;
}

export interface NativeForgeTarget {
  projectPath?: string;
  type: string;
  number: number;
  runId: number;
  itemKey: string;
}

export interface NativeEventData {
  status?: string;
  session_id?: string;
  task_id?: string;
  execution_id?: string;
  project_path?: string;
  response_preview_plain?: string;
  session_title?: string;
  tool_name?: string;
  title?: string;
  body?: string;
  token?: string;
  message_id?: number;
  event_type?: string;
  event?: NativeForgeEvent;
  item?: NativeForgeItem;
}

export interface NativeEvent {
  type: string;
  id?: string;
  event?: string;
  data?: NativeEventData;
  replayed?: boolean;
  suppress_notification?: boolean;
}

export interface PendingEvent {
  event_id: string;
  payload: string;
  suppress_notification?: boolean;
}

export interface NativeNavigation {
  sessionId?: string;
  taskId?: string;
  executionId?: string;
  projectPath?: string;
  messageId?: number;
  token?: string;
  usageMeter?: boolean;
  forge?: boolean;
  forgeTarget?: NativeForgeTarget;
}

export interface NotificationLaunch {
  origin: string;
  navigation: NativeNavigation;
}

export interface NotificationText {
  title: string;
  body: string;
}

export interface EventDecision {
  notify: boolean;
  dismiss: boolean;
  advance: boolean;
  duplicate: boolean;
}

export interface EventState {
  cursor: string;
  recentIds: string[];
  recentMessages?: string[];
  baselineInitialized?: boolean;
}

export function boundedEventText(value: string | undefined, max: number): string {
  return typeof value === 'string' ? value.replace(/[\u0000-\u001f\u007f]/g, ' ').slice(0, max) : '';
}

export function validEventId(value: string | undefined): boolean {
  return typeof value === 'string' && /^evt_[a-zA-Z0-9_-]{1,96}$/.test(value);
}

function compareDecimal(left: string, right: string): number {
  const a = left.replace(/^0+(?=\d)/, '');
  const b = right.replace(/^0+(?=\d)/, '');
  return a.length !== b.length ? a.length - b.length : a === b ? 0 : a > b ? 1 : -1;
}

export function newestEventId(current: string, candidate: string): string {
  if (!validEventId(candidate)) {
    return current;
  }
  if (!current) {
    return candidate;
  }
  const before = /^evt_([0-9]+)_([0-9]+)$/.exec(current);
  const after = /^evt_([0-9]+)_([0-9]+)$/.exec(candidate);
  if (before && after) {
    const instance = compareDecimal(after[1], before[1]);
    if (instance < 0 || (instance === 0 && compareDecimal(after[2], before[2]) <= 0)) {
      return current;
    }
  }
  return candidate;
}

export function advancesNativeCursor(event: NativeEvent): boolean {
  const kind = event.event;
  const status = event.data?.status;
  if (kind === 'session_update') {
    return status === 'completed' || status === 'cancelled' || status === 'permission_pending';
  }
  if (kind === 'task_update') {
    return status === 'completed' || status === 'failed' || status === 'cancelled';
  }
  return kind === 'usage_meter_update' || (kind === 'chat_stream' && event.data?.event_type === 'user_message');
}

export function nativeEventDecision(event: NativeEvent, state: EventState,
  foreground: boolean, freshBacklog: boolean = false): EventDecision {
  const kind = event.event;
  const status = event.data?.status;
  const message = completedMessageKey(event);
  const duplicate = (validEventId(event.id) && state.recentIds.includes(event.id || '')) ||
    (message.length > 0 && (state.recentMessages || []).includes(message));
  const approval = kind === 'session_update' && status === 'permission_pending';
  const notifiable = (kind === 'session_update' &&
    (status === 'completed' || status === 'cancelled' || approval)) ||
    (kind === 'task_update' && ['running', 'completed', 'failed', 'cancelled'].includes(status || '')) ||
    kind === 'usage_meter_update' || kind === 'live_event' ||
    (kind === 'forge_event' && ['opened', 'closed', 'merged', 'reopened', 'commented', 'pipeline_done']
      .includes(event.data?.event?.event_type || ''));
  return {
    notify: notifiable && !duplicate && !foreground && !freshBacklog &&
      (approval || (event.replayed !== true && event.suppress_notification !== true)),
    dismiss: (kind === 'session_update' || kind === 'task_update') &&
      (status === 'read' || status === 'permission_resolved' ||
        (event.suppress_notification === true && !approval)),
    advance: advancesNativeCursor(event) && validEventId(event.id),
    duplicate
  };
}

function completedMessageKey(event: NativeEvent): string {
  const data = event.data;
  if (event.event !== 'session_update' || data?.status !== 'completed' ||
    typeof data.session_id !== 'string' || !/^[a-zA-Z0-9_-]{1,160}$/.test(data.session_id) ||
    !Number.isSafeInteger(data.message_id) || (data.message_id || 0) <= 0) {
    return '';
  }
  return data.session_id + ':' + data.message_id;
}

export function recordNativeEvent(state: EventState, event: NativeEvent, fromPending: boolean = false): EventState {
  const id = event.id || '';
  const recent = state.recentIds.filter((value: string) => value !== id).slice(-511);
  if (validEventId(id)) {
    recent.push(id);
  }
  const message = completedMessageKey(event);
  const messages = (state.recentMessages || []).filter((value: string) => value !== message).slice(-511);
  if (message) {
    messages.push(message);
  }
  // A reply's persisted notification and WS-only completion have different IDs.
  // Only IDs observed in the pending endpoint are valid recovery cursors.
  return { cursor: fromPending ? newestEventId(state.cursor, id) : state.cursor,
    recentIds: recent, recentMessages: messages, baselineInitialized: state.baselineInitialized };
}

export function parseNativeEvent(json: string): NativeEvent | undefined {
  if (!json || json.length > 262144) {
    return undefined;
  }
  try {
    const event = JSON.parse(json) as NativeEvent;
    if (!event || typeof event !== 'object' || event.type !== 'event' ||
      typeof event.event !== 'string' || !event.data || typeof event.data !== 'object') {
      return undefined;
    }
    return event;
  } catch (_) {
    return undefined;
  }
}

export function navigationForEvent(event: NativeEvent): NativeNavigation | undefined {
  const data = event.data;
  if (!data) {
    return undefined;
  }
  const navigation: NativeNavigation = {};
  const project = boundedEventText(data.project_path, 4096);
  if (project) {
    navigation.projectPath = project;
  }
  if (event.event === 'task_update' && typeof data.task_id === 'string' && /^[0-9]{1,20}$/.test(data.task_id)) {
    navigation.taskId = data.task_id;
    if (typeof data.execution_id === 'string' && /^[0-9]{1,20}$/.test(data.execution_id)) {
      navigation.executionId = data.execution_id;
    }
  } else if (event.event === 'session_update' && typeof data.session_id === 'string' &&
    /^[a-zA-Z0-9_-]{1,160}$/.test(data.session_id)) {
    navigation.sessionId = data.session_id;
    if (Number.isSafeInteger(data.message_id) && (data.message_id || 0) > 0) {
      navigation.messageId = data.message_id;
    }
  } else if (event.event === 'live_event' && typeof data.token === 'string' &&
    /^[a-zA-Z0-9_-]{1,160}$/.test(data.token)) {
    navigation.token = data.token;
  } else if (event.event === 'forge_event') {
    const target = forgeTargetForEvent(data);
    if (!target) {
      return undefined;
    }
    navigation.forge = true;
    navigation.forgeTarget = target;
    if (target.projectPath) {
      navigation.projectPath = target.projectPath;
    }
  } else if (event.event === 'usage_meter_update') {
    navigation.usageMeter = true;
  } else {
    return undefined;
  }
  return navigation;
}

function forgeTargetForEvent(data: NativeEventData): NativeForgeTarget | undefined {
  const identity = data.event;
  const type = identity?.item_type || data.item?.type;
  if (type !== 'issue' && type !== 'pr' && type !== 'pipeline') {
    return undefined;
  }
  const number = type === 'pipeline' ? 0 : identity?.number || data.item?.number || 0;
  const runId = type === 'pipeline' ? identity?.run_id || 0 : 0;
  if (!Number.isSafeInteger(number) || !Number.isSafeInteger(runId) ||
    (type === 'pipeline' ? runId <= 0 : number <= 0)) {
    return undefined;
  }
  const target: NativeForgeTarget = {
    type, number, runId, itemKey: type === 'pipeline' ? 'pipeline/run:' + runId : type + '/' + number
  };
  const project = boundedEventText(identity?.project_path, 4096);
  if (project) {
    target.projectPath = project;
  }
  return target;
}

export function navigationEventName(navigation: NativeNavigation): string {
  if (navigation.taskId) {
    return 'clawbench-open-task';
  }
  if (navigation.sessionId) {
    return 'clawbench-open-session';
  }
  if (navigation.forge) {
    return 'clawbench-open-forge';
  }
  return navigation.token ? 'clawbench-open-meeting' : 'clawbench-open-usage-meter';
}

export function notificationSubject(navigation: NativeNavigation): string {
  if (navigation.taskId) {
    return 'task:' + navigation.taskId;
  }
  if (navigation.sessionId) {
    return 'session:' + navigation.sessionId;
  }
  if (navigation.forge && navigation.forgeTarget) {
    return 'forge:' + (navigation.projectPath || '') + ':' + navigation.forgeTarget.itemKey;
  }
  return navigation.token ? 'meeting:' + navigation.token : 'usage-meter';
}

export function notificationIdentity(origin: string, subject: string): number {
  let hash = 2166136261;
  const value = origin + '\n' + subject;
  for (let index = 0; index < value.length; index++) {
    hash = Math.imul(hash ^ value.charCodeAt(index), 16777619);
  }
  return (hash >>> 0) % 2147483646 + 1;
}

export function notificationText(event: NativeEvent, language: string): NotificationText {
  const data = event.data || {};
  const en = language === 'en';
  let title = '';
  let body = boundedEventText(data.response_preview_plain, 700);
  if (event.event === 'session_update') {
    if (data.status === 'permission_pending') {
      title = en ? 'Approval required' : '\u64cd\u4f5c\u9700\u6279\u51c6';
      body = boundedEventText(data.tool_name, 200);
    } else if (data.status === 'cancelled') {
      title = en ? 'Session cancelled' : '\u4f1a\u8bdd\u5df2\u53d6\u6d88';
    } else {
      title = en ? 'Session completed' : '\u4f1a\u8bdd\u5df2\u5b8c\u6210';
    }
  } else if (event.event === 'task_update') {
    if (data.status === 'running') {
      title = en ? 'Task started' : '\u4efb\u52a1\u5df2\u542f\u52a8';
      body = boundedEventText(data.session_title, 200);
    } else if (data.status === 'failed') {
      title = en ? 'Task failed' : '\u4efb\u52a1\u5931\u8d25';
    } else if (data.status === 'cancelled') {
      title = en ? 'Task cancelled' : '\u4efb\u52a1\u5df2\u53d6\u6d88';
    } else {
      title = en ? 'Task completed' : '\u4efb\u52a1\u5df2\u5b8c\u6210';
    }
  } else if (event.event === 'forge_event') {
    const target = forgeTargetForEvent(data);
    const kind = target?.type === 'pipeline' ? (en ? 'Pipeline' : '\u6d41\u6c34\u7ebf') :
      target?.type === 'pr' ? (en ? 'Pull request' : '\u5408\u5e76\u8bf7\u6c42') : (en ? 'Issue' : '\u8bae\u9898');
    const reference = target && target.type !== 'pipeline' ? ' #' + target.number : '';
    const owner = boundedEventText(data.event?.owner, 80);
    const repo = boundedEventText(data.event?.repo, 100);
    const slug = owner && repo ? owner + '/' + repo : owner || repo;
    const reason = forgeEventReason(data.event?.event_type || '', en);
    title = [slug, kind + reference, reason].filter((value: string) => value.length > 0).join(' \u00b7 ').slice(0, 200);
    body = boundedEventText(data.item?.title, 700) || reason;
  } else {
    title = boundedEventText(data.title, 200);
    body = boundedEventText(data.body, 700);
  }
  return { title: title || 'ClawBench', body: body || title || 'ClawBench' };
}

function forgeEventReason(kind: string, en: boolean): string {
  switch (kind) {
    case 'opened':
      return en ? 'Opened' : '\u5df2\u521b\u5efa';
    case 'closed':
      return en ? 'Closed' : '\u5df2\u5173\u95ed';
    case 'merged':
      return en ? 'Merged' : '\u5df2\u5408\u5e76';
    case 'reopened':
      return en ? 'Reopened' : '\u91cd\u65b0\u6253\u5f00';
    case 'commented':
      return en ? 'New comment' : '\u65b0\u8bc4\u8bba';
    case 'pipeline_done':
      return en ? 'Finished' : '\u5df2\u7ed3\u675f';
    default:
      return '';
  }
}

function sameForgeTarget(left: NativeForgeTarget | undefined, right: NativeForgeTarget | undefined): boolean {
  if (!left || !right) {
    return left === right;
  }
  return Object.keys(left).length === Object.keys(right).length && left.type === right.type &&
    left.number === right.number && left.runId === right.runId && left.itemKey === right.itemKey &&
    left.projectPath === right.projectPath;
}

export function isAllowedNotificationLaunch(launch: NotificationLaunch, origins: string[]): boolean {
  if (!launch || !origins.includes(launch.origin) || !launch.navigation) {
    return false;
  }
  const nav = launch.navigation;
  const kind = nav.taskId ? 'task_update' : nav.sessionId ? 'session_update' :
    nav.token ? 'live_event' : nav.forge === true ? 'forge_event' :
      nav.usageMeter === true ? 'usage_meter_update' : '';
  const checked = navigationForEvent({
    type: 'event', event: kind, data: {
      task_id: nav.taskId, execution_id: nav.executionId, session_id: nav.sessionId,
      project_path: nav.projectPath, message_id: nav.messageId, token: nav.token,
      event: nav.forgeTarget ? {
        item_type: nav.forgeTarget.type, number: nav.forgeTarget.number,
        run_id: nav.forgeTarget.runId, project_path: nav.forgeTarget.projectPath
      } : undefined
    }
  });
  return checked !== undefined && Object.keys(checked).length === Object.keys(nav).length &&
    checked.taskId === nav.taskId && checked.executionId === nav.executionId &&
    checked.sessionId === nav.sessionId && checked.projectPath === nav.projectPath &&
    checked.messageId === nav.messageId && checked.token === nav.token && checked.usageMeter === nav.usageMeter &&
    checked.forge === nav.forge && sameForgeTarget(checked.forgeTarget, nav.forgeTarget);
}
