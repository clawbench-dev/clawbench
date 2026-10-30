<template>
  <BottomSheet :open="drawer.effectiveOpen.value" auto size="wide" @close="drawer.close()">
    <template #header>
      <!-- ═══ List view ═══ -->
      <template v-if="!drillCluster">
        <SparklesIcon :size="16" class="bs-header-icon" />
        <span class="bs-header-title">{{ t('chat.messageClusters.title') }}</span>
        <span class="bs-header-actions">
          <RefreshButton v-if="loaded && clusters.length > 0 && !computing && progress.status !== 'error'" :size="16" class="mc-header-btn" :loading="starting" :disabled="starting" :title="t('chat.messageClusters.reanalyze')" @click.stop="handleStartCompute" />
        </span>
      </template>
      <!-- ═══ Drill-down view ═══
           The variants are a second PAGE of this sheet, not a nested dialog:
           they belong to the cluster you just tapped, and a stacked modal both
           hides the list you were reading and adds a second close affordance.
           Same shape as SessionSearchDrawer / GitHistoryDrawer: the header swaps
           in a back button and the body swaps wholesale. -->
      <template v-else>
        <button class="mc-back-btn" :title="t('common.back')" :aria-label="t('common.back')" @click.stop="closeDrill">
          <ChevronLeft :size="18" />
        </button>
        <span class="bs-header-title mc-detail-title">{{ drillCluster.representative }}</span>
        <span class="mc-detail-count">{{ drillCluster.total_count }}</span>
      </template>
    </template>

    <div class="mc-content">
      <!-- ═══════════ Drill-down: variants of one cluster ═══════════ -->
      <template v-if="drillCluster">
        <div class="mc-detail">
          <div class="mc-detail-bar">
            <span class="mc-figure">
              <MessageSquareTextIcon :size="13" class="mc-figure-icon" />
              <b>{{ drillCluster.total_count }}</b>
              <em>{{ t('chat.messageClusters.statsMessages') }}</em>
            </span>
            <span class="mc-figure-sep" />
            <span class="mc-figure">
              <ListTreeIcon :size="13" class="mc-figure-icon" />
              <b>{{ drillVariants.length }}</b>
              <em>{{ t('chat.messageClusters.statsPhrasings') }}</em>
            </span>
            <span class="mc-detail-hint">{{ t('chat.messageClusters.variantsHint') }}</span>
          </div>

          <div class="mc-variant-list">
            <!-- Two SECTIONS, not one flat list. The backend stores the variants
                 as plain strings with no per-variant counts, so a per-row figure
                 would have to be invented — the honest hierarchy is structural:
                 the representative stands alone, the rest are grouped under a
                 counted heading. -->
            <div class="mc-variant-section">
              <div class="mc-variant-section-head">
                <StarIcon :size="11" class="mc-variant-section-icon" />
                <span>{{ t('chat.messageClusters.representative') }}</span>
                <span v-if="representativeShare !== null" class="mc-variant-section-meta">{{ t('chat.messageClusters.representativeShare', { pct: representativeShare }) }}</span>
              </div>
              <div class="mc-variant-row is-representative">
                <span class="mc-variant-index">01</span>
                <span class="mc-variant-main">
                  <span class="mc-variant-text">{{ drillCluster.representative }}</span>
                </span>
                <button class="mc-btn add" @click="addVariantToQuickSend(drillCluster.representative)">
                  <PlusIcon :size="12" />
                  {{ t('chat.messageClusters.add') }}
                </button>
              </div>
            </div>

            <div v-if="otherVariants.length" class="mc-variant-section">
              <div class="mc-variant-section-head">
                <ListTreeIcon :size="11" class="mc-variant-section-icon" />
                <span>{{ t('chat.messageClusters.otherVariants') }}</span>
                <span class="mc-variant-section-meta">{{ t('chat.messageClusters.variantsCount', { count: otherVariants.length }) }}</span>
              </div>
              <div
                v-for="(v, i) in otherVariants"
                :key="v"
                class="mc-variant-row"
              >
                <span class="mc-variant-index">{{ String(i + 2).padStart(2, '0') }}</span>
                <span class="mc-variant-main">
                  <span class="mc-variant-text">{{ v }}</span>
                </span>
                <button class="mc-btn add" @click="addVariantToQuickSend(v)">
                  <PlusIcon :size="12" />
                  {{ t('chat.messageClusters.add') }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </template>

      <!-- ═══════════ List view: states ═══════════ -->
      <template v-else>
        <!-- Computing -->
        <div v-if="computing" class="mc-computing">
          <div class="mc-progress-bar">
            <div class="mc-progress-fill" :style="{ width: progressPercent + '%' }" />
          </div>
          <div class="mc-computing-info">
            <span class="mc-phase-text">{{ phaseText }}</span>
            <span class="mc-progress-detail">{{ Math.round(progressPercent) }}% · {{ elapsedText }}</span>
          </div>
        </div>

        <!-- Error -->
        <div v-else-if="progress.status === 'error'" class="mc-error">
          <span class="mc-error-icon"><CircleAlert :size="18" /></span>
          <span class="mc-error-text">{{ t('chat.messageClusters.error') }}</span>
          <button class="mc-btn" @click="handleStartCompute">{{ t('chat.messageClusters.retry') }}</button>
        </div>

        <!-- Loading -->
        <PanelSkeleton v-else-if="loading" variant="list" :rows="8" :meta="true" :label="t('chat.messageClusters.loading')" />

        <!-- No cache -->
        <div v-else-if="!loaded || clusters.length === 0 && progress.status === 'idle'" class="mc-empty">
          <span class="mc-empty-icon"><SparklesIcon :size="24" /></span>
          <span class="mc-empty-title">{{ t('chat.messageClusters.noCache') }}</span>
          <span class="mc-empty-hint">{{ t('chat.messageClusters.noCacheHint') }}</span>
          <button class="mc-btn primary" @click="handleStartCompute"><PlayIcon :size="14" />{{ t('chat.messageClusters.firstAnalyze') }}</button>
        </div>

        <!-- Results -->
        <div v-else class="mc-results">
          <!-- One-line figures bar. A strip, not a card: the sheet is already a
               surface, so a panel here would be a third nesting level. Each
               figure carries its own glyph so the three facts are separable at
               a glance instead of reading as one run of numbers. -->
          <div class="mc-summary">
            <span class="mc-figure">
              <LayersIcon :size="13" class="mc-figure-icon" />
              <b>{{ clusters.length }}</b>
              <em>{{ t('chat.messageClusters.statsGroups') }}</em>
            </span>
            <span class="mc-figure-sep" />
            <span class="mc-figure">
              <MessageSquareTextIcon :size="13" class="mc-figure-icon" />
              <b>{{ totalMessages }}</b>
              <em>{{ t('chat.messageClusters.statsMessages') }}</em>
            </span>
            <span class="mc-summary-meta">
              <span v-if="localizedMode" class="mc-meta-item">
                <CpuIcon :size="11" class="mc-meta-icon" />{{ localizedMode }}
              </span>
              <span v-if="updatedText" class="mc-meta-item">
                <ClockIcon :size="11" class="mc-meta-icon" />{{ updatedText }}
              </span>
            </span>
          </div>

          <!-- Scroller carries NO padding: the grid owns its inset, so the bar
               sits flush against the sheet edge. -->
          <div class="mc-cluster-list">
            <div class="mc-cluster-grid">
              <button
                v-for="(cluster, idx) in clusters"
                :key="cluster.id"
                type="button"
                class="mc-cluster-item"
                :class="{ 'is-static': !hasVariants(cluster), 'is-top': idx < 3 }"
                :disabled="!hasVariants(cluster)"
                @click="openDrill(cluster)"
              >
                <span class="mc-cluster-rank">{{ String(idx + 1).padStart(2, '0') }}</span>
                <span class="mc-cluster-representative">{{ cluster.representative }}</span>
                <span class="mc-cluster-sub">
                  <span v-if="variantCount(cluster) > 1" class="mc-cluster-variants">
                    <ListTreeIcon :size="10" class="mc-cluster-sub-icon" />{{ variantCount(cluster) }}<i>{{ t('chat.messageClusters.unitPhrasings') }}</i>
                  </span>
                  <span class="mc-cluster-count">{{ cluster.total_count }}<i>{{ t('chat.messageClusters.unitMessages') }}</i></span>
                </span>
                <ChevronRight v-if="hasVariants(cluster)" :size="13" class="mc-cluster-chevron" />
                <!-- Frequency meter, pinned to the row's bottom edge so it doubles
                     as the separator: one element carries both the magnitude and
                     the row boundary, which is what keeps a single-column list
                     from turning into a stack of boxes. -->
                <span class="mc-cluster-meter" aria-hidden="true">
                  <span class="mc-cluster-meter-fill" :style="{ width: clusterShare(cluster) + '%' }" />
                </span>
              </button>
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- QuickSend edit modal (pre-filled add mode) -->
    <QuickSendEditModal
      :open="quickSendEditOpen"
      :editing-item="null"
      :initial-values="quickSendInitialValues"
      @close="quickSendEditOpen = false; quickSendInitialValues = undefined"
      @saved="onQuickSendSaved"
    />
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Sparkles as SparklesIcon,
  Plus as PlusIcon,
  Play as PlayIcon,
  ChevronRight,
  ChevronLeft,
  CircleAlert,
  Layers as LayersIcon,
  MessageSquareText as MessageSquareTextIcon,
  Cpu as CpuIcon,
  Clock as ClockIcon,
  Star as StarIcon,
  ListTree as ListTreeIcon,
} from 'lucide-vue-next'
import RefreshButton from '@/components/common/RefreshButton.vue'
import BottomSheet from '@/components/common/BottomSheet.vue'
import QuickSendEditModal from '@/components/chat/QuickSendEditModal.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import { useMessageClusters, type MessageCluster } from '@/composables/useMessageClusters'
import { useToast } from '@/composables/useToast'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import { formatDuration, formatRelativeTime } from '@/utils/format'

