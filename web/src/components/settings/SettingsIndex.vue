<template>
  <div class="settings-index">
    <!-- Search box (issue #691): settings live in ~20 categories, so the fast
         path is typing the setting's name here. Matches are shown in place of
         the category cards, grouped by category; clicking one opens that page
         with only the matching rows left. -->
    <div class="settings-index__search">
      <SearchInput v-model="query" :placeholder="t('settings.search.placeholder')" @enter="openFirst" />
    </div>
    <template v-if="searching">
      <div v-if="resultGroups.length === 0" class="settings-index__empty">
        {{ t('settings.search.noResults', { query: query.trim() }) }}
      </div>
      <SettingsCard
        v-for="group in resultGroups"
        :key="group.categoryId"
        :title="group.label"
      >
        <div
          v-for="hit in group.rows"
          :key="hit.id"
          class="settings-index__hit"
          :data-settings-hit="hit.id"
          @click="openHit(hit)"
        >
          <div class="settings-index__hit-text">
            <!-- The typed words are marked in both lines: a hit often matches
                 the description rather than the label, and the user needs to
                 see WHY a row is in the list. -->
            <SettingsHighlight
              class="settings-index__label"
              :text="hit.kind === 'category' ? hit.categoryLabel : hit.label"
              :query="query"
            />
            <SettingsHighlight
              v-if="hit.kind === 'item' && hit.description"
              class="settings-index__hit-desc"
              :text="hit.description"
              :query="query"
            />
          </div>
          <ChevronRight class="settings-index__arrow" :size="18" />
        </div>
      </SettingsCard>
    </template>
    <!-- The unfiltered category list; `v-else` chains onto the search template
         above, so only one of the two blocks is ever rendered. -->
    <template v-else>
      <SettingsCard
        v-for="group in groups"
        :key="group.id"
        :title="t(`settings.groups.${group.id}`)"
      >
        <div
          v-for="cat in group.items"
          :key="cat.id"
          class="settings-index__row"
          :data-category="cat.id"
          @click="$emit('navigate', cat.id)"
        >
          <div class="settings-index__left">
            <component :is="cat.icon" class="settings-index__icon" :size="18" />
            <span class="settings-index__label">{{ cat.label }}</span>
          </div>
          <ChevronRight class="settings-index__arrow" :size="18" />
        </div>
      </SettingsCard>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  Palette,
  LayoutGrid,
  FolderGit2,
  MessageSquare,
  MessagesSquare,
  Bot,
  Volume2,
  Mic,
  Brain,
  ScanText,
  Bell,
  Shield,
  Bug,
  Info,
  Gauge,
  Wand2,
  Plug,
  Boxes,
  ChevronRight,
  UserRound,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import SettingsCard from './SettingsCard.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SettingsHighlight from './SettingsHighlight.vue'
import { searchSettings, groupHitsByCategory, type SettingsHit } from './settingsSearch'
import { DOCK_SETTINGS_PAGES, dockCategoryTitleKey } from '@/composables/dockPluginRuntime'

const emit = defineEmits<{
  navigate: [categoryId: string]
  /**
   * A search hit was picked: open `categoryId` narrowed to the row `key`, and
   * show `query` in the filter banner. The query travels with the hit because
   * the index (and its search box) is unmounted as soon as the page opens.
   */
  openSetting: [categoryId: string, key: string, query: string]
}>()

const { t } = useI18n()

/**
 * Rows of the 功能 group that do NOT come from a dock plugin.
 *
 * The manager is the one page that configures every plugin. 聊天会话, 会话列表
 * and 会话索引 are settings pages of the right-pane chat (chat is not a dock
 * entry), so they are listed explicitly right under the manager. Message push /
 * usage meter are features with a settings page but no dock entry. Everything
 * between them is derived from the plugin registry (see `DOCK_SETTINGS_PAGES`),
 * in DOCK order.
 */
const DOCK_MODULE_ITEMS = [
  { id: 'dockPlugins', icon: LayoutGrid },
  { id: 'chat', icon: MessageSquare },
  { id: 'sessions', icon: MessagesSquare },
  { id: 'rag', icon: Brain },
  ...DOCK_SETTINGS_PAGES.filter((page) => page.group === 'dockModules').map((page) => ({
    id: page.categoryId,
    icon: page.icon,
  })),
  { id: 'notification', icon: Bell },
  { id: 'usageMeter', icon: Gauge },
]

/**
 * Category → group layout for the settings home page. Grouping is purely
 * presentational: the category IDs are unchanged, so deep links and the
 * navigation stack keep working (only the display labels are translated).
 *
 * Every category defined in `categoryItems` (settingsFieldMap.ts) must appear
 * here exactly once; `SettingsIndex.test.ts` enforces both directions, because a
 * category with no row is unreachable and a row with no category is a blank
 * page.
 */
const groupDefs = [
  {
    id: 'projectAppearance',
    items: [
      { id: 'project', icon: FolderGit2 },
      { id: 'appearance', icon: Palette },
    ],
  },
  {
    // Model capability is its own top-level group, and it leads the agent
    // group: every agent setting below depends on a model/engine being
    // configured first (provider credentials, the shared embedding service,
    // speech synthesis and recognition).
    id: 'modelCapability',
    items: [
      { id: 'providers', icon: Boxes },
      // The embedding service sits next to the providers because it IS one:
      // one endpoint, one model, one key. Both 会话索引 and 知识库 call it, so it
      // owns its own page instead of living inside either consumer.
      { id: 'embedding', icon: ScanText },
      { id: 'tts', icon: Volume2 },
      { id: 'stt', icon: Mic },
    ],
  },
  {
    id: 'aiChat',
    items: [
      // 身份 first: the nickname and self-description are the context every
      // agent sees, and they are not specific to chat or group chat.
      { id: 'identity', icon: UserRound },
      { id: 'agents', icon: Bot },
      { id: 'skills', icon: Wand2 },
      { id: 'mcp', icon: Plug },
    ],
  },
  {
    // 功能: everything that configures a left-dock feature lives in one
    // group. The manager comes first (it is where those switches live), then
    // the right-pane chat's own three pages, then one page per plugin in DOCK
    // order — DERIVED from the plugin manifests, so a new plugin's settings
    // page appears here without touching this file — then the two pages that
    // configure a feature without owning a dock entry (message push, usage
    // meter).
    //
    // These pages stay listed even when their plugin is hidden or switched
    // off: the user needs them to configure the feature back on.
    id: 'dockModules',
    items: DOCK_MODULE_ITEMS,
  },
  {
    id: 'system',
    items: [
      { id: 'debug', icon: Bug },
      { id: 'security', icon: Shield },
      { id: 'about', icon: Info },
    ],
  },
]

const groups = computed(() =>
  groupDefs
    .map(group => ({
      ...group,
      items: group.items.map(cat => ({
        ...cat,
        // Dock features take their name from the plugin manifest, so the
        // manager, the dock tab and this row can never disagree. Categories
        // without a dock plugin keep their own `settings.categories.*` label.
        label: t(dockCategoryTitleKey(cat.id) ?? `settings.categories.${cat.id}`),
      })),
    }))
    // Dropping the only item of a group would leave an empty titled card.
    .filter(group => group.items.length > 0)
)

// ── Search (issue #691) ──

const query = ref('')

/** Results for the current query, or null while the normal card list shows. */
const results = computed<SettingsHit[] | null>(() =>
  query.value.trim() ? searchSettings(query.value) : null,
)

const searching = computed(() => results.value !== null)

/**
 * One card per category, holding the rows that matched — so a hit's category
 * is the card title instead of a repeated trail on every row. A category hit
 * and its rows land in the same card; the category row is first (its own page
 * is the broader answer to the query) and its `rows` counts both.
 */
const resultGroups = computed(() => {
  const hits = results.value
  if (!hits) return []
  const groups: { categoryId: string; label: string; rows: SettingsHit[] }[] = []
  for (const [categoryId, rows] of groupHitsByCategory(hits)) {
    groups.push({ categoryId, label: rows[0].categoryLabel, rows })
  }
  return groups
})

/** Open a hit: the category page, narrowed to the matching rows. */
function openHit(hit: SettingsHit) {
  if (hit.kind === 'category') {
    // A page hit opens the page itself, unfiltered.
    query.value = ''
    emit('navigate', hit.categoryId)
    return
  }
  emit('openSetting', hit.categoryId, hit.id, query.value.trim())
}

/** Enter in the box opens the best match, so a confident query is one key away. */
function openFirst() {
  const first = results.value?.[0]
  if (first) openHit(first)
}
</script>

<style scoped>
.settings-index {
  padding: var(--space-4);
  /* Same page surface as every other left-panel tab; the grouped cards below
     are the surface step on top of it. */
  background: var(--panel-page-bg);
  min-height: 100%;
}

/* The trailing gap after the last card is redundant page padding. */
.settings-index > :deep(.settings-card:last-child) {
  margin-bottom: 0;
}

.settings-index__search {
  margin-bottom: var(--space-4);
}

.settings-index__empty {
  padding: var(--space-8) var(--space-7);
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--text-muted);
}

