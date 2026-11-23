<template>
  <!-- One card per project, in a seamless stack. The cards are the SAME
       component in both layouts: under the project's own rows (the default
       "strip") and as the whole content of the "other projects" tab. -->
  <div ref="rootRef" class="other-cards" data-other-zone>
    <div
      v-for="group in groups"
      :key="group.name"
      class="pcard"
      :class="{ 'is-expanded': expandedProject === group.name, 'is-idle': group.sessions.length === 0 }"
      :style="projectTintStyle(group.name)"
      :data-project="group.name"
    >
      <button
        class="pcard-head"
        :title="expandedProject === group.name ? t('session.collapseProject') : t('session.expandProject')"
        :aria-label="`${expandedProject === group.name ? t('session.collapseProject') : t('session.expandProject')} · ${group.displayName}`"
        :aria-expanded="expandedProject === group.name"
        @click.stop="toggleProject(group)"
      >
        <!-- Same mark as the app header's project segment: the card and the
             top bar name the same thing, so they read with the same glyph. -->
        <Projector :size="12" class="pcard-icon" aria-hidden="true" />
        <span class="pcard-name">{{ group.displayName }}</span>
        <!-- Active card: the count. Idle card: when it was last touched, in
             the same slot, so the header keeps one rhythm either way. -->
        <span v-if="group.sessions.length > 0" class="pcard-count">{{ cardCount(group) }}</span>
        <span v-else class="pcard-recent">{{ formatRelativeTime(group.lastActiveAt) }}</span>
        <span class="pcard-path" :title="group.name">{{ group.displayPath }}</span>
        <!-- Same slot in all three states, so nothing shifts while loading. -->
        <LoadingIndicator
          v-if="loadingProject === group.name"
          class="pcard-chev pcard-chev--busy"
          size="sm"
          inline
          aria-hidden="true"
        />
        <ChevronUp v-else-if="expandedProject === group.name" :size="14" class="pcard-chev" />
        <ChevronDown v-else :size="14" class="pcard-chev" />
      </button>

      <!-- An idle card has no rows to show until it is opened, and its
           session list is the only thing that ever appears — so nothing
           renders below the header while collapsed. -->
      <div v-if="expandedProject === group.name && expandedError" class="pcard-state">{{ expandedError }}</div>
      <div v-else-if="expandedProject === group.name && expandedSessions.length === 0" class="pcard-state">
        {{ t('session.noSessions') }}
      </div>

      <div
        v-for="session in cardRows(group)"
        :key="group.name + '/' + session.id"
        class="pcard-row"
        :data-session-id="session.id"
        @click="emit('select', session, group.name)"
      >
        <!-- One fixed slot for every state, so the titles of a running,
             blocked and unread row all start on the same x. Pending still
             outranks everything: the user is the blocker there. Running is
             a ring (not the project rows' bottom-edge comet) — these cards
             are compact and a travelling light along a 36dp row reads as
             noise. Unread keeps its dot, centred in the same slot. -->
        <span
          v-if="rowStatus(session) || session.running"
          class="pcard-row-status"
        >
          <span
            v-if="rowStatus(session) === 'pending'"
            class="pcard-row-pending"
            :title="statusLabel(session)"
            :aria-label="statusLabel(session)"
            role="img"
          ><ShieldAlert :size="14" /></span>
          <LoadingIndicator
            v-else-if="session.running"
            class="pcard-row-ring"
            size="sm"
            inline
            aria-hidden="true"
          />
          <span
            v-else
            class="pcard-row-dot"
            :title="statusLabel(session)"
            :aria-label="statusLabel(session)"
            role="img"
          />
        </span>
        <span class="pcard-row-title">{{ session.title }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronUp, Projector, ShieldAlert } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { formatRelativeTime } from '@/utils/format.ts'
import { projectTintStyle } from '@/utils/projectTint'
import { rowStatus, statusLabelKey } from '@/utils/sessionRowStatus'
import { store } from '@/stores/app.ts'
import { appLog } from '@/utils/appLog'

defineProps({
  /** Other-project groups, already ordered by `useCrossProjectSessions`. */
  groups: { type: Array, default: () => [] },
})

const emit = defineEmits(['select'])

const { t } = useI18n()
const rootRef = ref(null)

/** Tooltip for a card row's status slot. */
function statusLabel(session) {
  return t(statusLabelKey(rowStatus(session)))
}

// ── Expanding a project card in place ──────────────────────────────────────
//
// Clicking a card's header grows THAT card to show the project's full session
// list, in place, pushing the rows below it down. Nothing is covered and no
// separate view is opened, so the list keeps its scroll position and the other
// cards stay where they were.
//
// The project is passed explicitly to the API because the ordinary list
// endpoint resolves it from the cookie — using that one would mean switching
// chat, files and terminal just to read a list.
const expandedProject = ref('')
const expandedSessions = ref([])
const expandedError = ref('')
// Project whose list is being fetched. The card must NOT change size while
// this is set: showing a placeholder would push the rows around, then the real
// content would push them again (the flash). The header swaps its chevron for a
// ring in the same slot, and the card grows only once the data is here.
const loadingProject = ref('')
// Stale-response guard: the user can collapse (or expand another project) while
// a request is still in flight.
let expandGeneration = 0

/** Rows to render for a card: the full list when expanded, active ones otherwise. */
function cardRows(group) {
  return expandedProject.value === group.name ? expandedSessions.value : group.sessions
}

/** Header count: the full list once it is loaded, otherwise the active count. */
function cardCount(group) {
  const isOpen = expandedProject.value === group.name
  if (!isOpen || expandedError.value) return group.sessions.length
  return expandedSessions.value.length
}

function cardElementFor(projectPath) {
  const cards = rootRef.value?.querySelectorAll('.pcard') || []
  for (const el of cards) {
    if (el.dataset && el.dataset.project === projectPath) return el
  }
  return null
}

/**
 * Animate a card between its previous and current height.
 *
 * The card clips its content (`overflow: hidden`), so animating `height` from
 * the old value reads as the card unfolding rather than the page jumping. The
 * animation has no fill, so the element returns to its natural (auto) height
 * when it ends. Skipped where there is nothing to animate — no element, no
 * measurable heights (jsdom), or a host without the Web Animations API.
 */
function animateCardHeight(el, fromHeight) {
  if (!el || typeof el.animate !== 'function' || fromHeight <= 0) return
  const toHeight = el.getBoundingClientRect().height
  if (toHeight <= 0 || Math.abs(toHeight - fromHeight) < 1) return
  el.animate(
    [{ height: `${fromHeight}px` }, { height: `${toHeight}px` }],
    { duration: 180, easing: 'cubic-bezier(0.2, 0.8, 0.2, 1)' },
  )
}

function toggleProject(group) {
  if (loadingProject.value === group.name) return
  if (expandedProject.value === group.name) {
    collapseProject()
    return
  }
  void expandProject(group)
}

async function expandProject(group) {
  const el = cardElementFor(group.name)
  // Measured BEFORE anything changes: the card keeps exactly this height while
  // the request is in flight, so nothing moves under the pointer.
  const fromHeight = el ? el.getBoundingClientRect().height : 0
  loadingProject.value = group.name
  const generation = ++expandGeneration
  try {
    const resp = await fetch(`/api/ai/sessions/by-project?path=${encodeURIComponent(group.name)}`)
    if (generation !== expandGeneration) return
    if (!resp.ok) throw new Error(`by-project HTTP ${resp.status}`)
    const data = await resp.json()
    expandedSessions.value = Array.isArray(data.sessions) ? data.sessions : []
    expandedError.value = ''
  } catch (err) {
    if (generation === expandGeneration) {
      expandedSessions.value = []
      expandedError.value = t('session.browseFailed')
    }
    appLog.e('OtherProjectCards', 'failed to load project sessions', err)
  } finally {
    if (generation === expandGeneration) loadingProject.value = ''
  }
  if (generation !== expandGeneration) return
  // Swap in the loaded rows and unfold to the new height in one frame.
  expandedProject.value = group.name
  await nextTick()
  animateCardHeight(el, fromHeight)
}

function collapseProject() {
  const el = cardElementFor(expandedProject.value)
  const fromHeight = el ? el.getBoundingClientRect().height : 0
  // Bump the generation so a late response cannot repopulate the closed card.
  expandGeneration++
  loadingProject.value = ''
  expandedProject.value = ''
  expandedSessions.value = []
  expandedError.value = ''
  void nextTick(() => animateCardHeight(el, fromHeight))
}

// An expanded card describes the project we just left; it must not survive a
// project switch (the list now belongs to the new project).
watch(() => store.state.projectRoot, () => collapseProject())
</script>

<style scoped>
/* The cards themselves are layout-agnostic: no fixed height, no split, no
   heading of their own. The card edge is what separates "another project" from
   the user's own list; the tinted header says which project it is. */
.other-cards {
  display: flex;
  flex-direction: column;
  /* Flush stack: no padding, no gap. Cards are separated by a single hairline
     (the next card's top border), so a column of projects reads as one block
     rather than a pile of floating boxes. */
  gap: 0;
}

/* One card per project, in a seamless stack: no outer border, only the top
   hairline, so two adjacent cards share one divider instead of showing a
   double line. Everything inside a card is derived from the SAME project
   colour (`--pc`) — the header band is that colour mixed into the surface
   (opaque, so scrolling content never shows through it), the chips, row
   separators and hovers are the same colour at low alpha. Status keeps its own
   meaning: pending stays amber. */
.pcard {
  /* One colour drives the whole card. Both theme values arrive inline; the
     attribute selector below picks one, so switching themes needs no JS. */
  --pc: var(--pc-light);
  border: 1px solid color-mix(in srgb, var(--pc, var(--border-color)) 45%, var(--border-color));
  background: var(--bg-primary, #fff);
  overflow: hidden;
}

/* Dark themes get the re-lit palette: the light values are darkened for text
   use and read as mud on a dark surface. */
:root[data-theme-base="dark"] .pcard {
  --pc: var(--pc-dark);
}

/* Cards are flush, so their facing borders would show as a 2px double line.
   Pulling the next card up by one pixel makes the two edges share a single
   line instead. */
.pcard + .pcard {
  margin-top: calc(-1 * var(--dp));
}

/* The whole header is the entry point, so it is a button — no nested control.
   `border: 0` plus an explicit bottom border keeps it from inheriting the
   browser's default button frame. */
.pcard-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-3) var(--space-4);
  font: inherit;
  color: inherit;
  text-align: left;
  cursor: pointer;
  /* Opaque: mixed into the SURFACE, not into `transparent`, so the band keeps
     its colour while content scrolls underneath. */
  background: color-mix(in srgb, var(--pc, var(--text-primary)) 15%, var(--bg-primary, #fff));
  border: 0;
  border-bottom: 1px solid color-mix(in srgb, var(--pc, var(--border-color)) 30%, var(--border-color));
}

@media (hover: hover) {
  .pcard-head:hover {
    background: color-mix(in srgb, var(--pc, var(--text-primary)) 22%, var(--bg-primary, #fff));
  }
}

.pcard-name {
  flex: 0 0 auto;
  max-width: 55%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
}

/* The same glyph the app header's project segment uses — these cards name the
   same thing, so they must not read as a different kind of object. Tinted with
   the card's own colour so it belongs to the header band rather than floating
   above it. */
.pcard-icon {
  flex: 0 0 auto;
  color: var(--pc, var(--text-muted, #656d76));
}

.pcard-count {
  flex: 0 0 auto;
  /* Round badge, fixed size: the count is a single number (the session cap is
     two digits), so a circle keeps the header rhythm even and reads as a
     marker rather than a label. */
  width: calc(18 * var(--dp));
  height: calc(18 * var(--dp));
  display: inline-grid;
  place-items: center;
  border-radius: 50%;
  /* Semi-transparent version of the same project colour — it sits on the
     opaque header, so alpha reads as a tint rather than a separate colour. */
  background: color-mix(in srgb, var(--pc, var(--text-primary)) 16%, transparent);
  color: color-mix(in srgb, var(--pc, var(--text-primary)) 62%, var(--text-primary, #1a1a1a));
  font-size: var(--font-size-2xs);
  font-variant-numeric: tabular-nums;
}

/* Idle card (recently touched, nothing active): the count slot becomes a
   relative timestamp. It is text, not a badge, so it takes only the width it
   needs — a fixed circle would either clip "23 小时前" or leave a hole. */
.pcard-recent {
  flex: 0 0 auto;
  font-size: var(--font-size-2xs);
  color: var(--text-hint, #8c959f);
  white-space: nowrap;
}

/* The card expands downward, so the hint points down; the expanded header
   collapses back up, so it points up. The direction is the state. */
.pcard-chev {
  flex: 0 0 auto;
  color: var(--text-muted, #656d76);
}

.pcard-head:hover .pcard-chev {
  color: var(--pc, var(--accent-color, #0066cc));
}

/* The busy ring occupies the chevron's slot, so the header does not move while
   the project's list is being fetched. Same colour rules as the row ring: the
   shared spinner reads these two variables, and the card is monochrome. */
.pcard-chev--busy {
  width: calc(14 * var(--dp));
  height: calc(14 * var(--dp));
  --li-color: var(--pc, var(--accent-color, #0066cc));
  --li-track-color: color-mix(in srgb, var(--pc, var(--border-color)) 22%, transparent);
}

.pcard-path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: right;
  color: var(--text-hint, #8c959f);
  font-size: var(--font-size-2xs);
}

.pcard-row {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-height: calc(36 * var(--dp));
  padding: var(--space-3) var(--space-4) var(--space-3) var(--space-5);
  border-top: 1px solid color-mix(in srgb, var(--pc, var(--border-color)) 14%, transparent);
  cursor: pointer;
}

.pcard-row:first-of-type {
  border-top: 0;
}

@media (hover: hover) {
  .pcard-row:hover {
    background: color-mix(in srgb, var(--pc, var(--text-primary)) 9%, transparent);
  }
}

.pcard-row-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-sm);
  color: var(--text-secondary, #495057);
}

.pcard-row-ring {
  flex: 0 0 auto;
  /* The shared ring reads its colour from these two variables; pointing them at
     the project colour keeps the card monochrome. */
  --li-color: var(--pc, var(--accent-color, #0066cc));
  --li-track-color: color-mix(in srgb, var(--pc, var(--border-color)) 22%, transparent);
}

.pcard-row-status {
  flex: 0 0 auto;
  /* One slot width for the ring, the shield and the dot: without it the title
     would start 8dp further left on unread rows than on running ones. */
  width: calc(14 * var(--dp));
  height: calc(14 * var(--dp));
  display: flex;
  align-items: center;
  justify-content: center;
}

.pcard-row-dot {
  flex: 0 0 auto;
  width: calc(6 * var(--dp));
  height: calc(6 * var(--dp));
  border-radius: 50%;
  /* Same colour as the card: unread is read from the dot's presence, not from
     a second hue competing with the project's. */
  background: var(--pc, var(--accent-color, #0066cc));
}

.pcard-row-pending {
  flex: 0 0 auto;
  display: inline-flex;
  color: var(--color-yellow, #a16207);
}

/* A card that is expanded in place: its rows replace the active-only list and
   the card simply gets taller, pushing the rows below it down. */
.pcard.is-expanded {
  background: var(--bg-primary, #fff);
}

/* An idle card (recently touched, nothing active) is nothing but its header
   until opened, so the header's bottom border would sit right on the card's
   own bottom border and read as a double line. Drop it while collapsed — when
   the card is open the border is the header/rows divider again and must stay. */
.pcard.is-idle:not(.is-expanded) .pcard-head {
  border-bottom: 0;
}

/* A collapsed idle card greys out: "this project has nothing going on right
   now" should be visible at a glance, not inferred from "the slot shows a
   timestamp instead of a count". `--pc` drives the band, the icon, the border
   and the hover tint, so overriding it here desaturates the card in one move.
   The dark rule has to be repeated: `:root[data-theme-base="dark"] .pcard`
   re-points `--pc` at the dark palette and outranks a plain `.pcard.is-idle`. */
.pcard.is-idle:not(.is-expanded) {
  --pc: var(--text-muted, #6c757d);
}

:root[data-theme-base="dark"] .pcard.is-idle:not(.is-expanded) {
  --pc: var(--text-muted, #6e7681);
}

/* The name stays one step above the hint greys. 12px at the hint token falls
   under the 4.5:1 the rest of this list keeps itself to, and the card is still
   a thing you can open and read. */
.pcard.is-idle:not(.is-expanded) .pcard-name {
  color: var(--text-secondary, #495057);
}

.pcard.is-idle:not(.is-expanded) .pcard-icon,
.pcard.is-idle:not(.is-expanded) .pcard-path,
.pcard.is-idle:not(.is-expanded) .pcard-recent,
.pcard.is-idle:not(.is-expanded) .pcard-chev {
  color: var(--text-hint, #8c959f);
  opacity: 0.85;
}

.pcard-state {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-5) var(--space-4);
  color: var(--text-hint, #8c959f);
  font-size: var(--font-size-2xs);
}
</style>
