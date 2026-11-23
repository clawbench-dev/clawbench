import i18n from '@/i18n'

/**
 * Localized display names for every known tool call.
 *
 * The wire name (Read / Bash / TaskOutput …) is an implementation detail of the
 * agent backends; the chat shows what the tool DOES instead. Keyed by the
 * lowercased canonical tool name so the lookup matches the icon table's
 * case-insensitive behavior.
 *
 * Unknown names (custom ACP agents, MCP servers) keep their raw name — inventing
 * a label for a tool we know nothing about would be worse than showing it.
 *
 * This module deliberately imports only the i18n singleton: it is consumed by
 * the HTML-string renderers and by notification text, neither of which should
 * pull the whole lucide icon table in through utils/icons.ts.
 */

/**
 * Readable card titles for the task-management tool family.
 *
 * Their inputs carry no descriptive text — TaskOutput/TaskStop take a bare
 * `task_id` (e.g. "cyqjeL") and TaskUpdate takes `{taskId, status}` — so the
 * generic summary chain degrades to an opaque identifier and the card strip
 * reads "TaskOutput  cyqjeL", which says nothing about what happened. The pill
 * title states the ACTION instead, leaving the id as the secondary summary
 * (see toolCallSummary, which already derives it from the input).
 *
 * Exported so the key-parity test can iterate THIS table instead of a
 * hand-copied list — a hand-copied list silently stops guarding the moment a
 * seventh tool is added here, which is the same blind spot that made the
 * literal-key scan miss this map in the first place (it only sees a quoted key
 * written directly at a translate call site).
 */
export const TASK_TOOL_TITLE_KEYS: Readonly<Record<string, string>> = Object.freeze({
  backgroundtask: 'tool.task.action.background',
  taskcreate: 'tool.task.action.create',
  taskupdate: 'tool.task.action.update',
  tasklist: 'tool.task.action.list',
  taskget: 'tool.task.action.get',
  taskstop: 'tool.task.action.stop',
  taskoutput: 'tool.task.action.output',
})

/**
 * Lowercased canonical tool name → i18n key.
 *
 * Every entry of TOOL_ICONS (utils/icons.ts) must appear here — the icon table
 * is what "known tool" means in this codebase, and a known tool with an icon
 * but no name would silently fall back to the raw wire name. The parity test
 * iterates that table so a newly added tool cannot be half-registered.
 *
 * `wait` is the one known tool without an icon entry: Codex's collaboration
 * wait has a registered renderer (renderToolDetail) but no icon of its own, so
 * it is named here explicitly.
 */
export const TOOL_NAME_KEYS: Readonly<Record<string, string>> = Object.freeze({
  read: 'tool.name.read',
  write: 'tool.name.write',
  edit: 'tool.name.edit',
  bash: 'tool.name.bash',
  grep: 'tool.name.grep',
  glob: 'tool.name.glob',
  websearch: 'tool.name.websearch',
  webfetch: 'tool.name.webfetch',
  agent: 'tool.name.agent',
  task: 'tool.name.task',
  skill: 'tool.name.skill',
  askuserquestion: 'tool.name.askUserQuestion',
  enterplanmode: 'tool.name.enterPlanMode',
  exitplanmode: 'tool.name.exitPlanMode',
  ls: 'tool.name.ls',
  powershell: 'tool.name.powerShell',
  sendmessage: 'tool.name.sendMessage',
  notebookedit: 'tool.name.notebookEdit',
  todowrite: 'tool.name.todoWrite',
  todoread: 'tool.name.todoRead',
  lsp: 'tool.name.lsp',
  imagegen: 'tool.name.imageGen',
  viewimage: 'tool.name.viewImage',
  enterworktree: 'tool.name.enterWorktree',
  leaveworktree: 'tool.name.leaveWorktree',
  computeruse: 'tool.name.computerUse',
  teamcreate: 'tool.name.teamCreate',
  teamdelete: 'tool.name.teamDelete',
  wechatreply: 'tool.name.weChatReply',
  wecomreply: 'tool.name.weComReply',
  save_memory: 'tool.name.saveMemory',
  deepthink: 'tool.name.deepThink',
  structuredoutput: 'tool.name.structuredOutput',
  skillmanage: 'tool.name.skillManage',
  monitor: 'tool.name.monitor',
  permissionapproval: 'tool.name.permissionApproval',
  multiedit: 'tool.name.multiEdit',
  git: 'tool.name.git',
  wait: 'tool.name.wait',
  ...TASK_TOOL_TITLE_KEYS,
})

/**
 * Return the localized name of a known tool, or the raw name when it is not in
 * the table.
 *
 * `Object.hasOwn` rather than a truthiness check: the name comes from the agent
 * (custom ACP / MCP servers may report anything), and a plain object lookup
 * would resolve inherited keys — "constructor" would return the Object
 * constructor's source text, "__proto__" an object, both rendered into the pill
 * instead of the tool's own name.
 */
export function localizedToolName(name?: string): string {
  const safeName = name || ''
  if (!safeName) return ''
  const lower = safeName.toLowerCase()
  if (Object.hasOwn(TOOL_NAME_KEYS, lower)) return i18n.global.t(TOOL_NAME_KEYS[lower])
  return safeName
}
