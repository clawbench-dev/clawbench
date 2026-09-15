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
          :class="{ selected: isSelected(tag.name), pending: tag.pending }"
        >
          <!-- The chip itself is the toggle. It replaced a checkbox + label pair:
               the checkbox duplicated what the chip already said, and the filled
               style below states the selection on its own. -->
          <button
            type="button"
            class="st-chip"
            :class="{ active: isSelected(tag.name) }"
            :style="chipStyle(tag.name)"
            :aria-pressed="isSelected(tag.name)"
            :title="tag.name"
            @click="toggleTag(tag.name)"
          >
            <!-- role="img" is required for the label to be announced: a bare
                 <svg> is exposed as a generic graphic and its aria-label is
                 dropped, so screen readers would announce an unnamed icon.
                 Same pattern as AgentIcon.vue. -->
            <Globe
              v-if="tag.scope === 'global'"
              :size="11"
              class="st-chip-scope"
              role="img"
              :aria-label="t('sessionTags.scopeGlobal')"
            />
            <span class="st-chip-name">{{ tag.name }}</span>
          </button>
          <!-- Deleting removes the label from every session; confirm first.
               A SIBLING of the chip, not a child: a <button> inside a <button> is
               invalid HTML and the parser would break the nesting. It is
               positioned over the chip's right edge so the two never fight for
               width, and stays permanently visible rather than appearing on
               hover — a hover-only control is undiscoverable and does not exist
               on touch at all. -->
          <button
            class="st-delete-btn"
            type="button"
            :title="t('sessionTags.deleteTag')"
            :aria-label="t('sessionTags.deleteTag')"
            @click.stop="requestDelete(tag)"
          >
            <Trash2 :size="12" />
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
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Globe, Plus, Trash2 } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { useDialog } from '@/composables/useDialog.ts'
import { apiGet, apiDelete, apiPatch } from '@/utils/api.ts'
import { tagAccent, readableTextOn } from '@/utils/tagColor.ts'
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
 * Inline style for a chip: the per-theme accent, plus — when the chip is filled
 * (selected) — the label colour that stays readable on that fill.
 *
 * The fill colour is the tag's own accent, so the label must be chosen per tag.
 * `readableTextOn` picks the better of black/white by WCAG luminance, which is
 * what the hardcoded per-theme rule in SessionTagFilterBar cannot do: several
 * palette entries are dark enough that white looks right but scores below
 * 4.5:1. Tag accents come from TAG_PALETTE as literal hex, so this needs no
 * getComputedStyle / theme watcher — the light and dark variants are handed to
 * CSS and it picks one via [data-theme-base], the same way the border colour
 * already worked.
 */
function chipStyle(name) {
  const { light, dark } = tagAccent(name)
  return {
    '--tag-accent-light': light,
    '--tag-accent-dark': dark,
    '--st-chip-text-light': readableTextOn(light),
    '--st-chip-text-dark': readableTextOn(dark),
  }
}

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

/* Chips flow at their natural width and wrap onto the next line when the row
   fills up. An even-width grid (minmax(120px, 1fr)) was tried first and
   rejected: it stretched short tags like "bug" out to a fixed 120px, so every
   chip was mostly empty padding and the row read as a table of boxes rather
   than a cluster of labels. Sizing to content keeps the pill shape proportional
   to its text, which is what the filter bar does too. */
.st-candidate-list {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  max-height: 220px;
  overflow-y: auto;
}

/* The cell shrink-wraps its chip (inline-flex, so it takes the chip's width
   rather than a column's) and is a positioning context for the overlaid delete
   button. It carries no surface of its own: with the chip stating the selection
   by fill, a tinted cell would be a second, differently-shaped block around it.
   max-width:100% keeps an over-long name from pushing the chip past the row —
   the name span ellipsises instead. */
.st-candidate {
  position: relative;
  display: inline-flex;
  max-width: 100%;
  min-width: 0;
}

/* The chip is the toggle. A <button> so it is keyboard-reachable and announces
   pressed state; the checkbox it replaced provided both for free. */
.st-chip {
  --tag-accent: var(--tag-accent-light);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  /* No flex-grow: the chip is only as wide as its label needs, so the cell that
     shrink-wraps it ends up content-sized too. */
  min-width: 0;
  /* Room for the overlaid delete button, which is permanently visible and sits
     at right:3px with an 18px width — i.e. its left edge is 21px in from the
     chip's right border. Absolute offsets are measured from the padding box, so
     padding-right must clear 21px or a long tag name ellipsises underneath the
     icon. --space-8 (20px) is a pixel short, hence the +4px of slack. */
  padding: 4px calc(var(--space-8) + 4px) 4px var(--space-3);
  /* Reset UA button chrome: without an explicit border and background the
     browser paints its own. */
  border: 1px solid color-mix(in srgb, var(--tag-accent) 30%, transparent);
  border-radius: 999px;
  background: color-mix(in srgb, var(--tag-accent) 12%, transparent);
  color: var(--tag-accent);
  font-size: var(--font-size-xs);
  font-family: inherit;
  line-height: 16px;
  cursor: pointer;
  transition: background var(--duration-fast, 120ms) ease, color var(--duration-fast, 120ms) ease;
}

/* Selected = filled with the tag's own accent, matching the filter bar's active
   chip so the same tag looks the same in both places.
 *
 * The label colour is resolved per theme from --st-chip-text-{light,dark}, set
 * inline by chipStyle() via readableTextOn. A hardcoded #fff would sit as low as
 * 1.67:1 on the light-theme palette entries (see tagColor.ts). */
.st-chip.active {
  background: var(--tag-accent);
  border-color: var(--tag-accent);
  color: var(--st-chip-text-light);
}

[data-theme-base="dark"] .st-chip {
  --tag-accent: var(--tag-accent-dark);
}

[data-theme-base="dark"] .st-chip.active {
  color: var(--st-chip-text-dark);
}

/* Hover on an unselected chip deepens its own tint — the same recipe the filter
   bar uses. The cell stays transparent so the only thing that reacts is the
   pill the pointer is actually over. */
.st-chip:hover:not(.active) {
  background: color-mix(in srgb, var(--tag-accent) 22%, transparent);
}

.st-chip:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 1px;
}

/* A tag typed in this dialog but not yet saved. Dashed rather than a colour or
   badge: it needs no width and reuses the chip's own border. It also explains
   why deleting this one skips the confirmation — there is nothing server-side
   to destroy yet. */
.st-candidate.pending .st-chip {
  border-style: dashed;
}

.st-chip-scope {
  flex-shrink: 0;
  /* Inherits the chip's label colour so it stays readable on both the outlined
     and the filled state. */
  opacity: 0.75;
}

.st-chip-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Overlaid on the chip's right edge, vertically centred, and always visible.
 *
 * It is a sibling of the chip (nesting buttons is invalid), so it must be
 * positioned rather than laid out, and @click.stop keeps a tap on it from also
 * toggling the chip. Always shown rather than revealed on hover: a control that
 * only appears on hover is undiscoverable, and on touch there is no hover at
 * all. The chip reserves room for it via its right padding, so it never covers
 * the label. */
.st-delete-btn {
  position: absolute;
  right: 3px;
  top: 50%;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: color var(--duration-fast, 120ms) ease, background var(--duration-fast, 120ms) ease;
}

.st-delete-btn:hover {
  color: var(--color-red, #ef4444);
  background: var(--bg-tertiary);
}

.st-delete-btn:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 1px;
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
</style>
