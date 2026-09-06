/**
 * Recent-projects display helpers.
 *
 * The recent-project rows show each project path relative to the user's home
 * directory when it lives under it (cleaner, shorter rows); otherwise the
 * absolute path is shown.
 */

/**
 * Normalize Windows backslashes to forward slashes so the prefix comparison
 * works on either separator style (the backend may return native-form paths).
 */
function norm(p: string): string {
  return p.replace(/\\/g, '/')
}

/**
 * The display path for a recent-project row: `path` made relative to `homeDir`
 * when it starts with it, otherwise the original `path` unchanged.
 *
 * Note: the prefix match compares normalized (forward-slash) forms, but the
 * slice length uses the ORIGINAL `path` byte offset. This is safe only while
 * '/' and '\' are single-byte — which holds for the filesystem paths the
 * backend returns, so no adjustment is needed here.
 */
export function recentProjectDisplayPath(path: string, homeDir: string): string {
  const normHome = norm(homeDir)
  const normP = norm(path)
  return normHome && normP.startsWith(normHome + '/')
    ? path.slice(homeDir.length + 1)
    : path
}
