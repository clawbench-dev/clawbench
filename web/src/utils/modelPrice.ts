/**
 * Per-million-token list price for a model (issue #643).
 *
 * The backend derives this from the models.dev catalog snapshot at response
 * time — it is never stored, so it always reflects the current catalog. An
 * absent `price` means no trustworthy number exists (the catalog does not know
 * the model, or only one reseller quotes it); the picker then shows no tag.
 */
export interface ModelPrice {
  /** USD per million input tokens. */
  input: number
  /** USD per million output tokens. */
  output: number
  /**
   * True when the price did not come from the model's own vendor — it is a
   * consensus across resellers, or matched by display name rather than id.
   * Rendered with a leading "~" so it never reads as an exact quote.
   */
  approx?: boolean
}