/* Search results are records, not list rows: label on one line, the
   description wrapped under it, and no fixed row height (a clamped one-line
   description made the result unreadable). The separator is drawn between
   results instead of under each, so the card's own padding is the only margin. */
.settings-index__hit {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-6);
  padding: var(--space-4) var(--space-7);
  cursor: pointer;
  position: relative;
}

.settings-index__hit:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: var(--space-7);
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

@media (hover: hover) {
  .settings-index__hit:hover {
    background: color-mix(in srgb, var(--text-primary) 5%, var(--bg-secondary));
  }
}

.settings-index__hit:active {
  background: color-mix(in srgb, var(--text-primary) 10%, var(--bg-secondary));
}

.settings-index__hit-text {
  display: flex;
  flex-direction: column;
  gap: calc(2 * var(--dp));
  min-width: 0;
  flex: 1;
}

.settings-index__hit-desc {
  font-size: var(--font-size-sm);
  line-height: var(--line-height-snug);
  color: var(--text-muted);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.settings-index__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: calc(42 * var(--dp));
  padding: 0 var(--space-7);
  cursor: pointer;
  gap: var(--space-6);
  background: transparent;
  position: relative;
}

/* Row separator (not on last) */
.settings-index__row:not(:last-child)::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: calc(48 * var(--dp));
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

@media (hover: hover) {
  /* The row sits on a --bg-secondary card, so the old bg-secondary hover
     vanished into it. Mixed from the text colour: correct in every theme,
     unlike a fixed one-step background jump. */
  .settings-index__row:hover {
    background: color-mix(in srgb, var(--text-primary) 5%, var(--bg-secondary));
  }
}

.settings-index__row:active {
  background: color-mix(in srgb, var(--text-primary) 10%, var(--bg-secondary));
}

.settings-index__left {
  display: flex;
  align-items: center;
  gap: var(--space-6);
  min-width: 0;
}

.settings-index__icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.settings-index__label {
  font-size: var(--font-size-md);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-index__arrow {
  flex-shrink: 0;
  color: var(--text-muted);
}
</style>
