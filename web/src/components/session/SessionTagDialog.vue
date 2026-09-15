<template>
  <ModalDialog :open="open" :title="t('sessionTags.title')" :z-index="2600" @close="close">
    <div class="session-tags-dialog">
      <!-- Failures must be visible: a silent catch leaves the user staring at an
           unchanged dialog with no way to tell a failed save from a no-op. -->
      <div v-if="errorMsg" class="st-error">{{ errorMsg }}</div>
      <!-- Existing candidates: global tags + this project's tags -->
      <div class="st-section-label">{{ t('sessionTags.existingLabel') }}</div>
      <div v-if="loading" class="st-empty">{{ t('common.loading') }}</div>
      <div v-else-if="candidates.length === 0" class="st-empty">{{ t('sessionTags.emptyCandidates') }}</div>
      <div v-else class="st-candidate-list">
        <div
          v-for="tag in candidates"
          :key="tag.name"
          class="st-candidate"
          :class="{ selected: isSelected(tag.name) }"
        >
          <label class="st-candidate-main">
            <input
              type="checkbox"
              class="st-checkbox"
              :style="{ '--st-tick': checkboxTickColor }"
              :checked="isSelected(tag.name)"
              @change="toggleTag(tag.name)"
            />
            <span class="st-chip" :style="tagAccentStyle(tag.name)">
              <span class="st-chip-name">{{ tag.name }}</span>
            </span>
            <span v-if="tag.scope === 'global'" class="st-scope-badge">{{ t('sessionTags.scopeGlobal') }}</span>
          </label>
          <!-- Deleting removes the label from every session; confirm first. -->
          <button
            class="st-delete-btn"
            type="button"
            :title="t('sessionTags.deleteTag')"
            :aria-label="t('sessionTags.deleteTag')"
            @click.stop="requestDelete(tag)"
          >
            <Trash2 :size="14" />
          </button>
        </div>
      </div>

      <!-- Create a new tag -->
      <div class="st-section-label">{{ t('sessionTags.addLabel') }}</div>
      <div class="st-add-row">
        <input
          v-model="newTagName"
          class="st-input"
          type="text"
          maxlength="32"
          :placeholder="t('sessionTags.addPlaceholder')"
          @keydown.enter.prevent="addNewTag"
        />
        <select v-model="newTagScope" class="st-scope-select" :title="t('sessionTags.scopeLabel')">
          <option value="project">{{ t('sessionTags.scopeProject') }}</option>
          <option value="global">{{ t('sessionTags.scopeGlobal') }}</option>
        </select>
        <button class="st-add-btn" type="button" :disabled="!canAdd" @click="addNewTag">
          <Plus :size="14" />
          {{ t('common.create') }}
        </button>
      </div>
      <div v-if="newTagScope === 'global'" class="st-scope-hint">{{ t('sessionTags.globalHint') }}</div>
    </div>

    <template #footer>
      <button class="fbtn" type="button" @click="close">{{ t('common.cancel') }}</button>
      <button class="fbtn fbtn-primary" type="button" :disabled="saving" @click="save">
        {{ t('common.confirm') }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup>
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Trash2 } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { useDialog } from '@/composables/useDialog.ts'
import { apiGet, apiDelete, apiPatch } from '@/utils/api.ts'
import { tagAccentStyle, readableTextOn } from '@/utils/tagColor.ts'
import { appLog } from '@/utils/appLog'
import { store } from '@/stores/app.ts'

const props = defineProps({
  open: { type: Boolean, default: false },
  sessionId: { type: String, default: '' },
  initialTags: { type: Array, default: () => [] },
})

const emit = defineEmits(['close', 'saved'])

const { t } = useI18n()
const dialog = useDialog()

const candidates = ref([])      // selectable registry entries
const selected = ref([])        // names currently attached to the session
const loading = ref(false)
const saving = ref(false)
const errorMsg = ref('')
const newTagName = ref('')
const newTagScope = ref('project')

