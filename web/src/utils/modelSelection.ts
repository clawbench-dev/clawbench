/**
 * Resolve a session's model choice against an agent's CURRENT model list.
 *
 * Kept as a leaf module (no composable imports) because three call sites need
 * it and they sit on both sides of the useAgents/useSessionIdentity cycle:
 * initial session load, post-refresh reconciliation, and the ACP
 * `model_list_update` handler. Putting it here also means tests that mock
 * `@/composables/useAgents` do not have to grow another export.
 *
 * The rule set:
 *  - a candidate the list still offers wins (its display name is refreshed);
 *  - a candidate a POPULATED list no longer offers is retired — fall back to
 *    the agent's default, because the runtime cannot run it (ACP would
 *    silently keep its own model; a CLI backend would receive a dead id);
 *  - an EMPTY list means "discovery has not run/reported yet", not "retired",
 *    so the candidate is kept verbatim rather than rewritten on no evidence.
 */

export interface ModelChoice {
  id: string
  name?: string
  default?: boolean
}

export interface ModelSelection {
  modelId: string
  modelName: string
  retired: boolean
}

/**
 * Strip the provider scope from a model id.
 *
 * The built-in agent can run several providers at once, so its picker hands out
 * `p<providerId>:<model>` ids (issue #753). Sessions created before that hold
 * the bare id, and a user reading a model name should never see the scope — so
 * every place that compares or displays an id goes through this.
 *
 * Any other id is returned unchanged: an ACP/CLI id is opaque, and a colon in it
 * (a gateway namespacing its own ids) must not be mistaken for our scope.
 */
export function bareModelId(id: string): string {
  const m = /^p\d+:(.+)$/.exec((id || '').trim())
  return m ? m[1] : id
}

/**
 * Pick the agent's default model id.
 *
 * Priority: `preferredModel` (the user's choice) > first entry flagged
 * `default` > first in the list. The preference only wins while the list can
 * still run it — an empty list means "not discovered yet", so it still wins
 * there (there is no evidence it was retired).
 */
export function pickDefaultModelId(models: ModelChoice[], preferredModel: string): string {
  const preferred = (preferredModel || '').trim()
  if (preferred && (models.length === 0 || models.some(m => m.id === preferred))) {
    return preferred
  }
  if (models.length === 0) return ''
  const defaultModel = models.find(m => m.default)
  return defaultModel ? defaultModel.id : models[0].id
}

/**
 * Resolve `candidateId` against `models`.
 *
 * `nameOf` supplies a display name for an id the list does not contain — the
 * list may simply not have been discovered yet, in which case the id is still
 * authoritative and only its label is unknown.
 */
export function resolveModelSelection(
  models: ModelChoice[],
  preferredModel: string,
  candidateId: string,
  nameOf?: (id: string) => string | undefined,
): ModelSelection {
  const candidate = (candidateId || '').trim()

  if (candidate) {
    const known = models.find(m => m.id === candidate)
    if (known) return { modelId: candidate, modelName: known.name || candidate, retired: false }
    if (models.length === 0) {
      return { modelId: candidate, modelName: nameOf?.(candidate) || candidate, retired: false }
    }
    // A session created before the picker scoped ids to providers holds the
    // bare name, while the list now offers `pN:<same name>`. That is the SAME
    // model, not a retired one — falling back to the default here would move
    // the session off the model it is actually running on the first reload.
    //
    // Only an UNAMBIGUOUS match counts: two providers may serve the same name,
    // and guessing between them would silently switch providers.
    const bare = bareModelId(candidate)
    if (bare !== candidate) {
      // Already scoped, so the exact match above is the only correct one.
      const modelId = pickDefaultModelId(models, preferredModel)
      const fallback = models.find(m => m.id === modelId)
      return { modelId, modelName: fallback?.name || modelId, retired: true }
    }
    const sameName = models.filter(m => bareModelId(m.id) === bare)
    if (sameName.length === 1) {
      return { modelId: sameName[0].id, modelName: sameName[0].name || bare, retired: false }
    }
    const modelId = pickDefaultModelId(models, preferredModel)
    const fallback = models.find(m => m.id === modelId)
    return { modelId, modelName: fallback?.name || modelId, retired: true }
  }

  const modelId = pickDefaultModelId(models, preferredModel)
  const fallback = models.find(m => m.id === modelId)
  return { modelId, modelName: fallback?.name || modelId, retired: false }
}
