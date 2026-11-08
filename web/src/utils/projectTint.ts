/**
 * Stable colour for a project, used by the "other projects" cards (the header
 * dot and the card's tinted band).
 *
 * Theme tokens rather than fixed hex values: the palette follows all 36 themes
 * and stays legible in both light and dark. Collisions between two projects are
 * acceptable — the card header also carries the project name.
 */
const PROJECT_TINT_VARS = ['--color-purple', '--color-orange', '--color-green', '--color-info']

export function projectTintVar(projectPath: string): string {
  let hash = 0
  for (let i = 0; i < projectPath.length; i++) {
    hash = (hash * 31 + projectPath.charCodeAt(i)) | 0
  }
  const index = Math.abs(hash) % PROJECT_TINT_VARS.length
  return `var(${PROJECT_TINT_VARS[index]})`
}