/**
 * Tick colour for the checked checkbox.
 *
 * The box is filled with the theme's --accent-color and the tick sits on top,
 * so the tick has to contrast with the ACCENT, not the dialog surface. A fixed
 * white fails on most themes — the accents are mid-to-light blues, and white
 * scored as low as 1.69:1, below 4.5:1 on 28 of 36 themes. Resolving the accent
 * and picking the better of near-black/white puts every theme at or above
 * 4.5:1, which no static CSS rule can do because it cannot evaluate luminance.
 *
 * Recomputed when `store.state.theme` changes; the CSS variable is read after
 * the theme attribute has been applied, so `nextTick` is required for the new
 * value to be visible.
 */
const checkboxTickColor = ref('#ffffff')
watch(
  () => store.state.theme,
  async () => {
    await nextTick()
    const accent = getComputedStyle(document.documentElement)
      .getPropertyValue('--accent-color')
      .trim()
    if (accent) checkboxTickColor.value = readableTextOn(accent)
  },
  { immediate: true },
)

/**
 * Normalize a tag name the same way the backend does (`strings.Fields` + join,
 * lowercased): trim, collapse whitespace runs, and lowercase. Without this a
 * name typed as "Needs  review" would render that way in the dialog but come
 * back as "needs review" after saving, i.e. the chip would silently change.
 * Lowercasing matters because the backend treats tag identity as
 * case-insensitive, so "Bug" and "bug" are the SAME tag.
 */
function normalizeTagName(raw) {
  return String(raw ?? '').trim().split(/\s+/).filter(Boolean).join(' ').toLowerCase()
}

const canAdd = computed(() => normalizeTagName(newTagName.value).length > 0)

function isSelected(name) {
  return selected.value.includes(name)
}

function toggleTag(name) {
  if (isSelected(name)) {
    selected.value = selected.value.filter(n => n !== name)
  } else {
    selected.value = [...selected.value, name]
  }
}

/**
 * Scope of a name: an existing candidate keeps its own scope (the backend also
 * refuses to re-scope an existing tag), a brand-new name uses the picker.
 */
function scopeFor(name) {
  const existing = candidates.value.find(c => c.name === name)
  if (existing) return existing.scope
  return newTagScope.value
}

function addNewTag() {
  const name = normalizeTagName(newTagName.value)
  if (!name) return
  // Adding a name that already exists in the candidate list must not create a
  // duplicate chip — just select it.
  if (!candidates.value.some(c => c.name === name)) {
    // `pending: true` marks a tag that exists only in this dialog. It has no
    // server-side definition yet, which requestDelete relies on: asking the
    // server to delete it would fail and the row would appear undeletable.
    candidates.value = [...candidates.value, { name, scope: newTagScope.value, count: 0, pending: true }]
  }
  if (!isSelected(name)) selected.value = [...selected.value, name]
  newTagName.value = ''
}

/**
 * Removes a tag from this dialog.
 *
 * For a tag that already exists server-side this deletes the DEFINITION (from
 * every session), which is why it confirms first and sends the scope the user
 * saw — when the same name exists globally and as a project tag, omitting the
 * scope could destroy a global label shared by every project.
 *
 * A tag created in this dialog but not yet saved has no server-side definition,
 * so there is nothing to delete: the request would fail and the row would look
 * undeletable. Dropping it locally is exactly "undo the creation", and it needs
 * no confirmation since nothing outside this dialog is affected.
 */
async function requestDelete(tag) {
  if (tag.pending) {
    candidates.value = candidates.value.filter(c => c.name !== tag.name)
    selected.value = selected.value.filter(n => n !== tag.name)
    return
  }
  const confirmed = await dialog.confirm(
    t('sessionTags.deleteConfirm', { name: tag.name }),
    { confirmText: t('common.delete'), cancelText: t('common.cancel') }
  )
  if (!confirmed) return
  errorMsg.value = ''
  try {
    const query = `name=${encodeURIComponent(tag.name)}&scope=${encodeURIComponent(tag.scope || 'project')}`
    await apiDelete(`/api/ai/session/tags?${query}`)
    candidates.value = candidates.value.filter(c => c.name !== tag.name)
    selected.value = selected.value.filter(n => n !== tag.name)
    // Other rows may carry this label too — refresh them.
    store.state.sessionListVersion++
  } catch (err) {
    appLog.e('SessionTagDialog', 'Failed to delete tag:', err)
    errorMsg.value = t('sessionTags.deleteFailed')
  }
}

