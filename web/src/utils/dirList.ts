/**
 * Shared URL builder for listing a directory.
 *
 * The file manager, the docked preview pane and the floating preview card all
 * list directories, and they can all be pointed at a project-EXTERNAL directory
 * (a chat annotation may name any path the server exposes). `/api/dir` only
 * resolves inside the project root — it answers 400 NotADirectory for anything
 * else — while `/api/projects` serves any absolute path under a configured root
 * and returns the same `{ items }` entry shape.
 *
 * So the endpoint is chosen from the path's own form: absolute → /api/projects,
 * relative (or empty, meaning the project root) → /api/dir. Centralized here so
 * the three call sites cannot drift apart.
 */

import { isAbsolutePath } from '@/utils/path.ts'

/** The directory-listing endpoint + query for `path`. */
export function buildDirListUrl(path: string): string {
  if (isAbsolutePath(path)) {
    return `/api/projects?path=${encodeURIComponent(path)}`
  }
  return path ? `/api/dir?path=${encodeURIComponent(path)}` : '/api/dir?path='
}