const { t } = useI18n()
const toast = useToast()
const { clusters, loaded, loading, computing, progress, mode, updatedAt, fetchClusters, startCompute: rawStartCompute, syncProgressOnce } = useMessageClusters()

// ── QuickSend edit modal ──
const quickSendEditOpen = ref(false)
const quickSendInitialValues = ref<{ label: string; command: string } | undefined>(undefined)

// Reanalyze-button spin feedback while the compute request is in flight
const starting = ref(false)

// ── Drill-down: variants of one cluster ──
const drillCluster = ref<MessageCluster | null>(null)
const drillVariants = computed(() => drillCluster.value?.variants ?? [])

/** Every phrasing except the representative. The backend guarantees the
 *  representative is one of its own variants, but the filter does not rely on
 *  it — a representative that somehow is not in the list still renders once. */
const otherVariants = computed(() =>
  drillVariants.value.filter((v) => v !== drillCluster.value?.representative),
)

/** How much of the cluster the representative phrasing accounts for. Only the
 *  representative has a server-side exact-match count, so this is the one place
 *  a per-phrasing number exists. Rounded, and omitted when the count is absent
 *  rather than rendered as a misleading 0%. */
const representativeShare = computed<number | null>(() => {
  const c = drillCluster.value
  if (!c || !c.total_count || !c.representative_count) return null
  return Math.round((c.representative_count / c.total_count) * 100)
})

