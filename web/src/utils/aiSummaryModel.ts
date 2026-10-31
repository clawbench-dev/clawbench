/**
 * Whether the shared AI summary model is configured.
 *
 * The binding (`ai_summary.provider_id`) is the single LLM behind several
 * features: recommended reply (推荐回复), AI auto-rename (自动命名), voice
 * summary (语音摘要, when summarize.tts_backend is "api"), and the manual
 * "generate title" button. Each of them is unusable without it, and each
 * previously re-implemented its own emptiness check — so a changed config path
 * would silently apply to only some of them.
 *
 * This is the one place that knows the shape. A missing binding, or one that
 * points at 0, means "not configured".
 */
export function isSummaryModelConfigured(
  serverConfig: Record<string, unknown> | undefined,
): boolean {
  const summary = serverConfig?.ai_summary as { provider_id?: number } | undefined
  return typeof summary?.provider_id === 'number' && summary.provider_id > 0
}
