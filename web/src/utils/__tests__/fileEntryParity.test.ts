import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'
import { normalizeFileEntry, type FileEntry } from '@/utils/fileAttachmentUtils'

/**
 * Every field the backend's `model.FileEntry` can carry must survive the
 * client's normalizer.
 *
 * `normalizeFileEntry` runs on EVERY render and dedupe pass, and it rebuilds the
 * entry field by field. A field it forgets is not an error anywhere — it is
 * silently absent from the outgoing payload, so the quote still renders, still
 * sends, and only fails when the user clicks "跳转来源" (nothing happens) or when
 * the AI never receives the locator it needed.
 *
 * This has already happened twice: `mailId` and `sourceDir` were added to the Go
 * struct and to every producer, and were missing here. The guard that was
 * supposed to catch it listed the locators BY HAND, so it only ever checked the
 * fields someone remembered to list — which is the same mistake, one level up.
 *
 * So the list is not written here. It is READ from the Go struct, which is the
 * single source of truth for what a `FileEntry` can hold. Adding a field on the
 * backend without teaching the client about it now fails this test by name.
 */
interface GoField {
  /** Go field name, for a readable failure message. */
  go: string
  /** JSON name — also the key the client uses. */
  json: string
  type: string
}

/** Extract the fields of `type FileEntry struct { … }` from the Go model. */
function readGoFileEntryFields(): GoField[] {
  const src = readWebFile('internal/model/chat.go')
  const start = src.indexOf('type FileEntry struct {')
  if (start < 0) throw new Error('type FileEntry struct not found in internal/model/chat.go')
  const body = src.slice(start)
  const end = body.indexOf('\n}')
  if (end < 0) throw new Error('unterminated FileEntry struct')
  const fields: GoField[] = []
  for (const line of body.slice(0, end).split('\n')) {
    const m = /^\s*([A-Z]\w*)\s+([\w.[\]*]+)\s+`json:"([^",]+)/.exec(line)
    if (!m) continue
    fields.push({ go: m[1], json: m[3], type: m[2] })
  }
  if (fields.length === 0) throw new Error('parsed zero fields out of FileEntry — the parser drifted')
  return fields
}

/** A value of the right shape for a Go field type. */
function sampleValue(type: string): unknown {
  if (type === 'bool') return true
  if (type.startsWith('int') || type.startsWith('float')) return 7
  // `map[string]string` — the locator bag. Sampled as a real bag rather than a
  // string, so a normalizer that (say) stringified it would fail by name.
  if (type.startsWith('map[')) return { sample: 'value' }
  return 'sample-value'
}

describe('FileEntry parity — client normalizer vs Go model', () => {
  const fields = readGoFileEntryFields()

  it('parses the struct it is guarding (so a parser drift cannot pass silently)', () => {
    const names = fields.map(f => f.json)
    // Spot-check the shapes that matter most: if the parser ever stops finding
    // these, every assertion below becomes vacuously true.
    expect(names).toContain('path')
    expect(names).toContain('kind')
    expect(names).toContain('text')
    expect(names).toContain('note')
  })

  it.each(fields.map(f => [f.json, f] as const))(
    'normalizeFileEntry keeps %s',
    (_json, field) => {
      const value = sampleValue(field.type)
      const input = { path: 'label', [field.json]: value } as unknown as FileEntry
      const normalized = normalizeFileEntry(input) as Record<string, unknown>

      expect(
        normalized[field.json],
        `model.FileEntry.${field.go} (json "${field.json}") is dropped by normalizeFileEntry — ` +
        `a quote carrying it will silently lose that locator after one render/dedupe pass`,
      ).toEqual(value)
    },
  )
})