function openDrill(cluster: MessageCluster) {
  if (!hasVariants(cluster)) return
  drillCluster.value = cluster
}

function closeDrill() {
  drillCluster.value = null
}

// Back (Android hardware key / edge swipe) pops one level before closing the
// sheet. Priority must exceed BottomSheet's own `PRIORITY_OVERLAY + seq*0.001`
// handler, which registers later and would otherwise win the tie.
const unregisterDrillBack = registerBackHandler({
  id: 'message-clusters-drill',
  priority: PRIORITY_OVERLAY + 1,
  canGoBack: () => drillCluster.value !== null,
  goBack: closeDrill,
})
onBeforeUnmount(unregisterDrillBack)

// ── Tab binding (useTabDrawer) ──
// BottomSheet is teleported to <body>, so it survives v-show tab-panel hiding
// and would remain visible when switching to other tabs (file manager, etc.).
// useTabDrawer('chat', { autoRestore: false }) ensures:
//   1. effectiveOpen = false when activeTab !== 'chat' (drawer auto-hides)
//   2. autoRestore: false — won't auto-reopen when switching back to chat tab
//      (it's a recommendation picker, not a persistent panel)
// IMPORTANT: All BottomSheet-based drawers on the chat tab MUST use useTabDrawer
// to stay hidden on non-chat tabs. See useTabDrawer.ts for details.
const drawer = useTabDrawer('chat', { autoRestore: false })

