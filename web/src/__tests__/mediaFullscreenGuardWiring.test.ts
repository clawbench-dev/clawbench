import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring contract for the media-fullscreen guard.
 *
 * The guard only works if it is installed in EVERY SPA entry that can render a
 * media element. It is a document-level listener with no component, so a
 * missing install is invisible to component tests and to the behavior suite
 * (which installs it itself) — only a source assertion catches it.
 *
 * Both entries matter: the share SPA renders the same VideoPreview, so a
 * fullscreen video whose shared file is revoked would freeze that page too.
 */
describe('mediaFullscreenGuard wiring', () => {
  for (const entry of ['src/main.ts', 'src/share/main.ts']) {
    it(`${entry} imports and installs the guard`, () => {
      const src = readWebFile(entry)
      expect(src).toMatch(/import \{[^}]*installMediaFullscreenGuard[^}]*\} from '.*mediaFullscreenGuard(\.ts)?'/)
      expect(src).toContain('installMediaFullscreenGuard()')
    })
  }
})
