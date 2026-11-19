import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The login page offers "下载 Android 应用" before the user is signed in, but
 * the download bar used to be mounted inside the authenticated `.app-container`
 * branch — so on the login screen the click changed progress state with nothing
 * rendering it and the download looked dead (#698).
 *
 * A full App.vue mount would need dozens of stubs for a purely structural fact,
 * so this guard walks the template and asserts the bar is outside that branch.
 */

const APP = readWebFile('src/App.vue')

/** Index just past the `</div>` that closes the opening tag at `start`. */
function containerEnd(template: string, start: number): number {
  const tag = /<div\b|<\/div>/g
  tag.lastIndex = start
  let depth = 0
  let match: RegExpExecArray | null
  while ((match = tag.exec(template)) !== null) {
    if (match[0] === '</div>') {
      depth -= 1
      if (depth === 0) return match.index + match[0].length
    } else {
      depth += 1
    }
  }
  throw new Error('unbalanced template: .app-container never closes')
}

describe('download progress bar mounting', () => {
  // Nested `<template #slot>` blocks close earlier in the file, so the root
  // template spans from the first opening tag to the LAST closing one.
  const template = APP.slice(APP.indexOf('<template>'), APP.lastIndexOf('</template>'))

  it('is rendered outside the authenticated .app-container branch', () => {
    const open = template.indexOf('<div v-else class="app-container"')
    expect(open).toBeGreaterThan(-1)

    const bar = template.indexOf('<DownloadProgressBar')
    expect(bar).toBeGreaterThan(-1)

    // Inside the branch ⇒ hidden on the login page, which is exactly the bug.
    expect(bar).toBeGreaterThan(containerEnd(template, open))
  })

  it('stays outside the app-container so both auth states mount it', () => {
    // The login branch is a sibling of .app-container; the bar must be a
    // sibling too, not nested in either.
    const loginBranch = template.indexOf('<LoginView')
    expect(loginBranch).toBeGreaterThan(-1)
    expect(template.indexOf('<DownloadProgressBar')).toBeGreaterThan(loginBranch)
  })
})