// Reset the drill level on close so reopening starts at the list, never on a
// cluster whose data may have been re-analyzed in the meantime.
watch(() => drawer.effectiveOpen.value, (isOpen) => {
  if (!isOpen) drillCluster.value = null
})

const progressPercent = computed(() => {
  // Use server-provided progress_pct when available (fine-grained clustering progress)
  if (progress.value.progress_pct > 0) {
    // Map phase + pct to overall: extracting=0-20%, clustering=20-80%, saving=80-90%, done=100%
    const phase = progress.value.phase
    const pct = progress.value.progress_pct
    if (phase === 'extracting') return pct * 20 / 100
    if (phase === 'clustering') return 20 + pct * 60 / 100
    if (phase === 'saving') return 80 + pct * 10 / 100
    return pct
  }
  // Fallback: no fine-grained progress, use phase-based estimates
  const phase = progress.value.phase
  const status = progress.value.status
  if (status === 'done') return 100
  if (phase === 'extracting') return 10
  if (phase === 'clustering') return 50
  if (phase === 'saving') return 90
  return 0
})

const localizedMode = computed(() => {
  const m = mode.value
  if (!m) return ''
  const key = `chat.messageClusters.mode_${m}`
  // If i18n has the key, use it; otherwise fall back to raw value
  return t(key) !== key ? t(key) : m
})

/** Total messages covered by the visible clusters — the "how much was analyzed"
 *  figure that a per-row count alone cannot convey. */
const totalMessages = computed(() => clusters.value.reduce((sum, c) => sum + (c.total_count || 0), 0))

/** Largest cluster, used as the meter's 100% reference. Never 0 (the list is
 *  non-empty when this is rendered), but guarded so a divide cannot produce
 *  NaN widths. */
const maxCount = computed(() => Math.max(1, ...clusters.value.map((c) => c.total_count || 0)))

function clusterShare(cluster: MessageCluster): number {
  return Math.round(((cluster.total_count || 0) / maxCount.value) * 100)
}

/** Cache age, relative: "3 hours ago". Absolute timestamps read as noise here —
 *  the only question this line answers is "is this fresh?". */
const updatedText = computed(() => formatRelativeTime(updatedAt.value))

function variantCount(cluster: MessageCluster): number {
  return cluster.variants?.length ?? 0
}

/** Whether the card opens the drill-down. "Has at least one variant" rather than
 *  "has more than one": a single-variant cluster still has a phrasing worth
 *  adding to quick send. The disabled branch stays as a guard for a cluster the
 *  backend could not expand at all. */
function hasVariants(cluster: MessageCluster): boolean {
  return variantCount(cluster) > 0
}

