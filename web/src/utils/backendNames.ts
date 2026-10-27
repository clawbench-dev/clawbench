/**
 * Static mapping of AI backend id → human-readable display name.
 * Mirrors the BackendSpec.Name values defined in internal/ai/backends/*.
 * Used where a backend's display name is needed without the agent name
 * (e.g. the ACP session resume drawer header).
 */

const backendDisplayNames: Record<string, string> = {
  claude: 'Claude',
  codebuddy: 'Codebuddy',
  opencode: 'OpenCode',
  codex: 'Codex',
  copilot: 'Copilot',
  qoder: 'Qoder',
  qodercn: 'Qoder CN',
  kimi: 'Kimi',
  mimo: 'MiMo-Code',
  pi: 'Pi',
  deepseek: 'CodeWhale',
  dsh: 'DeepSeek Harness',
  vecli: 'VeCLI',
  grok: 'Grok',
  antigravity: 'Antigravity',
  hermes: 'Hermes Agent',
  commandcode: 'Command Code',
  cursor: 'Cursor',
  openclaw: 'OpenClaw',
  // User-defined ACP agents (issue #557) all share one backend id; the agent's
  // own name is what distinguishes them, so this label is only a fallback for
  // places that show the backend alone (share header, resume drawer).
  'custom-acp': 'ACP',
}

/**
 * Get the human-readable display name for an AI backend id.
 * Falls back to the backend id itself when unknown.
 */
export function getBackendDisplayName(backendId: string): string {
  return backendDisplayNames[backendId] || backendId
}
