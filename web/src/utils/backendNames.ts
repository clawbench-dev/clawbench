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
  kimi: 'Kimi',
  mimo: 'MiMo-Code',
  pi: 'Pi',
  deepseek: 'CodeWhale',
  vecli: 'VeCLI',
  grok: 'Grok',
  antigravity: 'Antigravity',
}

/**
 * Get the human-readable display name for an AI backend id.
 * Falls back to the backend id itself when unknown.
 */
export function getBackendDisplayName(backendId: string): string {
  return backendDisplayNames[backendId] || backendId
}