const phaseText = computed(() => {
  const p = progress.value
  const phase = p.phase
  if (phase === 'extracting') return t('chat.messageClusters.phase_extracting', { msgCount: p.msg_count })
  if (phase === 'clustering') return t('chat.messageClusters.phase_clustering')
  if (phase === 'saving') return t('chat.messageClusters.phase_saving')
  return t('chat.messageClusters.computing')
})

const elapsedText = computed(() => {
  return formatDuration(progress.value.elapsed_ms)
})

async function open() {
  drawer.open()
  await fetchClusters()
  // Sync with server state once to detect ongoing computation
  await syncProgressOnce()
}

async function handleStartCompute() {
  if (starting.value) return
  starting.value = true
  try {
    const result = await rawStartCompute()
    if (result === 'already_running') {
      toast.show(t('chat.messageClusters.computing'), { icon: 'pending', type: 'info' })
    } else if (result === 'error') {
      toast.show(t('chat.messageClusters.error'), { type: 'error' })
    }
  } finally {
    // The button is hidden by `v-if="!computing"` once compute kicks in, so the
    // spin only covers the request-in-flight window (real duration, no fixed min).
    starting.value = false
  }
}

function addVariantToQuickSend(variant: string) {
  quickSendInitialValues.value = { label: drillCluster.value?.representative ?? '', command: variant }
  quickSendEditOpen.value = true
}

async function onQuickSendSaved() {
  quickSendEditOpen.value = false
  quickSendInitialValues.value = undefined
  await fetchClusters()
}

defineExpose({ open, openDrill, closeDrill, drillCluster, representativeShare })
</script>

<style>
/* ── Shell ──
   The body carries NO inline padding. Every scrollable list below owns its own
   horizontal inset on the CONTENT, so the scrollbar sits flush against the
   sheet edge. The old `.mc-content { padding: 12px }` pushed the scroller (and
   with it the bar) 12px inside the panel, which read as a bar floating in the
   middle of the sheet — the reported bug. Same convention as `.qs-list` /
   `.panel-list`, which never padded their scrollers either. */
.mc-content {
  display: flex;
  flex-direction: column;
  /* 16dp is `.mc-cluster-item`'s inset (`var(--space-7)`) — the rows the
     placeholder previews. */
  --panel-skeleton-inset: calc(16 * var(--dp));
  /* flex:1 + min-height:0 (not height:100%): the sheet runs in `auto` height
     mode, so a percentage height resolves against an auto-sized parent and the
     inner list never becomes the scroller — the body would scroll instead and
     the bar would land outside this element. */
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

/* ── Back button (drill-down header) ──
   Mirrors `.detail-back-btn` in SessionSearchDrawer: 28px, accent glyph, no
   chrome until hovered. */
.mc-back-btn {
  flex-shrink: 0;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: none;
  border-radius: var(--radius-xs);
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .mc-back-btn:hover {
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  }
}

.mc-detail-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Header count: mono tabular, no pill — the header already carries a glyph and
   a title, and a third chip-shaped element up there reads as noise. */
.mc-detail-count {
  flex-shrink: 0;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-bold);
  color: var(--text-secondary);
}

/* ── Figure (shared by the list's summary strip and the drill-down bar) ──
   icon · big number · small unit. Three distinct type sizes on one baseline is
   what makes a run of numbers scannable: the eye lands on the figure, the icon
   says which fact it is, the unit says how to read it. */
.mc-figure {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  flex-shrink: 0;
  white-space: nowrap;
}

.mc-figure-icon {
  flex-shrink: 0;
  color: var(--text-hint);
}

.mc-figure b {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--font-size-xl);
  font-weight: var(--font-weight-bold);
  color: var(--text-primary);
  line-height: 1;
}

.mc-figure em {
  font-style: normal;
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
}

/* Vertical hairline between figures — the "hard" separator that a gap alone
   cannot express. */
