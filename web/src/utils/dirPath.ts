/**
 * How a directory path becomes a listing request.
 *
 * Two callers browse directories: the file manager (via the store) and the
 * standalone file picker. They must agree on three things or one of them
 * silently 403s / shows the wrong folder:
 *
 *  - relative paths resolve against the project root, so a leading/trailing
 *    slash has to go (`/src/` → `src`);
 *  - an absolute path INSIDE the project root is converted to project-relative
 *    first, because `/api/dir` refuses absolute paths;
 *  - an absolute path OUTSIDE the project root is a different endpoint
 *    (`/api/projects`), and keeps its absolute form — that is what makes the
 *    breadcrumb, the parent button and every `joinPath` call stay correct.
 *
 * Extracted rather than duplicated: the file picker needs the exact same rules,
 * and a second hand-written copy is how the two would drift on the external
 * case (the one that is easy to forget, since it needs a project root).
 */
import { isAbsolutePath, normalizeSlashes, toProjectRelative } from './path'

export interface DirRequest {
  /** The directory identity to keep in state (normalized, no trailing slash). */
  dir: string
  /** Listed through `/api/projects` because it lies outside the project root. */
  isExternal: boolean
  /** The listing endpoint to call. */
  url: string
}

/**
 * Resolve `dir` (as typed by the user / remembered in state) into a request.
 *
 * `projectRoot` may be empty: without it the path cannot be classified as
 * external, and the legacy behavior (treat it as project-relative) is the safe
 * fallback — an unknown root means every request 403s regardless.
 */
export function resolveDirRequest(dir: string, projectRoot: string): DirRequest {
  // Windows paths from the backend use "/", but user input and chat annotations
  // may carry "\"; normalize before any comparison.
  let next = normalizeSlashes(dir)
  // `/api/dir` resolves relative paths against the project root, so passing an
  // absolute path (E:/git/…, possibly from file search or chat annotation) must
  // be converted to project-relative first.
  next = toProjectRelative(next, projectRoot)
  const isExternal = isAbsolutePath(next) && !!projectRoot
  if (!isExternal) {
    // Defensive: strip leading/trailing slashes so the stored dir is always a
    // clean project-relative path. The Go backend treats paths starting with
    // "/" as absolute filesystem paths.
    next = next.replace(/^\/+/, '').replace(/\/+$/, '')
  } else {
    // Drop a trailing slash (but keep the root's own "/") so identity
    // comparisons against listing paths stay stable.
    next = next.replace(/\/+$/, '') || '/'
  }
  return {
    dir: next,
    isExternal,
    url: isExternal
      ? `/api/projects?path=${encodeURIComponent(next)}`
      : next
        ? `/api/dir?path=${encodeURIComponent(next)}`
        : '/api/dir?path=',
  }
}