async function load() {
  if (!props.sessionId) return
  loading.value = true
  errorMsg.value = ''
  try {
    const res = await apiGet(`/api/ai/session/tags`)
    candidates.value = res?.tags || []
    // Seed the selection from the session list's already-loaded copy so the
    // dialog opens instantly; the save is authoritative either way.
    selected.value = [...(props.initialTags || [])]
  } catch (err) {
    appLog.e('SessionTagDialog', 'Failed to load tags:', err)
    candidates.value = []
    // Without this the dialog renders "no tags yet" while the session's real
    // tags are still selected but invisible — the user would think the session
    // has no tags at all.
    errorMsg.value = t('sessionTags.loadFailed')
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!props.sessionId) return
  saving.value = true
  errorMsg.value = ''
  try {
    // A name typed into the input but not yet committed with the 创建 button
    // must not be silently dropped: the user filled the field and pressed
    // 确定, so saving without it looks like the tag was lost. Fold it into the
    // payload (and mark it selected) before sending.
    const pending = normalizeTagName(newTagName.value)
    const names = pending && !selected.value.includes(pending)
      ? [...selected.value, pending]
      : selected.value
    const tags = names.map(name => ({ name, scope: scopeFor(name) }))
    await apiPatch(
      `/api/ai/session/update?session_id=${encodeURIComponent(props.sessionId)}`,
      { tags }
    )
    store.state.sessionListVersion++
    emit('saved')
    emit('close')
  } catch (err) {
    appLog.e('SessionTagDialog', 'Failed to save tags:', err)
    // Keep the dialog open with the user's edits intact so a retry is possible;
    // the error row is what tells them the first attempt did not stick.
    errorMsg.value = t('sessionTags.saveFailed')
  } finally {
    saving.value = false
  }
}

function close() {
  emit('close')
}

// Reset transient input and (re)load candidates each time the dialog opens.
//
// `immediate` matters: the parent may mount this component with `open` already
// true (or keep it mounted and flip the flag), and without it a dialog that is
// open at mount time would render an empty candidate list forever.
watch(() => props.open, (open) => {
  if (!open) return
  newTagName.value = ''
  newTagScope.value = 'project'
  selected.value = [...props.initialTags]
  load()
}, { immediate: true })
</script>

<style scoped>
/* The shared .modal-body is padding-free by design (modal-card.css) — it only
   provides the scroll container, so every dialog must inset its own content or
   it renders flush against the card edge. Matches the convention used by the
   other dialogs (e.g. .jump-dialog-body). */
.session-tags-dialog {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-6) var(--space-7);
}

.st-section-label {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary);
  margin-top: var(--space-2);
}

.st-empty {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  padding: var(--space-3) 0;
}

/* Same visual language as the other dialogs' error rows
   (e.g. .copy-agent-dialog__error). */
.st-error {
  font-size: var(--font-size-md);
  color: #e74c3c;
  padding: var(--space-4) var(--space-6);
  background: rgba(231, 76, 60, 0.1);
  border-radius: var(--radius-sm);
}

.st-candidate-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  max-height: 220px;
  overflow-y: auto;
}

.st-candidate {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  border: 1px solid transparent;
}

.st-candidate:hover {
  background: var(--bg-tertiary);
}

.st-candidate.selected {
  background: var(--bg-tertiary);
  border-color: var(--border-color);
}

.st-candidate-main {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 1;
  min-width: 0;
  cursor: pointer;
}

/* Custom-drawn checkbox: the native control renders at its own weight and
   corner radius, which reads as crude next to the pill chips. Same recipe as
   .form-checkbox in QuickCommandEditModal.vue, so both dialogs match.
   The checked colour stays the single accent rather than the tag's own colour:
   tag hues come from a hash palette and the pale ones (yellow, light green)
   would leave a white tick with almost no contrast. */