.mc-figure-sep {
  width: 1px;
  height: calc(11 * var(--dp));
  background: color-mix(in srgb, var(--text-primary) 20%, transparent);
  flex-shrink: 0;
}

/* ── Computing ── */
.mc-computing {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6) var(--space-7) var(--space-5);
}

.mc-computing-info {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-5);
}

.mc-progress-bar {
  height: calc(4 * var(--dp));
  background: color-mix(in srgb, var(--text-primary) 10%, transparent);
  border-radius: var(--radius-xs);
  overflow: hidden;
}

.mc-progress-fill {
  height: 100%;
  background: var(--accent-color, #0066cc);
  transition: width 0.3s ease;
}

.mc-phase-text {
  font-size: var(--font-size-md);
  color: var(--text-secondary, #666);
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mc-progress-detail {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
}

/* ── Error ── */
.mc-error {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  padding: var(--space-6) var(--space-7);
  color: var(--color-red, #e53e3e);
  font-size: var(--font-size-md);
}

.mc-error-icon {
  flex-shrink: 0;
  display: inline-flex;
}

.mc-error-text {
  flex: 1;
  min-width: 0;
}

/* ── Empty ── */
.mc-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  flex: 1;
  min-height: calc(200 * var(--dp));
  padding: calc(24 * var(--dp)) calc(28 * var(--dp));
  text-align: center;
}

.mc-empty-icon {
  width: calc(52 * var(--dp));
  height: calc(52 * var(--dp));
  border-radius: var(--radius-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--text-primary) 8%, transparent);
  color: var(--text-muted);
  margin-bottom: var(--space-2);
}

.mc-empty-title {
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary, #495057);
}

.mc-empty-hint {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: var(--line-height-normal);
  max-width: calc(280 * var(--dp));
  margin-bottom: var(--space-3);
}

/* ── Results ── */
.mc-results {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

/* Figures strip: one line, hairline below, no card. */
.mc-summary {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  padding: var(--space-3) var(--space-7);
  border-bottom: 1px solid color-mix(in srgb, var(--text-primary) 12%, transparent);
  flex-shrink: 0;
}

.mc-summary-meta {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-5);
  min-width: 0;
}

.mc-meta-item {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mc-meta-icon {
  flex-shrink: 0;
  color: var(--text-hint);
}

/* The scroller: full-bleed, so the themed bar hugs the sheet edge. */
.mc-cluster-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

/* ONE column. The list is a ranking, and a ranking reads top-to-bottom: a grid
   splits the sequence into rows you have to scan left-to-right as well, and the
   four columns were narrow enough to truncate most titles anyway. Each row is a
   single horizontal band — rank · title · counts · chevron — with the meter
   pinned to its bottom edge.
   No outer padding: the rows own their inset, so the scrollbar stays flush with
   the sheet edge (see .mc-content). */
.mc-cluster-grid {
  display: flex;
  flex-direction: column;
  padding-bottom: var(--space-2);
}

/* Rows are <button> (they open the drill-down) — reset the UA chrome.
   No border and no radius: in a single column the meter already separates one
   row from the next, so a box around every row would be a third horizontal line
   competing with it. */
.mc-cluster-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--space-5);
  width: 100%;
  min-width: 0;
  padding: calc(10 * var(--dp)) var(--space-7);
  border: none;
  background: none;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background var(--duration-fast) ease;
}

@media (hover: hover) {
  .mc-cluster-item:not(.is-static):hover {
    background: color-mix(in srgb, var(--text-primary) 5%, transparent);
  }
}

.mc-cluster-item:not(.is-static):active {
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
}

.mc-cluster-item:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: -2px;
}

/* Nothing to expand: no hover, no pointer. */
.mc-cluster-item.is-static {
  cursor: default;
}

.mc-cluster-rank {
  flex-shrink: 0;
  width: calc(20 * var(--dp));
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-bold);
  color: var(--text-hint);
  letter-spacing: calc(0.3 * var(--dp));
}

