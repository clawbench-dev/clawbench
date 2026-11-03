import { ref } from 'vue'

/**
 * Provider id → display name, shared by the screens that show a provider
 * reference without fetching the list themselves (the settings row for the
 * shared summary model only knows `ai_summary.provider_id`).
 *
 * Whoever loads `/api/providers` records the names here, so the row can render
 * "provider · model" — the same shape the picker uses — without adding another
 * request to every settings page. It is a `ref` so a row rendered before the
 * list arrives still updates once it does.
 */
const providerNames = ref<Record<number, string>>({})

/** Merge a freshly loaded provider list into the shared name map. */
export function rememberProviderNames(list: readonly { id: number; name: string }[]): void {
  if (list.length === 0) return
  const next = { ...providerNames.value }
  for (const p of list) next[p.id] = p.name
  providerNames.value = next
}

/** Display name of a provider, or '' when it has not been loaded yet. */
export function providerNameOf(id: number | null | undefined): string {
  if (id == null) return ''
  return providerNames.value[id] ?? ''
}
