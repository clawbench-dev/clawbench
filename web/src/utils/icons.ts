import {
  // Tool icons
  Eye, PenLine, FilePenLine, SquareTerminal, Terminal, Search, Folder,
  Globe, Bot, Sparkles, MessageSquarePlus, Plus, Pencil,
  CircleDot, ListChecks, ListTodo, Target,
  FileText, Compass, CheckCircle2, FolderSync, Monitor,
  Users, MessageSquare, Send, Save, Camera, Wrench,
  ShieldAlert, GitBranch,
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

/** Look up tool icon by name (case-insensitive) */
export function getToolIcon(name: string) {
  const entry = Object.entries(TOOL_ICONS).find(([k]) => k.toLowerCase() === name.toLowerCase())
  return entry ? entry[1] : FALLBACK_TOOL_ICON
}