/* Top three by frequency: accent rank. The meter below carries the same
   emphasis, so the pair reads as one signal rather than two. */
.mc-cluster-item.is-top:not(.is-static) .mc-cluster-rank {
  color: var(--accent-color);
}

.mc-cluster-chevron {
  flex-shrink: 0;
  color: var(--text-hint);
}

.mc-cluster-representative {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  line-height: var(--line-height-snug);
  color: var(--text-primary);
}

/* Counts sit at a fixed right edge so the column of numbers can be scanned
   independently of the titles. */
.mc-cluster-sub {
  flex-shrink: 0;
  display: flex;
  align-items: baseline;
  justify-content: flex-end;
  gap: var(--space-4);
  min-width: calc(96 * var(--dp));
  color: var(--text-secondary);
}

/* Frequency meter: 2px, pinned to the row's bottom edge so it doubles as the
   separator. `inset-inline` keeps it clear of the panel's own edge. */
.mc-cluster-meter {
  position: absolute;
  left: var(--space-7);
  right: var(--space-7);
  bottom: 0;
  height: calc(2 * var(--dp));
  background: color-mix(in srgb, var(--text-primary) 8%, transparent);
  overflow: hidden;
}

.mc-cluster-meter-fill {
  display: block;
  height: 100%;
  background: color-mix(in srgb, var(--accent-color) 45%, transparent);
}

.mc-cluster-item.is-top:not(.is-static) .mc-cluster-meter-fill {
  background: var(--accent-color);
}

.mc-cluster-count {
  display: inline-flex;
  align-items: baseline;
  gap: calc(2 * var(--dp));
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-bold);
  color: var(--text-primary);
  line-height: 1;
}

.mc-cluster-count i {
  font-style: normal;
  font-family: inherit;
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-normal);
  color: var(--text-muted);
}

/* The phrasing count renders at the SAME type as the message count — same
   family, size, weight and colour — so the pair reads as one measurement in two
   units rather than a headline with a footnote. Only the unit stays small. */
.mc-cluster-variants {
  display: inline-flex;
  align-items: baseline;
  gap: calc(2 * var(--dp));
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-bold);
  color: var(--text-primary);
  line-height: 1;
  white-space: nowrap;
  overflow: hidden;
}

.mc-cluster-sub-icon {
  flex-shrink: 0;
  align-self: center;
  color: var(--text-hint);
}

/* Divider lives on the count's leading edge and only appears when a phrasing
   count precedes it — a `v-if`-ed neighbour cannot be assumed, and a bare
   border on the count would leave a dangling rule on single-phrasing rows. */
.mc-cluster-variants + .mc-cluster-count {
  padding-left: var(--space-4);
  border-left: 1px solid color-mix(in srgb, var(--text-primary) 20%, transparent);
}

/* Unit on the phrasing count: the number alone ("4") was ambiguous next to the
   message count's "42 条". Small + muted, exactly like the message unit. */
.mc-cluster-variants i {
  font-style: normal;
  font-family: inherit;
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-normal);
  color: var(--text-muted);
}

/* ── Drill-down page ── */
.mc-detail {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.mc-detail-bar {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  padding: var(--space-4) var(--space-7);
  border-bottom: 1px solid color-mix(in srgb, var(--text-primary) 12%, transparent);
  flex-shrink: 0;
}

/* The drill-down figures are the page's headline (this cluster's magnitude), so
   they are a step larger than the list strip's — same components, more weight.
   Without the bump the top half of the page read as a caption. */
.mc-detail-bar .mc-figure b {
  font-size: calc(20 * var(--dp));
}

.mc-detail-bar .mc-figure em {
  font-size: var(--font-size-xs);
}

.mc-detail-hint {
  margin-left: auto;
  min-width: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Below ~600px the hint cannot fit next to the figures, and an ellipsised
   half-sentence is worse than no sentence: the figures already say what the
   page is. */
@media (max-width: 600px) {
  .mc-detail-hint {
    display: none;
  }
}

.mc-variant-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

/* Rows are flat with hairline separators, not cards: they are a list of
   near-identical strings, and boxing each one would add noise without adding
   structure. The ordinal gutter is what makes the list scannable. */
.mc-variant-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-5);
  padding: var(--space-4) var(--space-7);
  border-bottom: 1px solid color-mix(in srgb, var(--text-primary) 8%, transparent);
  transition: background var(--duration-fast) ease;
}

