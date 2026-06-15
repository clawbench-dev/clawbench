/**
 * Terminal font size utilities — pure functions extracted from TerminalPanelContent.
 */

const FONT_SIZE_KEY = 'clawbench-terminal-font-size'
export const DEFAULT_FONT_SIZE = 12
const MIN_FONT_SIZE = 8
const MAX_FONT_SIZE = 28

/**
 * Load font size from localStorage with bounds checking.
 * Returns DEFAULT_FONT_SIZE when no valid value is stored.
 */
export function loadFontSize(getItem: (key: string) => string | null = localStorage.getItem.bind(localStorage)): number {
  const saved = getItem(FONT_SIZE_KEY)
  if (saved) {
    const n = parseInt(saved, 10)
    if (n >= MIN_FONT_SIZE && n <= MAX_FONT_SIZE) return n
  }
  return DEFAULT_FONT_SIZE
}

/**
 * Clamp font size to [MIN_FONT_SIZE, MAX_FONT_SIZE] and persist.
 * Returns the clamped value.
 */
export function applyFontSize(
  size: number,
  setItem: (key: string, value: string) => void = localStorage.setItem.bind(localStorage),
): number {
  const clamped = Math.max(MIN_FONT_SIZE, Math.min(MAX_FONT_SIZE, size))
  setItem(FONT_SIZE_KEY, String(clamped))
  return clamped
}

/**
 * Shorten a CWD path for display in the terminal header.
 * Shows ".../last2/parts" for paths deeper than 2 levels.
 */
export function shortCwd(cwd: string | undefined | null): string {
  if (!cwd) return ''
  const parts = cwd.split('/')
  return parts.length > 2 ? '.../' + parts.slice(-2).join('/') : cwd
}

/**
 * Determine if the reconnect button should be shown based on error code.
 * terminal_disabled means the feature is turned off — no point reconnecting.
 */
export function canReconnect(errorCode: string | undefined | null): boolean {
  if (errorCode === 'terminal_disabled' || errorCode === 'platform_unsupported') return false
  return true
}

/**
 * Get the display message for the error overlay based on error code.
 */
export function errorDisplayMessage(
  errorCode: string | undefined | null,
  errorMessage: string | undefined | null,
  fallback: string,
): string {
  if (errorCode === 'terminal_disabled') return fallback // t('terminal.disabled') passed as fallback
  if (errorCode === 'shell_start_failed') return fallback
  if (errorCode === 'platform_unsupported') return fallback
  return errorMessage || fallback
}

/**
 * Check if the error overlay should be shown.
 */
export function showErrorOverlay(connectionState: string): boolean {
  return connectionState === 'error' || connectionState === 'disconnected'
}
