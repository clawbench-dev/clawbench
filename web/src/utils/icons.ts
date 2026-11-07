import {
  // Tool icons
  Eye, PenLine, FilePenLine, SquareTerminal, Terminal, Search, Folder,
  Globe, Bot, Sparkles, MessageSquarePlus, Plus, Pencil,
  CircleDot, ListChecks, ListTodo, Target,
  FileText, Compass, CheckCircle2, FolderSync, Monitor,
  Users, MessageSquare, Send, Save, Camera, Wrench,
  ShieldAlert, GitBranch,
  // `Image` shadows the DOM constructor inside this module — alias it.
  Image as ImageIcon,
  // Fallback
  Wrench as WrenchFallback,
  // Thinking
  Brain,
} from 'lucide-vue-next'

/**
 * Tool icon mapping: tool name -> { icon: LucideComponent, category: string }
 * Category values drive the CSS color system via [data-category] selectors in ContentBlocks.vue
 */
export const TOOL_ICONS: Record<string, { icon: typeof Wrench; category: string }> = {
  'Read':              { icon: Eye,              category: 'file' },
  'Write':             { icon: PenLine,          category: 'file' },
  'Edit':              { icon: FilePenLine,      category: 'file' },
  'Bash':              { icon: SquareTerminal,   category: 'bash' },
  'Grep':              { icon: Search,           category: 'search' },
  'Glob':              { icon: Folder,           category: 'search' },
  'WebSearch':         { icon: Globe,            category: 'search' },
  'WebFetch':          { icon: Globe,            category: 'search' },
  'Agent':             { icon: Bot,              category: 'agent' },
  'Task':              { icon: Bot,              category: 'agent' },  // ACP generic Task = sub-agent delegation
  'Skill':             { icon: Sparkles,         category: 'skill' },
  'AskUserQuestion':   { icon: MessageSquarePlus, category: 'ask' },
  'TaskCreate':        { icon: Plus,             category: 'task' },
  'TaskUpdate':        { icon: Pencil,           category: 'task' },
  'TaskList':          { icon: ListChecks,       category: 'task' },
  'TaskGet':           { icon: CircleDot,        category: 'task' },
  'TaskStop':          { icon: Target,           category: 'task' },
  'TaskOutput':        { icon: FileText,         category: 'task' },
  'EnterPlanMode':     { icon: Compass,          category: 'plan' },
  'ExitPlanMode':      { icon: CheckCircle2,     category: 'plan' },
  'LS':                { icon: Folder,           category: 'file' },
  'PowerShell':        { icon: Terminal,         category: 'bash' },
  'SendMessage':       { icon: Send,             category: 'agent' },
  'NotebookEdit':      { icon: FilePenLine,      category: 'file' },
  'TodoWrite':         { icon: ListTodo,         category: 'task' },
  'LSP':               { icon: Sparkles,         category: 'skill' },
  'ImageGen':          { icon: Camera,           category: 'skill' },
  'ViewImage':         { icon: ImageIcon,        category: 'file' },
  'EnterWorktree':     { icon: FolderSync,       category: 'plan' },
  'LeaveWorktree':     { icon: FolderSync,       category: 'plan' },
  'ComputerUse':       { icon: Monitor,          category: 'agent' },
  'TeamCreate':        { icon: Users,            category: 'agent' },
  'TeamDelete':        { icon: Users,            category: 'agent' },
  'WeChatReply':       { icon: MessageSquare,    category: 'agent' },
  'WeComReply':        { icon: MessageSquare,    category: 'agent' },
  'save_memory':       { icon: Save,             category: 'skill' },
  'DeepThink':        { icon: Brain,           category: 'skill' },
  'StructuredOutput':  { icon: FileText,         category: 'file' },
  'SkillManage':       { icon: Sparkles,         category: 'skill' },
  'Monitor':           { icon: Monitor,          category: 'bash' },
  'PermissionApproval':{ icon: ShieldAlert,      category: 'permission' },
  'MultiEdit':         { icon: FilePenLine,      category: 'file' },
  'TodoRead':          { icon: ListChecks,       category: 'task' },
  'Git':               { icon: GitBranch,        category: 'bash' },
}

export const FALLBACK_TOOL_ICON = { icon: WrenchFallback, category: 'fallback' }

/**
 * Known Agent sub-type names that should use the Agent icon/category.
 * ACP ToolCallUpdate may set block.name to the sub-type title (e.g. "Explore")
 * instead of the canonical "Agent". Without this mapping, the frontend falls
 * back to the wrench icon. These names always render with Bot icon + agent category.
 */
const AGENT_SUBTYPE_NAMES = new Set([
  'explore', 'plan', 'general-purpose', 'general', 'claude',
  'code-reviewer', 'statusline-setup', 'fork',
  // OpenCode 2.x's delegation tool; older stored blocks still carry this name
  // verbatim, so the icon must resolve even without the backend remap.
  'subagent', 'sub agent', 'sub-agent',
])

/** Tool names that are the Agent delegation card (all backends' spellings). */
const AGENT_FAMILY_NAMES = new Set(['agent', 'task', 'subagent', 'sub agent', 'sub-agent'])

/**
 * Lowercased tool name → entry, built once at module load.
 *
 * `getToolIcon` is called from the chat template several times per tool block
 * per render pass. The previous implementation ran `Object.entries(TOOL_ICONS)
 * .find(...)` on every call: it allocated a 42-element array and lowercased
 * both sides for each entry. On a 241-block message that alone measured 404ms
 * of self time (2.1% of the whole main thread) over a 19s window.
 */
const TOOL_ICON_BY_LOWER_NAME = new Map<string, { icon: typeof Wrench; category: string }>(
  Object.entries(TOOL_ICONS).map(([k, v]) => [k.toLowerCase(), v]),
)

/** Look up tool icon by name (case-insensitive), with Agent sub-type fallback */
export function getToolIcon(name: string) {
  const safeName = name || ''
  if (!safeName) return FALLBACK_TOOL_ICON
  const lower = safeName.toLowerCase()
  const entry = TOOL_ICON_BY_LOWER_NAME.get(lower)
  if (entry) return entry
  // Unrecognized name that is a known Agent sub-type → use Agent icon/category
  if (AGENT_SUBTYPE_NAMES.has(lower)) {
    return TOOL_ICONS['Agent']
  }
  return FALLBACK_TOOL_ICON
}

/**
 * Return the display name for a tool call block.
 * For Agent/Task calls with a subagent_type, show the sub-agent name
 * (PascalCased) instead of the generic "Agent".
 */
export function toolDisplayName(name: string, input?: Record<string, unknown>, displayName?: string): string {
  // Prefer pre-extracted display_name from slim SSE/DB
  if (displayName) return displayName
  const safeName = name || ''
  const lower = safeName.toLowerCase()
  // Agent-family tools: show the sub-agent type instead of the generic label.
  // OpenCode 2.x uses `subagent` + an `agent` input field (v1/Claude use `task`/
  // `agent` + `subagent_type`), so both shapes resolve here.
  if (AGENT_FAMILY_NAMES.has(lower)) {
    const raw = input?.subagent_type || input?.agent || input?.mode
    if (raw) {
      const s = String(raw)
      return s.charAt(0).toUpperCase() + s.slice(1)
    }
    if (lower !== 'agent' && lower !== 'task') return 'Agent'
  }
  return safeName
}