.mc-variant-row:last-child {
  border-bottom: none;
}

@media (hover: hover) {
  .mc-variant-row:hover {
    background: color-mix(in srgb, var(--text-primary) 4%, transparent);
  }
}

/* Section heading. Small caps-ish label with its glyph and a trailing fact
   (the share, or the count of the section). This is what gives the page a
   second and third type size instead of one flat run of rows. */
.mc-variant-section + .mc-variant-section {
  margin-top: var(--space-2);
}

.mc-variant-section-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-7) var(--space-2);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  letter-spacing: calc(0.4 * var(--dp));
  color: var(--text-secondary);
  text-transform: uppercase;
}

.mc-variant-section-icon {
  flex-shrink: 0;
  color: var(--text-hint);
}

.mc-variant-section-meta {
  margin-left: auto;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-weight: var(--font-weight-normal);
  letter-spacing: 0;
  text-transform: none;
  color: var(--text-secondary);
}

/* The representative is the section's only row, so it carries the accent rail
   on its own — no need for a per-row class check. */
.mc-variant-row.is-representative {
  background: color-mix(in srgb, var(--accent-color) 6%, transparent);
  box-shadow: inset calc(2 * var(--dp)) 0 0 var(--accent-color);
  border-bottom: none;
}

.mc-variant-row.is-representative .mc-variant-index {
  color: var(--accent-color);
}

.mc-variant-row.is-representative .mc-variant-text {
  font-weight: var(--font-weight-medium);
}

.mc-variant-index {
  flex-shrink: 0;
  width: calc(22 * var(--dp));
  padding-top: calc(1 * var(--dp));
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--font-size-xs);
  color: var(--text-hint);
  text-align: right;
}

.mc-variant-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.mc-variant-text {
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
  color: var(--text-primary);
  word-break: break-word;
}

/* ── Buttons ── */
.mc-btn {
  padding: var(--space-2) var(--space-5);
  border: 1px solid color-mix(in srgb, var(--text-primary) 22%, transparent);
  border-radius: var(--radius-xs);
  font-size: var(--font-size-sm);
  cursor: pointer;
  background: none;
  color: var(--text-primary);
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  transition: background var(--duration-fast) ease, border-color var(--duration-fast) ease;
}

@media (hover: hover) {
  .mc-btn:hover {
    background: color-mix(in srgb, var(--text-primary) 7%, transparent);
  }
}

.mc-btn.primary {
  background: var(--accent-color, #0066cc);
  color: #fff;
  border-color: var(--accent-color, #0066cc);
  font-weight: var(--font-weight-medium);
}

@media (hover: hover) {
  .mc-btn.primary:hover {
    opacity: var(--opacity-hover);
  }
}

/* Compact add action: a small outlined control, so a dozen rows do not read as
   a wall of buttons. */
.mc-btn.add {
  color: var(--accent-color, #0066cc);
  border-color: color-mix(in srgb, var(--accent-color) 45%, transparent);
  padding: calc(2 * var(--dp)) var(--space-4);
  font-size: var(--font-size-xs);
  gap: var(--space-1);
}

@media (hover: hover) {
  .mc-btn.add:hover {
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
    border-color: var(--accent-color);
  }
}

/* ── Header action ── */
.bs-header-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.mc-header-btn {
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-xs);
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .mc-header-btn:hover {
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  }
}
</style>
