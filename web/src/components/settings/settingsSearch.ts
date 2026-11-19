/**
 * Search index for the settings home page (issue #691).
 *
 * Settings are spread over ~20 categories, so finding "the row that turns X on"
 * meant clicking through pages. This module flattens every settings row into a
 * searchable record — one hit per row, plus one hit per category — so the index
 * page can list matches directly and jump to them.
 *
 * Labels are matched in BOTH locales, not just the active one: the issue asks
 * for a term to be findable by its Chinese and English name alike, and people
 * who work in English copy with terms they remember from Chinese (and the other
 * way round).
 *
 * Every visible row of `categoryItems` is indexed, including the fields inside
 * group panels and their per-option sub-fields (e.g. the DingTalk credentials
 * under 推送通知 → 推送方式). A record's `id` is what the page later looks up
 * in the DOM, so rendering uses the same scheme (`data-settings-key`,
 * `data-search-key`, `data-category`).
 */

import i18n from '@/i18n'
import { categoryItems, type GroupPanelConfig, type ItemSpec } from './settingsFieldMap'
import { dockCategoryTitleKey } from '@/composables/dockPluginRuntime'

/**
 * Categories that exist only to give a sub-page its own panel config
 * (`tts_engine` backs `tts:tts_engine`). They are not index rows — the parent
 * page lists the same fields — and their label key does not exist, so indexing
 * them would list 「settings.categories.tts_engine」 as a page.
 */
const SUB_PAGE_ONLY_CATEGORIES = new Set(['tts_engine'])

export type SettingsHitKind = 'category' | 'item'

export interface SettingsHit {
  /**
   * Row hits use the item's setting key (`usage_meter.quota`) — the same value
   * `SettingsItem` carries in `data-settings-key`. Category hits use the plain
   * category id, which the index rows already expose as `data-category`.
   */
  id: string
  kind: SettingsHitKind
  categoryId: string
  /** Category label in the ACTIVE locale — shown as the hit's trail. */
  categoryLabel: string
  /** Row label in the active locale; empty for a category hit. */
  label: string
  /** Row description in the active locale, shown as a second line. */
  description: string
  /** Lower-cased text every query word must be found in (both locales). */
  haystack: string
}

type Dict = Record<string, unknown>

/** Resolve a dot path inside a locale object. */
function localeString(locale: Dict, key: string): string | undefined {
  let node: unknown = locale
  for (const part of key.split('.')) {
    if (!node || typeof node !== 'object') return undefined
    node = (node as Dict)[part]
  }
  return typeof node === 'string' ? node : undefined
}

/**
 * Searchable text of one i18n key in both locales.
 *
 * vue-i18n answers a missing key with the key itself, and that renders as the
 * active label too — neither is text worth matching, so both are dropped.
 */
function textOfKey(locale: Dict, key: string | undefined): string[] {
  if (!key) return []
  const active = i18n.global.t(key)
  return [active, localeString(locale, key)]
    .filter((v): v is string => !!v && v !== key)
}

/** The config-key fallback lets a user paste `rag.chunk_size` and find the row. */
function haystackOf(locale: Dict, keys: (string | undefined)[], fallback: string[]): string {
  const words = keys.flatMap(k => textOfKey(locale, k))
  return [...words, ...fallback].join('\n').toLowerCase()
}

function activeLabel(key: string): string {
  return i18n.global.t(key)
}

function categoryLabelOf(categoryId: string): string {
  return activeLabel(dockCategoryTitleKey(categoryId) ?? `settings.categories.${categoryId}`)
}

/** Build a row hit for one `ItemSpec`, or null for dividers. */
function itemHit(
  locale: Dict,
  spec: ItemSpec,
  categoryId: string,
  extraKeys: (string | undefined)[],
): SettingsHit | null {
  if (spec.type === 'header') return null
  const categoryLabel = categoryLabelOf(categoryId)
  return {
    id: spec.key,
    kind: 'item',
    categoryId,
    categoryLabel,
    label: activeLabel(spec.labelKey),
    description: spec.descriptionKey ? activeLabel(spec.descriptionKey) : '',
    haystack: haystackOf(
      locale,
      [spec.labelKey, spec.descriptionKey, ...extraKeys, ...(spec.options ?? []).map(o => o.labelKey)],
      [spec.key, categoryLabel],
    ),
  }
}

/** Category hit + every row hit of that category, in page order. */
function collectCategory(locale: Dict, categoryId: string): SettingsHit[] {
  const titleKey = dockCategoryTitleKey(categoryId) ?? `settings.categories.${categoryId}`
  const hits: SettingsHit[] = [{
    id: categoryId,
    kind: 'category',
    categoryId,
    categoryLabel: activeLabel(titleKey),
    label: '',
    description: '',
    haystack: haystackOf(locale, [titleKey], [categoryId]),
  }]
  for (const entry of categoryItems[categoryId] ?? []) {
    if (entry.type === 'item') {
      const hit = itemHit(locale, entry.spec, categoryId, [])
      if (hit) hits.push(hit)
      continue
    }
    const cfg = entry.config
    const panelKeys = [cfg.titleKey]
    if (cfg.enableKey && cfg.enableLabelKey) {
      const hit = itemHit(locale, { labelKey: cfg.enableLabelKey, key: cfg.enableKey, type: 'switch', source: 'local' }, categoryId, panelKeys)
      if (hit) hits.push(hit)
    }
    const specs: ItemSpec[] = [
      ...(cfg.entrySelector ? [cfg.entrySelector] : []),
      ...cfg.commonFields,
      ...(cfg.optionSubFields ?? []).flatMap(o => o.fields),
    ]
    for (const spec of specs) {
      const hit = itemHit(locale, spec, categoryId, panelKeys)
      if (hit) hits.push(hit)
    }
  }
  return hits
}