.st-checkbox {
  -webkit-appearance: none;
  appearance: none;
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  margin: 0;
  position: relative;
  /* --border-color is too faint against the dialog surface for an unchecked
     box to read as a control, so darken it toward the text colour. */
  border: 1.5px solid color-mix(in srgb, var(--text-muted) 60%, transparent);
  border-radius: var(--radius-xs, 3px);
  background: var(--bg-primary, #fff);
  cursor: pointer;
  transition: background var(--duration-base), border-color var(--duration-base);
}

.st-checkbox:checked {
  background: var(--accent-color, #0066cc);
  border-color: var(--accent-color, #0066cc);
}

/* The tick, drawn as two borders of a rotated box. Centred by transform rather
   than by hand-tuned offsets: absolute left/top values depend on the border
   width and box-sizing and drift as soon as either changes (the copied
   left:4px/top:1px put the tick 0.1px from the right edge).
   45% rather than 50% on the vertical axis: the rotated glyph's bounding box
   is taller than its visual mass, so 50% sits ~0.75px low. Measured on an 8x
   render, 44-46% all land within 0.25px of centred. */
.st-checkbox:checked::after {
  content: '';
  position: absolute;
  left: 50%;
  top: 45%;
  width: 4px;
  height: 8px;
  /* --st-tick is resolved per theme in JS (see checkboxTickColor): a fixed
     white sits at 1.69:1 on the lightest accents. */
  border: solid var(--st-tick, #fff);
  border-width: 0 2px 2px 0;
  transform: translate(-50%, -50%) rotate(45deg);
}

.st-checkbox:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 1px;
}

/* Unchecked rows dim slightly on hover so the row reads as one click target. */
.st-candidate:hover .st-checkbox:not(:checked) {
  border-color: var(--accent-color, #0066cc);
}

.st-scope-badge {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-2);
}

.st-delete-btn {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.st-delete-btn:hover {
  color: var(--color-red, #ef4444);
  background: var(--bg-secondary);
}

.st-add-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

/* Native controls carry no styling of their own here: the global reset zeroes
   padding/margin on every element (web/css/base.css), so an input left with
   only layout rules renders as bare text with no border or hit area. Every
   control below therefore declares its own surface, matching the convention
   used by other form dialogs (e.g. .form-input, .port-add-input). */
.st-input {
  flex: 1;
  min-width: 0;
  height: 32px;
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm);
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  font-family: inherit;
}

.st-input:focus {
  outline: none;
  border-color: var(--accent-color, #0066cc);
}

.st-input::placeholder {
  color: var(--text-muted);
}

.st-scope-select {
  flex-shrink: 0;
  height: 32px;
  padding: 0 var(--space-4);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: var(--radius-sm);
  background: var(--bg-primary, #fff);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  font-family: inherit;
  cursor: pointer;
}

.st-scope-select:focus {
  outline: none;
  border-color: var(--accent-color, #0066cc);
}

.st-add-btn {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: 32px;
  padding: 0 var(--space-6);
  /* Explicit border: this is a <button>, and without one the UA paints its own
     chrome, which reads as an unstyled element next to the input/select. */
  border: 1px solid var(--accent-color, #0066cc);
  border-radius: var(--radius-sm);
  background: var(--accent-color, #0066cc);
  color: #fff;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  transition: opacity var(--duration-base) ease;
}

.st-add-btn:hover:not(:disabled) {
  opacity: 0.88;
}

.st-add-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.st-scope-hint {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

/* ── Tag chip: accent comes from --tag-accent-{light,dark} set inline by
   tagAccentStyle(), so the same palette as tool calls is used and the color is
   stable per tag name. The theme picks which variable wins. ── */
.st-chip {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  padding: 1px var(--space-3);
  border-radius: 999px;
  font-size: var(--font-size-xs);
  --tag-accent: var(--tag-accent-light);
  color: var(--tag-accent);
  background: color-mix(in srgb, var(--tag-accent) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--tag-accent) 30%, transparent);
}

[data-theme-base="dark"] .st-chip {
  --tag-accent: var(--tag-accent-dark);
}

.st-chip-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