/** The message tree of the ACTIVE locale, for matching its labels. */
function activeMessages(): Dict {
  return i18n.global.getLocaleMessage(i18n.global.locale.value) as Dict
}

const cache = new Map<string, SettingsHit[]>()

/** Every hit in the active locale, built once per locale (the config is static). */
export function settingsHits(): SettingsHit[] {
  const locale = String(i18n.global.locale.value)
  const cached = cache.get(locale)
  if (cached) return cached
  const messages = activeMessages()
  const hits: SettingsHit[] = []
  for (const categoryId of Object.keys(categoryItems)) {
    if (SUB_PAGE_ONLY_CATEGORIES.has(categoryId)) continue
    hits.push(...collectCategory(messages, categoryId))
  }
  cache.set(locale, hits)
  return hits
}

/**
 * Rank a hit against the first query word.
 *
 * Deliberately simple and predictable:
 *  1. a label that starts with the query (typing 「邮」 finds 邮件 first),
 *  2. a label that contains it,
 *  3. a description / category name match.
 * A category row loses a tie to a real row: the user asks for a setting, not
 * for the page around it.
 */
function scoreOf(hit: SettingsHit, firstWord: string): number {
  const label = hit.label.toLowerCase()
  if (label && label.startsWith(firstWord)) return 100
  if (label && label.includes(firstWord)) return 60
  return hit.kind === 'category' ? 15 : 30
}

/**
 * Search the active locale's index. An empty query returns nothing — the plain category
 * list is already on the page below the box. Several words are ANDed, so
 * 「邮件 间隔」narrows instead of widening.
 */
export function searchSettings(query: string, limit = 50): SettingsHit[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return []
  const out: { hit: SettingsHit; score: number }[] = []
  for (const hit of settingsHits()) {
    if (!words.every(w => hit.haystack.includes(w))) continue
    out.push({ hit, score: scoreOf(hit, words[0]) })
  }
  out.sort((a, b) => b.score - a.score)
  return out.slice(0, limit).map(s => s.hit)
}

/** Category id → the rows of it that matched, in index order. */
export function groupHitsByCategory(hits: SettingsHit[]): Map<string, SettingsHit[]> {
  const map = new Map<string, SettingsHit[]>()
  for (const hit of hits) {
    const list = map.get(hit.categoryId)
    if (list) list.push(hit)
    else map.set(hit.categoryId, [hit])
  }
  return map
}

/**
 * Which fields of a group panel to render when the page is filtered by search.
 *
 * A panel renders its fields from `dependsOn` conditions, so a hit may point at
 * a field that is currently hidden (the DingTalk credentials while 推送方式 is
 * 飞书). Jumping to a row that was never rendered lands on nothing, so the
 * filtered page renders the panel with `onlyKeys`: the matching fields are
 * forced visible and every other field is hidden.
 *
 * A field the user CAN currently see is left to the normal rules — the panel
 * stays readable while searching, and only the rows that would otherwise be
 * missing are forced in.
 */
export function panelSearchKeys(
  cfg: GroupPanelConfig,
  wanted: Set<string>,
  isVisible: (field: ItemSpec) => boolean,
): Set<string> {
  const keys = new Set<string>()
  const consider = (field: ItemSpec) => {
    if (wanted.has(field.key) && !isVisible(field)) keys.add(field.key)
  }
  // The enable switch and the entry selector are always rendered (they gate
  // the fields), so they are matched by presence alone.
  if (cfg.enableKey && wanted.has(cfg.enableKey)) keys.add(cfg.enableKey)
  if (cfg.entrySelector && wanted.has(cfg.entrySelector.key)) keys.add(cfg.entrySelector.key)
  for (const field of cfg.commonFields) consider(field)
  for (const sub of cfg.optionSubFields ?? []) {
    for (const field of sub.fields) {
      // Sub-fields of an option that is not selected are unreachable in the
      // live form, so they are always forced in — the row becomes visible
      // without switching the mode first.
      if (wanted.has(field.key)) keys.add(field.key)
    }
  }
  return keys
}

/** One slice of a result's text, flagged when it is part of the query. */
export interface HighlightPart {
  text: string
  hit: boolean
}

/**
 * Split `text` into plain and matched slices, so the result list can mark the
 * words the user typed (issue #691: 命中结果高亮展示).
 *
 * Matching mirrors the index: case-insensitive, every query word counts, and
 * overlapping words resolve to the earliest match (the longer one on a tie) so
 * a word is never cut in half.
 */
export function highlightParts(text: string, query: string): HighlightPart[] {
  const words = [...new Set(query.trim().toLowerCase().split(/\s+/).filter(Boolean))]
  if (!text || words.length === 0) return [{ text, hit: false }]
  const lower = text.toLowerCase()
  const parts: HighlightPart[] = []
  let i = 0
  while (i < text.length) {
    let at = -1
    let len = 0
    for (const word of words) {
      const found = lower.indexOf(word, i)
      if (found === -1) continue
      if (at === -1 || found < at || (found === at && word.length > len)) {
        at = found
        len = word.length
      }
    }
    if (at === -1) {
      parts.push({ text: text.slice(i), hit: false })
      break
    }
    if (at > i) parts.push({ text: text.slice(i, at), hit: false })
    parts.push({ text: text.slice(at, at + len), hit: true })
    i = at + len
  }
  return parts
}
