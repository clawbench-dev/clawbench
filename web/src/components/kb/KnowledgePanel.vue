<template>
  <div
    ref="rootEl"
    class="kb-panel"
    :class="{ 'is-drop-target': dragActive }"
    @dragenter.prevent="onDragEnter"
    @dragover.prevent="onDragOver"
    @dragleave="onDragLeave"
    @drop.prevent.stop="onDrop"
  >
    <div class="kb-header">
      <div class="kb-title-line">
        <BookOpen :size="14" class="panel-header-icon" />
        <span class="kb-title">{{ t('nav.knowledgeBase') }}</span>
        <span v-if="collections.length" class="kb-count">{{ t('kb.count', { count: collections.length }) }}</span>
      </div>
      <ActionBar class="kb-header-actions" mode="menu" align="end" size="header">
        <button class="header-btn is-primary" type="button" data-action="kb-new" :title="t('kb.newCollection')" @click="openCreate">
          <Plus :size="15" />
          <span class="action-label">{{ t('kb.newCollection') }}</span>
        </button>
        <RefreshButton class="header-btn" :loading="loadingCollections" :disabled="loadingCollections" :title="t('kb.refresh')" @click="refresh(split)">
          <span class="action-label">{{ t('kb.refresh') }}</span>
        </RefreshButton>
      </ActionBar>
    </div>

    <!-- Dropped files are uploaded into the selected library, so they need the
         same byte-based bar and cancel handle a file-manager folder upload has.
         The batch state is the shared one in useFileUpload; dock tabs are
         mutually exclusive, so the file manager cannot be showing it too. -->
    <UploadProgressBar
      class="kb-upload-bar"
      :visible="dirUploading"
      :progress="dirUploadProgress"
      :done="dirUploadDone"
      :total="dirUploadTotal"
      :speed="dirUploadSpeed"
      @cancel="cancelDirUpload"
    />

    <div v-if="!available" class="kb-empty kb-empty--blocked">
      <Database :size="28" class="kb-empty-icon" />
      <span>{{ t('kb.loadFailed') }}</span>
      <span class="kb-empty-desc">{{ t('kb.emptyCollectionsDesc') }}</span>
    </div>

    <div v-else class="kb-body">
      <!-- Search belongs to the panel, not to a collection: on a phone the
           collection list is the entry screen, and requiring a drill-down
           before searching made "search everything" unreachable. -->
      <div class="kb-search-wrap">
        <div class="kb-search-row">
          <SearchInput
            v-model="searchQuery"
            class="kb-search"
            :placeholder="t('kb.searchPlaceholder')"
            @enter="onSearchEnter"
          />
          <!-- Filters + submit travel as one block: if the row wraps they
               move together instead of leaving the button alone. -->
          <div class="kb-search-controls">
            <select v-model="searchScope" class="kb-select" :title="t('kb.searchScopeTitle')" @change="rerunSearch">
              <option value="current" :disabled="selectedId == null">{{ t('kb.searchThis') }}</option>
              <option value="all">{{ t('kb.searchAll') }}</option>
            </select>
            <select v-model="searchMode" class="kb-select" @change="rerunSearch">
              <option value="hybrid">{{ t('kb.modeHybrid') }}</option>
              <option value="fts">{{ t('kb.modeFts') }}</option>
            </select>
            <button class="header-btn" type="button" data-action="kb-search" :disabled="searching || !searchQuery.trim()" @click="onSearchEnter">
              <Search :size="14" />
            </button>
          </div>
        </div>
        <!-- Index progress lives here because this is where files are added:
             the user should see the queue move without opening settings. -->
        <KbIndexProgress :stats="stats" :vector-enabled="vectorEnabled" compact />
      </div>

      <SplitView
        class="kb-split"
        orientation="horizontal"
        :enabled="split"
        :ratio="splitRatio"
        :min-left="SPLIT_MIN_LIST"
        :min-right="SPLIT_MIN_DETAIL"
        @update:ratio="onSplitRatioChange"
      >
        <!-- Collections: cards on the left. On a phone this pane is the whole
             panel; picking one drills into the detail pane. -->
        <template #left>
          <aside v-show="split || (!searchActive && selectedId == null)" class="kb-rail">
            <PanelSkeleton v-if="loadingCollections && collections.length === 0" variant="list" :rows="6" :meta="true" :label="t('common.loading')" />

            <div v-else-if="error" class="kb-hint kb-hint--error">
              <AlertTriangle :size="16" />
              <span>{{ error }}</span>
              <button class="kb-link" type="button" @click="refresh(split)">{{ t('common.retry') }}</button>
            </div>

            <EmptyState
              v-else-if="collections.length === 0"
              :icon="BookOpen"
              :title="t('kb.emptyCollections')"
              :desc="t('kb.emptyCollectionsDesc')"
            >
              <template #action>
                <button class="fbtn fbtn-primary" type="button" @click="openCreate">
                  <Plus :size="15" />
                  <span>{{ t('kb.newCollection') }}</span>
                </button>
              </template>
            </EmptyState>

            <template v-else>
              <div class="kb-rail-tools">
                <SearchInput
                  v-model="collectionQuery"
                  class="kb-col-search"
                  :placeholder="t('kb.filterCollections')"
                />
                <select v-model="collectionSort" class="kb-select kb-col-sort" :title="t('kb.sortBy')">
                  <option value="recent">{{ t('kb.sortRecent') }}</option>
                  <option value="name">{{ t('kb.sortName') }}</option>
                </select>
              </div>

              <div v-if="filteredCollections.length === 0" class="kb-hint">
                <span>{{ t('kb.filterCollectionsEmpty') }}</span>
                <button class="kb-link" type="button" @click="collectionQuery = ''">{{ t('kb.filterClear') }}</button>
              </div>

              <div v-for="group in groupedCollections" :key="group.key" class="kb-group">
                <div class="kb-group-title">
                  <span>{{ t(group.labelKey) }}</span>
                  <span class="kb-group-count">{{ group.items.length }}</span>
                </div>
                <div v-for="col in group.items" :key="col.id" class="kb-col-row">
                  <button
                    type="button"
                    class="kb-col"
                    :class="{ 'is-active': col.id === selectedId }"
                    :data-kb-collection="col.id"
                    @click="selectCollection(col.id)"
                  >
                    <span class="kb-col-icon"><BookOpen :size="15" /></span>
                    <span class="kb-col-text">
                      <span class="kb-col-name">{{ col.name }}</span>
                      <span class="kb-col-meta">
                        {{ t('kb.documents', { count: col.doc_count }) }}
                        <span v-if="col.failed_count" class="kb-col-flag is-bad">{{ t('kb.colFailed', { count: col.failed_count }) }}</span>
                        <span v-if="col.pending_count" class="kb-col-flag">{{ t('kb.colPending', { count: col.pending_count }) }}</span>
                        <span v-if="!col.auto_inject" class="kb-col-off">{{ t('kb.autoInjectOff') }}</span>
                      </span>
                    </span>
                    <ChevronRight v-if="!split" :size="16" class="kb-col-chevron" />
                  </button>
                  <button
                    type="button"
                    class="kb-col-pin"
                    :class="{ 'is-on': col.pinned }"
                    :title="col.pinned ? t('kb.unpin') : t('kb.pin')"
                    :aria-label="col.pinned ? t('kb.unpin') : t('kb.pin')"
                    :aria-pressed="col.pinned"
                    :data-action="col.pinned ? 'kb-unpin' : 'kb-pin'"
                    @click.stop="togglePin(col)"
                  >
                    <Pin :size="13" :fill="col.pinned ? 'currentColor' : 'none'" />
                  </button>
                </div>
              </div>
            </template>
          </aside>
        </template>

        <template #right>
          <section v-show="split || searchActive || selectedId != null" class="kb-main">
            <!-- Search results replace the detail pane; the collection rail
                 stays put on a wide screen. -->
            <div v-if="searchActive" class="kb-results">
              <div class="kb-results-head">
                <span v-if="searching" class="kb-results-count">{{ t('common.loading') }}</span>
                <span v-else-if="searchResult" class="kb-results-count">{{ t('kb.results', { count: searchResult.total }) }}</span>
                <span v-if="searchResult" class="kb-results-mode">{{ modeLabel(searchResult.mode) }}</span>
                <button v-if="searchResult || searchError" class="kb-link" type="button" @click="clearSearch">
                  {{ t('common.close') }}
                </button>
              </div>

              <div v-if="searchError" class="kb-hint kb-hint--error">
                <AlertTriangle :size="15" />
                <span>{{ searchError }}</span>
              </div>
              <EmptyState
                v-else-if="!searching && searchResult && searchResult.results.length === 0"
                :icon="Search"
                :title="t('kb.searchEmpty')"
                :desc="t('kb.searchEmptyDesc')"
              />
              <div v-else class="kb-result-list">
                <article
                  v-for="hit in searchResult?.results ?? []"
                  :key="hit.chunk_id"
                  class="kb-result"
                  :data-kb-hit="hit.chunk_id"
                  role="button"
                  tabindex="0"
                  :title="t('kb.openDocument')"
                  @click="onResultClick($event, hit)"
                  @keydown.enter="openDocumentById(hit.document_id, hit.chunk_id)"
                >
                  <div class="kb-result-head">
                    <span class="kb-result-title">{{ hit.document_title }}</span>
                    <span v-if="hit.location" class="kb-result-loc">{{ locationText(hit.location) }}</span>
                    <span class="kb-result-col">{{ t('kb.inCollection', { name: hit.collection_name }) }}</span>
                    <ChevronRight :size="14" class="kb-result-open" aria-hidden="true" />
                  </div>
                  <!-- Rendered as markdown (same as the session search
                       drawer), with rune positions re-applied as <mark>. -->
                  <div
                    class="kb-result-text markdown-body"
                    :ref="(el) => setHitRef(hit.chunk_id, el)"
                    v-html="renderedHits[hit.chunk_id] || ''"
                  />
                </article>
              </div>
            </div>

            <template v-else-if="selected">
              <!-- Detail header: identity first, actions second. They never
                   compete for the same line, so a narrow pane cannot squeeze
                   the name into a vertical column. -->
              <header class="kb-main-head">
                <div class="kb-main-title-line">
                  <button v-if="!split" class="kb-back" type="button" :title="t('common.back')" @click="backToCollections">
                    <ChevronLeft :size="18" />
                  </button>
                  <span class="kb-main-icon"><BookOpen :size="18" /></span>
                  <div class="kb-main-title-text">
                    <div class="kb-main-name">
                      <span class="kb-main-name-text">{{ selected.name }}</span>
                      <span class="kb-scope" :class="`kb-scope--${selected.scope}`">
                        {{ selected.scope === 'global' ? t('kb.scopeGlobal') : t('kb.scopeProject') }}
                      </span>
                      <button
                        type="button"
                        class="kb-inject-chip"
                        :class="{ 'is-off': !selected.auto_inject }"
                        data-action="kb-toggle-auto-inject"
                        :title="selected.auto_inject ? t('kb.autoInjectOn') : t('kb.autoInjectOff')"
                        @click="toggleAutoInject"
                      >
                        <Sparkles :size="11" />
                        <span>{{ t('kb.autoInjectShort') }}</span>
                        <span class="kb-inject-dot" aria-hidden="true" />
                        <span class="kb-inject-state">{{ selected.auto_inject ? t('kb.on') : t('kb.off') }}</span>
                      </button>
                      <span v-if="selectMode" class="kb-select-count">
                        {{ t('kb.selectedCount', { count: selectedDocIds.length }) }}
                      </span>
                    </div>
                    <div v-if="selected.description" class="kb-main-desc">{{ selected.description }}</div>
                  </div>
                </div>

                <ActionBar v-if="!selectMode" class="kb-main-actions" mode="menu" align="start" size="bar">
                  <button class="header-btn is-primary" type="button" data-action="kb-add" :title="t('kb.addFile')" @click="openAdd">
                    <Plus :size="15" />
                    <span class="action-label">{{ t('kb.addFile') }}</span>
                  </button>
                  <button class="header-btn" type="button" data-action="kb-paste-text" :title="t('kb.pasteTitle')" @click="openText">
                    <ClipboardPaste :size="14" />
                  </button>
                  <button class="header-btn" type="button" data-action="kb-add-url" :title="t('kb.urlTitle')" @click="addFromURL">
                    <Link2 :size="14" />
                  </button>
                  <button class="header-btn" type="button" data-action="kb-edit-collection" :title="t('kb.editCollection')" @click="openEdit">
                    <Pencil :size="14" />
                  </button>
                  <button class="header-btn" type="button" data-action="kb-select-mode" :title="t('kb.select')" @click="enterSelectMode">
                    <ListChecks :size="14" />
                  </button>
                  <button class="header-btn" type="button" data-action="kb-reindex-collection" :title="t('kb.reindex')" @click="reindexCurrent">
                    <RotateCw :size="14" />
                  </button>
                  <button class="header-btn is-danger" type="button" data-action="kb-delete-collection" :title="t('kb.deleteCollection')" @click="removeCurrent">
                    <Trash2 :size="14" />
                  </button>
                </ActionBar>

                <ActionBar v-else class="kb-main-actions" mode="menu" align="start" size="bar">
                  <button class="header-btn" type="button" data-action="kb-select-all" :title="t('kb.selectAll')" @click="toggleSelectAll">
                    <ListChecks :size="14" />
                    <span class="action-label">{{ allShownSelected ? t('kb.selectNone') : t('kb.selectAll') }}</span>
                  </button>
                  <button
                    class="header-btn"
                    type="button"
                    data-action="kb-select-move"
                    :title="t('kb.move')"
                    :disabled="selectedDocIds.length === 0"
                    @click="openMovePicker"
                  >
                    <FolderInput :size="14" />
                  </button>
                  <button
                    class="header-btn"
                    type="button"
                    data-action="kb-select-reindex"
                    :title="t('kb.reindex')"
                    :disabled="selectedDocIds.length === 0"
                    @click="reindexSelected"
                  >
                    <RotateCw :size="14" />
                  </button>
                  <button
                    class="header-btn is-danger"
                    type="button"
                    data-action="kb-select-delete"
                    :title="t('common.delete')"
                    :disabled="selectedDocIds.length === 0"
                    @click="deleteSelected"
                  >
                    <Trash2 :size="14" />
                  </button>
                  <button class="header-btn" type="button" data-action="kb-select-cancel" :title="t('common.cancel')" @click="exitSelectMode">
                    <X :size="14" />
                  </button>
                </ActionBar>
              </header>

              <!-- Health line: the counts that decide whether this library can
                   be trusted. Each one is also a filter, because "1 failed"
                   is only useful if it takes you to the file. -->
              <div v-if="docHealth.total > 0" class="kb-health">
                <span class="kb-health-total">{{ t('kb.healthFiles', { count: docHealth.total }) }}</span>
                <button
                  v-if="docHealth.failed"
                  type="button"
                  class="kb-health-chip is-bad"
                  :class="{ 'is-on': docFilter === 'failed' }"
                  data-action="kb-filter-failed"
                  @click="toggleDocFilter('failed')"
                >{{ t('kb.healthFailed', { count: docHealth.failed }) }}</button>
                <button
                  v-if="docHealth.working"
                  type="button"
                  class="kb-health-chip"
                  :class="{ 'is-on': docFilter === 'working' }"
                  data-action="kb-filter-working"
                  @click="toggleDocFilter('working')"
                >{{ t('kb.healthWorking', { count: docHealth.working }) }}</button>
                <button
                  v-if="docHealth.ftsOnly"
                  type="button"
                  class="kb-health-chip"
                  :class="{ 'is-on': docFilter === 'fts' }"
                  data-action="kb-filter-fts"
                  @click="toggleDocFilter('fts')"
                >{{ t('kb.healthFts', { count: docHealth.ftsOnly }) }}</button>
                <button v-if="docFilter !== 'all'" class="kb-link" type="button" @click="docFilter = 'all'">
                  {{ t('kb.filterClear') }}
                </button>
                <span class="kb-health-spacer" />
                <select v-model="docType" class="kb-select kb-health-select" :title="t('kb.filterType')">
                  <option value="all">{{ t('kb.typeAll') }}</option>
                  <option value="pdf">PDF</option>
                  <option value="word">{{ t('kb.typeWord') }}</option>
                  <option value="excel">{{ t('kb.typeExcel') }}</option>
                  <option value="ppt">{{ t('kb.typePpt') }}</option>
                  <option value="text">{{ t('kb.typeText') }}</option>
                </select>
                <select v-model="docSort" class="kb-select kb-health-select" :title="t('kb.sortBy')">
                  <option value="recent">{{ t('kb.sortRecent') }}</option>
                  <option value="name">{{ t('kb.sortName') }}</option>
                </select>
                <input
                  v-model="docTagQuery"
                  class="kb-select kb-health-select kb-health-tag"
                  :placeholder="t('kb.filterTags')"
                />
              </div>

              <div class="kb-docs">
                <PanelSkeleton v-if="loadingDocuments && documents.length === 0" variant="list" :rows="6" :meta="true" :label="t('common.loading')" />

                <EmptyState
                  v-else-if="documents.length === 0"
                  :icon="FileText"
                  :title="t('kb.emptyDocuments')"
                  :desc="t('kb.emptyDocumentsDesc')"
                >
                  <template #action>
                    <button class="fbtn fbtn-primary" type="button" @click="openAdd">
                      <Plus :size="15" />
                      <span>{{ t('kb.addFile') }}</span>
                    </button>
                  </template>
                </EmptyState>

                <div v-else-if="visibleDocuments.length === 0" class="kb-hint">
                  <span>{{ t('kb.filterEmpty') }}</span>
                  <button class="kb-link" type="button" @click="docFilter = 'all'">{{ t('kb.filterClear') }}</button>
                </div>

                <div v-else class="kb-doc-list">
                  <div v-for="doc in shownDocuments" :key="doc.id" class="kb-doc" :class="{ 'is-selected': selectMode && isSelected(doc.id) }" :data-kb-doc="doc.id">
                    <span v-if="selectMode" class="kb-doc-check" :class="{ 'is-on': isSelected(doc.id) }" aria-hidden="true">
                      <Check v-if="isSelected(doc.id)" :size="12" />
                    </span>
                    <!-- The row body is a button so a click opens the parsed
                         content; the two icon actions stay siblings (a button
                         cannot nest another). In select mode the same click
                         toggles the row instead. -->
                    <button
                      class="kb-doc-open"
                      type="button"
                      :title="selectMode ? t('kb.select') : t('kb.openDocument')"
                      :aria-pressed="selectMode ? isSelected(doc.id) : undefined"
                      @click="selectMode ? toggleDoc(doc.id) : openDocument(doc)"
                    >
                      <span class="kb-doc-icon" :class="`kb-doc-icon--${doc.ext?.replace('.', '') || 'file'}`">
                        <FileText :size="16" />
                      </span>
                      <div class="kb-doc-main">
                        <div class="kb-doc-name-line">
                          <span class="kb-doc-name" :title="doc.title">{{ doc.title }}</span>
                          <span class="kb-status" :class="`kb-status--${statusKindOf(doc)}`">{{ statusLabelOf(doc) }}</span>
                        </div>
                        <div class="kb-doc-meta">
                          <span>{{ formatSize(doc.size_bytes) }}</span>
                          <template v-if="doc.chunk_count > 0">
                            <span class="kb-sep">·</span>
                            <span>{{ t('kb.chunks', { count: doc.chunk_count }) }}</span>
                          </template>
                          <template v-if="doc.status === 'ready' && vectorEnabled">
                            <span class="kb-sep">·</span>
                            <span :class="{ 'kb-muted': !doc.vector_ready }">
                              {{ doc.vector_ready ? t('kb.vectorReady') : t('kb.vectorPending') }}
                            </span>
                          </template>
                          <template v-if="doc.warning === 'truncated'">
                            <span class="kb-sep">·</span>
                            <span class="kb-warn">{{ t('kb.warningTruncated') }}</span>
                          </template>
                          <template v-if="doc.source_missing">
                            <span class="kb-sep">·</span>
                            <span class="kb-warn">{{ t('kb.sourceMissing') }}</span>
                          </template>
                          <template v-else-if="doc.source_stale">
                            <span class="kb-sep">·</span>
                            <span class="kb-stale">{{ t('kb.sourceStale') }}</span>
                          </template>
                        </div>
                        <div v-if="doc.tags" class="kb-doc-tags">
                          <span v-for="tag in tagList(doc.tags)" :key="tag" class="kb-doc-tag">{{ tag }}</span>
                        </div>
                        <!-- Two files can share a name; the path is what tells
                             them apart. -->
                        <div v-if="doc.source_path" class="kb-doc-source" :title="doc.source_path">{{ doc.source_path }}</div>
                        <div v-if="doc.error" class="kb-doc-error">{{ docErrorText(doc) }}</div>
                      </div>
                    </button>
                    <div v-if="!selectMode" class="kb-doc-actions">
                      <button class="kb-icon-btn" type="button" data-action="kb-reindex-doc" :title="t('kb.reindex')" @click.stop="reindexDoc(doc)">
                        <RotateCw :size="14" />
                      </button>
                      <button class="kb-icon-btn is-danger" type="button" data-action="kb-delete-doc" :title="t('common.delete')" @click.stop="removeDoc(doc)">
                        <Trash2 :size="14" />
                      </button>
                    </div>
                  </div>
                  <button
                    v-if="hasMoreDocuments"
                    class="kb-link kb-load-more"
                    type="button"
                    @click="docRenderLimit += DOC_PAGE"
                  >
                    {{ t('kb.showMore', { count: visibleDocuments.length - docRenderLimit }) }}
                  </button>
                </div>
              </div>
            </template>

            <EmptyState
              v-else
              :icon="BookOpen"
              :title="t('kb.selectCollection')"
              :desc="t('kb.selectCollectionDesc')"
            />
          </section>
        </template>
      </SplitView>
    </div>

    <!-- Create / edit collection. Scope is only editable while creating:
         moving a library between projects would make its files appear or
         vanish elsewhere, so the server rejects it too. -->
    <ModalDialog :open="formOpen" :title="formTitle" @close="formOpen = false">
      <template #header>
        <BookOpen :size="16" class="modal-header-icon" />
        <span class="modal-title">{{ formTitle }}</span>
      </template>

      <div class="kb-form">
        <label class="kb-field">
          <span class="kb-label">{{ t('kb.nameLabel') }}</span>
          <input
            v-model="formName"
            class="kb-input"
            data-role="kb-name"
            :placeholder="t('kb.namePlaceholder')"
            maxlength="100"
            @keydown.enter="submitForm"
          />
        </label>
        <label class="kb-field">
          <span class="kb-label">{{ t('kb.descriptionLabel') }}</span>
          <textarea
            v-model="formDescription"
            class="kb-input kb-textarea"
            data-role="kb-description"
            :placeholder="t('kb.descriptionPlaceholder')"
            rows="3"
          />
        </label>
        <div v-if="formMode === 'create'" class="kb-field">
          <span class="kb-label">{{ t('kb.scopeLabel') }}</span>
          <!-- Segmented control, same shape as the provider form: two choices
               belong on one line, and only the CURRENT one's explanation is
               shown. The old radio cards printed both hints at once and were
               the heaviest thing in the dialog. -->
          <div class="kb-seg" role="radiogroup" :aria-label="t('kb.scopeLabel')">
            <button
              type="button"
              class="kb-seg-btn"
              :class="{ 'is-on': formScope === 'project' }"
              :disabled="!hasProject"
              role="radio"
              :aria-checked="formScope === 'project'"
              data-role="kb-scope-project"
              @click="formScope = 'project'"
            >{{ t('kb.scopeProject') }}</button>
            <button
              type="button"
              class="kb-seg-btn"
              :class="{ 'is-on': formScope === 'global' }"
              role="radio"
              :aria-checked="formScope === 'global'"
              data-role="kb-scope-global"
              @click="formScope = 'global'"
            >{{ t('kb.scopeGlobal') }}</button>
          </div>
          <span class="kb-hint-text">
            {{ formScope === 'global' ? t('kb.scopeGlobalHint') : t('kb.scopeProjectHint') }}
          </span>
        </div>
        <div v-if="formError" class="kb-form-error" role="alert">{{ formError }}</div>
      </div>

      <template #footer>
        <button class="fbtn" type="button" @click="formOpen = false">{{ t('common.cancel') }}</button>
        <button class="fbtn fbtn-primary" type="button" :disabled="formSaving" @click="submitForm">
          {{ formSaving ? t('common.loading') : formMode === 'create' ? t('kb.create') : t('kb.save') }}
        </button>
      </template>
    </ModalDialog>

    <!-- Paste text: the fastest way to put a note into a library without
         creating a file first. It goes through the same upload path as a file,
         so limits and duplicate detection are identical. -->
    <ModalDialog :open="textOpen" :title="t('kb.pasteTitle')" @close="textOpen = false">
      <template #header>
        <ClipboardPaste :size="16" class="modal-header-icon" />
        <span class="modal-title">{{ t('kb.pasteTitle') }}</span>
      </template>
      <div class="kb-form">
        <label class="kb-field">
          <span class="kb-label">{{ t('kb.pasteNameLabel') }}</span>
          <input
            v-model="textTitle"
            class="kb-input"
            data-role="kb-text-title"
            maxlength="100"
            :placeholder="t('kb.pasteNamePlaceholder')"
          />
        </label>
        <label class="kb-field">
          <span class="kb-label">{{ t('kb.pasteBodyLabel') }}</span>
          <textarea
            v-model="textBody"
            class="kb-input kb-textarea kb-paste-body"
            data-role="kb-text-body"
            rows="10"
            :placeholder="t('kb.pastePlaceholder')"
          />
        </label>
        <div v-if="textError" class="kb-form-error" role="alert">{{ textError }}</div>
      </div>
      <template #footer>
        <button class="fbtn" type="button" @click="textOpen = false">{{ t('common.cancel') }}</button>
        <button class="fbtn fbtn-primary" type="button" :disabled="textSaving" @click="submitText">
          {{ textSaving ? t('common.loading') : t('kb.pasteConfirm') }}
        </button>
      </template>
    </ModalDialog>

    <KBCollectionPickerDialog
      :open="movePickerOpen"
      :allow-create="false"
      :exclude-id="selectedId ?? undefined"
      @close="movePickerOpen = false"
      @confirm="onMovePicked"
    />

    <!-- Document detail: what the file was parsed into, which is the only way
         to tell "indexed but useless" from "not indexed yet". -->
    <KBDocumentDrawer
      ref="documentDrawer"
      :open="drawerOpen"
      :document-id="drawerDocId"
      :focus-chunk-id="drawerChunkId"
      :vector-enabled="vectorEnabled"
      @close="closeDocument"
      @loaded="onDocumentLoaded"
      @reindex="onDrawerReindex"
      @refresh="onDrawerRefresh"
      @quote="onQuoteChunk"
      @save-tags="onDrawerTags"
      @delete="onDrawerDelete"
    />

    <!-- Add documents. This is the SAME picker the chat composer opens: browse
         the project, upload from the device (with progress + terminate), or
         reuse a recent upload. Re-implementing a bare file browser here lost
         multi-select, upload progress and the mobile upload entry. -->
    <AttachmentPicker
      :open="addOpen"
      variant="kb"
      selection-only
      :title="t('kb.addToCollection')"
      :confirm-label="adding ? t('common.loading') : t('kb.addToCollection')"
      :busy="adding"
      :current-file="store.state.currentFile?.path || null"
      :current-dir="store.state.projectRoot"
      @close="closeAdd"
      @confirm="submitAdd"
    />

    <!-- Desktop drop: files and folders from the OS go straight into the
         selected library. Same full-bleed overlay the file manager uses. -->
    <DropOverlay :visible="osDragActive" :label="osDropLabel" />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUpdate, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  BookOpen,
  Check,
  ChevronLeft,
  ClipboardPaste,
  Link2,
  ChevronRight,
  Database,
  FileText,
  FolderInput,
  ListChecks,
  Pencil,
  Pin,
  Plus,
  RotateCw,
  Search,
  Sparkles,
  Trash2,
  X,
} from 'lucide-vue-next'
import ActionBar from '@/components/common/ActionBar.vue'
import SplitView from '@/components/common/SplitView.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ModalDialog from '@/components/common/ModalDialog.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import DropOverlay from '@/components/common/DropOverlay.vue'
import UploadProgressBar from '@/components/common/UploadProgressBar.vue'
import AttachmentPicker from '@/components/attach/AttachmentPicker.vue'
import KBDocumentDrawer from '@/components/kb/KBDocumentDrawer.vue'
import { useDialog } from '@/composables/useDialog'
import {
  useKnowledgeBase,
  type KBCollection,
  type KBDocument,
  type KBDocumentDetail,
  type KBSearchHit,
} from '@/composables/useKnowledgeBase'
import { useToast } from '@/composables/useToast'
import { useUiScale } from '@/composables/useUiScale'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { store } from '@/stores/app'
import type { FileEntry } from '@/utils/fileAttachmentUtils'
import { baseName } from '@/utils/path'
import {
  documentErrorText,
  formatLocation,
  formatSize,
  skipReasonHint,
  statusKind,
  statusLabel,
  type TFn,
} from '@/utils/kbFormat'
import { appLog } from '@/utils/appLog'
import { startKBIndexWatch } from '@/composables/useKBIndexStatus'
import { kbAddFromURL, kbUploadText } from '@/composables/useKnowledgeBase'
import { consumeKBPendingOpen, kbPendingOpen } from '@/utils/kbNavigation'
import { useChatBridge } from '@/composables/useChatBridge'
import { attachDragTargets, hasAttachDragData, readAttachDragData } from '@/utils/attachDrag'
import { expandDataTransfer, hasOSFileDrop, type ExpandResult } from '@/utils/dropFolder'
import { useFileUpload } from '@/composables/useFileUpload'
import { DEFAULT_KB_SPLIT_RATIO, layoutEpoch, readPanelState, writePanelState } from '@/utils/panelState'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer.ts'
import { highlightTermsInElement, termsFromRunePositions } from '@/utils/searchHighlight'
import KbIndexProgress from './KbIndexProgress.vue'

const props = defineProps<{ active?: boolean }>()

const { t } = useI18n()
const { isWideScreen } = useWideScreenLayout()
const uiScale = useUiScale()
const dialog = useDialog()
const toast = useToast()
const chatBridge = useChatBridge()
const kb = useKnowledgeBase()
const {
  dirUploading,
  dirUploadProgress,
  dirUploadSpeed,
  dirUploadTotal,
  dirUploadDone,
  cancelDirUpload,
  uploadFilesToCollection,
} = useFileUpload()

const {
  collections,
  selectedId,
  documents,
  loadingCollections,
  loadingDocuments,
  error,
  available,
  vectorEnabled,
  stats,
  statsActive,
  searchQuery,
  searchScope,
  searchMode,
  searching,
  searchResult,
  searchError,
  selectCollection,
  clearSelection,
  loadDocuments,
  reloadForProject,
  refresh,
  createCollection,
  updateCollection,
  deleteCollection,
  setAutoInject,
  setPinned,
  addFile,
  importDirectory,
  previewDirectory,
  deleteDocument,
  reindexDocument,
  refreshDocument,
  moveDocument,
  setDocumentTags,
  reindexCollection,
  clearSearch,
  runSearch,
  startPolling,
  stopPolling,
} = kb

const selected = computed<KBCollection | null>(
  () => collections.value.find((c) => c.id === selectedId.value) ?? null,
)
const hasProject = computed(() => !!store.state.projectRoot)

// ── Search / health ──
const searchActive = computed(() => !!(searchResult.value || searching.value || searchError.value))

type DocFilter = 'all' | 'failed' | 'working' | 'fts'
const docFilter = ref<DocFilter>('all')

type DocType = 'all' | 'pdf' | 'word' | 'excel' | 'ppt' | 'text'
const docType = ref<DocType>('all')
const docSort = ref<'recent' | 'name'>('recent')
const docTagQuery = ref('')

/** File-type bucket. The knowledge base only accepts a small set, so a fixed
 *  list beats a select that grows with whatever extensions users happen to add. */
function typeOf(doc: KBDocument): Exclude<DocType, 'all'> {
  const ext = (doc.ext || '').toLowerCase()
  if (ext === '.pdf') return 'pdf'
  if (ext === '.docx') return 'word'
  if (ext === '.xlsx' || ext === '.csv') return 'excel'
  if (ext === '.pptx') return 'ppt'
  return 'text'
}

/** Counts that decide whether the library can be trusted, all from the list. */
const docHealth = computed(() => {
  const total = documents.value.length
  const failed = documents.value.filter((d) => d.status === 'failed').length
  const working = documents.value.filter((d) => d.status === 'pending' || d.status === 'extracting').length
  // Without a semantic index every document stays un-vectorized; that is the
  // library's normal state (the header already says so), not a backlog.
  const ftsOnly = vectorEnabled.value
    ? documents.value.filter((d) => d.status === 'ready' && !d.vector_ready).length
    : 0
  return { total, failed, working, ftsOnly }
})

const visibleDocuments = computed(() => {
  let out = documents.value
  switch (docFilter.value) {
    case 'failed':
      out = out.filter((d) => d.status === 'failed')
      break
    case 'working':
      out = out.filter((d) => d.status === 'pending' || d.status === 'extracting')
      break
    case 'fts':
      out = out.filter((d) => d.status === 'ready' && !d.vector_ready)
      break
  }
  if (docType.value !== 'all') {
    out = out.filter((d) => typeOf(d) === docType.value)
  }
  const tagQuery = docTagQuery.value.trim().toLowerCase()
  if (tagQuery) {
    out = out.filter((d) => (d.tags || '').toLowerCase().includes(tagQuery))
  }
  if (docSort.value === 'name') {
    out = [...out].sort((a, b) =>
      a.title.localeCompare(b.title, undefined, { numeric: true, sensitivity: 'base' }),
    )
  }
  return out
})

// A 500-file import must not build 500 rows on every poll. Render a page at a
// time; the rest is one click away.
const DOC_PAGE = 100
const docRenderLimit = ref(DOC_PAGE)
const shownDocuments = computed(() => visibleDocuments.value.slice(0, docRenderLimit.value))
const hasMoreDocuments = computed(() => visibleDocuments.value.length > docRenderLimit.value)

function toggleDocFilter(kind: Exclude<DocFilter, 'all'>): void {
  docFilter.value = docFilter.value === kind ? 'all' : kind
}

// ── Multi-select ──
// Selecting is a mode, not a persistent checkbox column: the normal row click
// opens the document, and that must stay the default.
const selectMode = ref(false)
const selectedDocIds = ref<number[]>([])

function enterSelectMode(): void {
  selectMode.value = true
  selectedDocIds.value = []
}

function exitSelectMode(): void {
  selectMode.value = false
  selectedDocIds.value = []
}

function isSelected(id: number): boolean {
  return selectedDocIds.value.includes(id)
}

function toggleDoc(id: number): void {
  selectedDocIds.value = isSelected(id)
    ? selectedDocIds.value.filter((x) => x !== id)
    : [...selectedDocIds.value, id]
}

const allShownSelected = computed(
  () => shownDocuments.value.length > 0 && shownDocuments.value.every((d) => isSelected(d.id)),
)

function toggleSelectAll(): void {
  selectedDocIds.value = allShownSelected.value ? [] : shownDocuments.value.map((d) => d.id)
}

async function deleteSelected(): Promise<void> {
  const count = selectedDocIds.value.length
  if (!count) return
  const ok = await dialog.confirm(t('kb.deleteSelectedConfirm', { count }), { dangerous: true })
  if (!ok) return
  const ids = [...selectedDocIds.value]
  const errors: string[] = []
  let done = 0
  for (const id of ids) {
    try {
      await deleteDocument(id)
      done++
    } catch (e) {
      errors.push(e instanceof Error ? e.message : String(e))
    }
  }
  exitSelectMode()
  if (errors.length > 0) {
    toast.show(errors[0], { icon: 'warning', type: 'error', duration: 4000 })
  } else {
    toast.show(t('kb.deleteSelectedDone', { count: done }), { type: 'success', duration: 2200 })
  }
}

async function reindexSelected(): Promise<void> {
  const count = selectedDocIds.value.length
  if (!count) return
  const ok = await dialog.confirm(t('kb.reindexSelectedConfirm', { count }))
  if (!ok) return
  const ids = [...selectedDocIds.value]
  const errors: string[] = []
  for (const id of ids) {
    try {
      await reindexDocument(id)
    } catch (e) {
      errors.push(e instanceof Error ? e.message : String(e))
    }
  }
  exitSelectMode()
  startKBIndexWatch()
  if (errors.length > 0) {
    toast.show(errors[0], { icon: 'warning', type: 'error', duration: 4000 })
  } else {
    toast.show(t('kb.reindexSelectedDone', { count }), { type: 'success', duration: 2200 })
  }
}

watch([docFilter, docType, docSort, docTagQuery], () => {
  docRenderLimit.value = DOC_PAGE
})

// ── Drag files/folders into the panel ──
// Two sources, two paths:
//  - an internal attach drag (a row dragged out of the file manager) names
//    server paths, so the existing add flow (preview included) does the work;
//  - a desktop drop carries OS files/folders, which must be uploaded first —
//    the file manager's drop pipeline, reused here, expands folders and
//    streams every file into the selected library.
const dragActive = ref(false)
let dragDepth = 0

// Desktop drop state is separate: both overlays can be armed by the same
// dragenter/dragleave sequence, and one counter cannot unwind both.
const osDragActive = ref(false)
let osDragDepth = 0

const osDropLabel = computed(() =>
  selected.value ? t('kb.dropToAdd') : t('kb.dropSelectCollection'),
)

function onDragEnter(e: DragEvent): void {
  if (hasAttachDragData(e.dataTransfer)) {
    dragDepth++
    dragActive.value = true
    return
  }
  if (!hasOSFileDrop(e.dataTransfer)) return
  osDragDepth++
  osDragActive.value = true
}

function onDragOver(e: DragEvent): void {
  if (hasAttachDragData(e.dataTransfer) || hasOSFileDrop(e.dataTransfer)) e.preventDefault()
}

function onDragLeave(): void {
  dragDepth = Math.max(0, dragDepth - 1)
  if (dragDepth === 0) dragActive.value = false
  osDragDepth = Math.max(0, osDragDepth - 1)
  if (osDragDepth === 0) osDragActive.value = false
}

function onDrop(e: DragEvent): void {
  dragDepth = 0
  dragActive.value = false
  osDragDepth = 0
  osDragActive.value = false

  const data = readAttachDragData(e.dataTransfer)
  if (data) {
    if (!selected.value) {
      toast.show(t('kb.dropSelectCollection'), { icon: 'warning', type: 'error', duration: 2600 })
      return
    }
    void submitAdd(attachDragTargets(data))
    return
  }

  if (!hasOSFileDrop(e.dataTransfer) || !e.dataTransfer) return
  // Snapshot the drop synchronously: the DataTransfer is cleared as soon as
  // this handler yields, so the expansion must start here, not after an await.
  const expanding = expandDataTransfer(e.dataTransfer)
  void addDroppedFiles(expanding)
}

/**
 * Add the files of a desktop drop to the selected library.
 *
 * Folders arrive as a flat list of files (see `expandDataTransfer`); the
 * library has no folder structure, so relative paths are dropped and only the
 * base names are kept. Each file is uploaded through the KB endpoint, which
 * copies it into the library — no staging copy is left in the project.
 */
async function addDroppedFiles(expanding: Promise<ExpandResult>): Promise<void> {
  const col = selected.value
  if (!col) {
    toast.show(t('kb.dropSelectCollection'), { icon: 'warning', type: 'error', duration: 2600 })
    return
  }
  // A blocked library (server unreachable, feature off) cannot take files; the
  // panel already says so, so a drop is a no-op rather than an error.
  if (!available.value || adding.value) return

  const { files } = await expanding
  if (files.length === 0) {
    toast.show(t('kb.dirImportEmpty'), { icon: 'warning', type: 'error', duration: 2600 })
    return
  }

  adding.value = true
  try {
    const res = await uploadFilesToCollection(files.map((f) => f.file), col.id)
    if (res.cancelled || (res.added === 0 && res.failed === 0)) return
    if (res.added > 0) {
      await loadDocuments(col.id, true)
      startKBIndexWatch()
    }
    if (res.failed === 0) {
      toast.show(t('kb.dirImportDone', { added: res.added }), { type: 'success', duration: 2400 })
      return
    }
    // Same wording as the picker's add: one rejected file names its reason,
    // several fold into "first + N more" so the list cannot flood the toast.
    const first = res.errors[0] || t('kb.unsupported')
    toast.show(
      res.failed === 1 ? first : t('kb.addPartial', { first, count: res.failed - 1 }),
      { icon: 'warning', type: 'error', duration: 4000 },
    )
  } finally {
    adding.value = false
  }
}

watch(selectedId, () => {
  docFilter.value = 'all'
  docType.value = 'all'
  docTagQuery.value = ''
  exitSelectMode()
})

// ── Layout: split by the PANEL's width, not the window's ──
// The dock pane is 40% of the window, so the same window can hold a wide or a
// narrow panel. Mail does the same: split only when both panes fit, otherwise
// drill down. `clientWidth` is CSS px, so thresholds are scaled.
// At the default 1280px window the dock pane is ~492px. 190 + 280 leaves the
// detail pane usable while still fitting that default, so wide screens get the
// side-by-side layout without the user having to drag the divider first.
const SPLIT_MIN_LIST = 190
const SPLIT_MIN_DETAIL = 280
const SPLIT_MIN_PANEL = SPLIT_MIN_LIST + SPLIT_MIN_DETAIL + 1

const rootEl = ref<HTMLElement | null>(null)
const panelWidth = ref(0)
const split = computed(() => {
  // jsdom / first paint has no measurement yet; fall back to the window class
  // so tests and the first desktop frame behave like a wide panel.
  const width = panelWidth.value || (isWideScreen.value ? 900 : 0)
  return width >= SPLIT_MIN_PANEL * uiScale.value
})

const splitRatio = ref(clampSplitRatio(readPanelState('kbSplitRatio')))

function clampSplitRatio(ratio: number): number {
  return ratio > 0 && ratio < 1 ? ratio : DEFAULT_KB_SPLIT_RATIO
}

function onSplitRatioChange(ratio: number): void {
  splitRatio.value = ratio
  writePanelState('kbSplitRatio', ratio)
}

watch(layoutEpoch, () => {
  splitRatio.value = clampSplitRatio(readPanelState('kbSplitRatio'))
})

function measureWidth(): void {
  const el = rootEl.value
  if (el) panelWidth.value = el.getBoundingClientRect().width
}

let widthObserver: ResizeObserver | null = null
onMounted(() => {
  measureWidth()
  if (typeof ResizeObserver !== 'undefined' && rootEl.value) {
    widthObserver = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width
      if (typeof width === 'number') panelWidth.value = width
    })
    widthObserver.observe(rootEl.value)
  }
})

// When the panel grows from drill-down to split, the list has no selection
// (that is the mobile entry state). Pick the project collection, then the
// first one, so the right pane is not empty.
watch(split, (on) => {
  if (!on || selectedId.value != null || collections.value.length === 0) return
  const target = collections.value.find((c) => c.scope === 'project') ?? collections.value[0]
  if (target) void selectCollection(target.id)
})

const collectionQuery = ref('')
const collectionSort = ref<'recent' | 'name'>('recent')

const filteredCollections = computed(() => {
  const q = collectionQuery.value.trim().toLowerCase()
  let out = q ? collections.value.filter((c) => c.name.toLowerCase().includes(q)) : collections.value
  if (collectionSort.value === 'name') {
    out = [...out].sort(
      (a, b) =>
        (b.pinned ? 1 : 0) - (a.pinned ? 1 : 0) ||
        a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' }),
    )
  }
  return out
})

const groupedCollections = computed(() => {
  const global = filteredCollections.value.filter((c) => c.scope === 'global')
  const project = filteredCollections.value.filter((c) => c.scope === 'project')
  const groups: { key: string; labelKey: string; items: KBCollection[] }[] = []
  if (project.length) groups.push({ key: 'project', labelKey: 'kb.projectGroup', items: project })
  if (global.length) groups.push({ key: 'global', labelKey: 'kb.globalGroup', items: global })
  return groups
})

// ── Activation / polling ──
watch(
  () => props.active,
  (on) => {
    if (on) {
      void refresh(split.value).then(() => {
        // The panel may be opened mid-index, possibly on an empty collection.
        // Start the counter poll from the freshly loaded stats, not from a
        // document list that has nothing pending.
        if (statsActive.value) startPolling()
      })
    } else {
      stopPolling()
    }
  },
  { immediate: true },
)

watch(documents, () => {
  if (
    documents.value.some(
      (d) =>
        d.status === 'pending' ||
        d.status === 'extracting' ||
        (vectorEnabled.value && d.status === 'ready' && !d.vector_ready),
    )
  ) {
    startPolling()
  }
})

onBeforeUnmount(() => {
  stopPolling()
  if (searchTimer) clearTimeout(searchTimer)
  widthObserver?.disconnect()
  widthObserver = null
})

// ── Create / edit ──
// One dialog for both: the fields are the same, and only scope is create-only.
const formOpen = ref(false)
const formMode = ref<'create' | 'edit'>('create')
const formName = ref('')
const formDescription = ref('')
const formScope = ref<'project' | 'global'>('project')
const formError = ref('')
const formSaving = ref(false)

const formTitle = computed(() =>
  formMode.value === 'create' ? t('kb.createTitle') : t('kb.editTitle'),
)

function openCreate(): void {
  formMode.value = 'create'
  formName.value = ''
  formDescription.value = ''
  formScope.value = hasProject.value ? 'project' : 'global'
  formError.value = ''
  formOpen.value = true
}

function openEdit(): void {
  const col = selected.value
  if (!col) return
  formMode.value = 'edit'
  formName.value = col.name
  formDescription.value = col.description
  formError.value = ''
  formOpen.value = true
}

async function submitForm(): Promise<void> {
  const name = formName.value.trim()
  if (!name) {
    formError.value = t('kb.nameRequired')
    return
  }
  formSaving.value = true
  formError.value = ''
  try {
    if (formMode.value === 'create') {
      await createCollection(name, formDescription.value.trim(), formScope.value)
      toast.show(t('kb.created'), { type: 'success', duration: 1800 })
    } else if (selected.value) {
      await updateCollection(selected.value.id, {
        name,
        description: formDescription.value.trim(),
      })
      toast.show(t('kb.saved'), { type: 'success', duration: 1800 })
    }
    formOpen.value = false
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e)
  } finally {
    formSaving.value = false
  }
}

// ── Paste text ──
const textOpen = ref(false)
const textTitle = ref('')
const textBody = ref('')
const textSaving = ref(false)
const textError = ref('')

function openText(): void {
  textTitle.value = ''
  textBody.value = ''
  textError.value = ''
  textOpen.value = true
}

async function submitText(): Promise<void> {
  const col = selected.value
  if (!col || textSaving.value) return
  const body = textBody.value.trim()
  if (!body) {
    textError.value = t('kb.pasteEmpty')
    return
  }
  const rawTitle = textTitle.value.trim() || t('kb.pasteDefaultTitle')
  const title = rawTitle.replace(/[\\/:*?"<>|]/g, ' ').replace(/\.md$/i, '').trim() || t('kb.pasteDefaultTitle')
  textSaving.value = true
  textError.value = ''
  try {
    await kbUploadText(col.id, `${title}.md`, body)
    textOpen.value = false
    await loadDocuments(col.id, true)
    startKBIndexWatch()
    toast.show(t('kb.pasteDone'), { type: 'success', duration: 2400 })
  } catch (e) {
    textError.value = e instanceof Error ? e.message : String(e)
  } finally {
    textSaving.value = false
  }
}

async function addFromURL(): Promise<void> {
  const col = selected.value
  if (!col) return
  const url = await dialog.prompt(t('kb.urlPrompt'), {
    title: t('kb.urlTitle'),
    placeholder: 'https://…',
    confirmText: t('kb.urlConfirm'),
    singleLine: true,
  })
  if (!url) return
  try {
    await kbAddFromURL(col.id, url.trim())
    await loadDocuments(col.id, true)
    startKBIndexWatch()
    toast.show(t('kb.urlDone'), { type: 'success', duration: 2400 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 4000 })
  }
}

// ── Collection actions ──
async function toggleAutoInject(): Promise<void> {
  if (!selected.value) return
  try {
    await setAutoInject(selected.value.id, !selected.value.auto_inject)
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function togglePin(col: KBCollection): Promise<void> {
  try {
    await setPinned(col.id, !col.pinned)
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function removeCurrent(): Promise<void> {
  const col = selected.value
  if (!col) return
  const ok = await dialog.confirm(
    t('kb.deleteCollectionConfirm', { name: col.name, count: col.doc_count }),
    { dangerous: true },
  )
  if (!ok) return
  try {
    await deleteCollection(col.id)
    toast.show(t('kb.deleted'), { type: 'success', duration: 1800 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function reindexCurrent(): Promise<void> {
  const col = selected.value
  if (!col) return
  const ok = await dialog.confirm(t('kb.reindexCollectionConfirm', { name: col.name }))
  if (!ok) return
  try {
    await reindexCollection(col.id)
    startKBIndexWatch()
    toast.show(t('kb.reindexQueued'), { type: 'success', duration: 1800 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

function backToCollections(): void {
  clearSelection()
}

// ── Move selected documents ──
const movePickerOpen = ref(false)
const moveDocIds = ref<number[]>([])

function openMovePicker(): void {
  if (selectedDocIds.value.length === 0) return
  moveDocIds.value = [...selectedDocIds.value]
  movePickerOpen.value = true
}

async function onMovePicked(collection: { id: number; name: string }): Promise<void> {
  movePickerOpen.value = false
  const ids = moveDocIds.value
  moveDocIds.value = []
  if (ids.length === 0) return
  const errors: string[] = []
  let done = 0
  for (const id of ids) {
    try {
      await moveDocument(id, collection.id)
      done++
    } catch (e) {
      errors.push(e instanceof Error ? e.message : String(e))
    }
  }
  exitSelectMode()
  await refresh(true)
  if (errors.length > 0) {
    toast.show(errors[0], { icon: 'warning', type: 'error', duration: 4000 })
  } else {
    toast.show(t('kb.moveDone', { count: done, name: collection.name }), {
      type: 'success',
      duration: 2400,
    })
  }
}

// ── Document detail ──
const drawerOpen = ref(false)
const drawerDocId = ref<number | null>(null)
const drawerChunkId = ref(0)
const documentDrawer = ref<InstanceType<typeof KBDocumentDrawer> | null>(null)

function openDocument(doc: KBDocument, chunkId = 0): void {
  drawerDocId.value = doc.id
  drawerChunkId.value = chunkId
  drawerOpen.value = true
}

function openDocumentById(documentId: number, chunkId = 0): void {
  if (!documentId) return
  drawerDocId.value = documentId
  drawerChunkId.value = chunkId
  drawerOpen.value = true
}

function closeDocument(): void {
  drawerOpen.value = false
}

async function onDocumentLoaded(detail: KBDocumentDetail): Promise<void> {
  // Opening a citation from another collection should leave the panel there
  // after the drawer closes. Same collection is a no-op (selectCollection
  // returns early), so search results behind a result click are not wiped.
  if (detail.document.collection_id !== selectedId.value) {
    await selectCollection(detail.document.collection_id)
  }
}

async function onDrawerReindex(id: number): Promise<void> {
  try {
    await reindexDocument(id)
    startKBIndexWatch()
    toast.show(t('kb.reindexDocQueued'), { type: 'success', duration: 1800 })
    documentDrawer.value?.reload()
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

async function onDrawerRefresh(id: number): Promise<void> {
  try {
    const changed = await refreshDocument(id)
    if (changed) startKBIndexWatch()
    toast.show(changed ? t('kb.refreshChanged') : t('kb.refreshUnchanged'), {
      type: changed ? 'success' : undefined,
      duration: 2200,
    })
    if (changed) documentDrawer.value?.reload()
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 4000 })
  }
}

function onQuoteChunk(payload: { text: string; title: string; location?: string }): void {
  const source = payload.location
    ? `${payload.title} · ${locationText(payload.location)}`
    : payload.title
  // Same call any panel uses; the bridge reveals the chat pane through the
  // handler App.vue registered, so there is no bespoke hand-off to keep in sync.
  chatBridge.quote({
    text: `${source}\n\n${payload.text}`,
    filePath: payload.title,
    language: 'markdown',
    startLine: 0,
    endLine: 0,
    sourceKind: 'selection',
  })
  closeDocument()
}

async function onDrawerDelete(payload: { id: number; title: string }): Promise<void> {
  const ok = await dialog.confirm(t('kb.deleteDocumentConfirm', { name: payload.title }), { dangerous: true })
  if (!ok) return
  const source = documents.value.find((d) => d.id === payload.id)?.source_path
  try {
    await deleteDocument(payload.id)
    closeDocument()
    offerUndo(source, t('kb.deleted'))
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

// ── Search ──
function cancelPendingSearch(): void {
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
}

function onSearchEnter(): void {
  // Enter means "run the selected mode now"; a pending keyword pass must not
  // start afterwards and overwrite the hybrid result.
  cancelPendingSearch()
  void runSearch()
}

function rerunSearch(): void {
  cancelPendingSearch()
  if (searchResult.value || searchError.value) void runSearch()
}

function onResultClick(event: MouseEvent, hit: KBSearchHit): void {
  // Links inside the rendered passage keep their own behaviour.
  const target = event.target as HTMLElement | null
  if (target?.closest('a, button')) return
  openDocumentById(hit.document_id, hit.chunk_id)
}

function modeLabel(mode: string): string {
  if (mode === 'fts') return t('kb.modeFts')
  if (mode === 'vector') return t('kb.modeVector')
  return t('kb.modeHybrid')
}

function tagList(tags: string | undefined): string[] {
  return (tags || '')
    .split(',')
    .map((tag) => tag.trim())
    .filter(Boolean)
    .slice(0, 4)
}

async function onDrawerTags(payload: { id: number; tags: string }): Promise<void> {
  try {
    await setDocumentTags(payload.id, payload.tags)
    toast.show(t('kb.tagsSaved'), { type: 'success', duration: 1800 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

function docErrorText(doc: KBDocument): string {
  return documentErrorText(doc.error, tf)
}

// Typing runs the cheap keyword pass; Enter (or the button) runs the selected
// mode, which may call the embedding endpoint.
let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(searchQuery, (q) => {
  cancelPendingSearch()
  const trimmed = q.trim()
  if (!trimmed) {
    if (searchResult.value || searchError.value) clearSearch()
    return
  }
  searchTimer = setTimeout(() => {
    void runSearch('fts')
  }, 260)
})

// ── Cross-panel / project wiring ──
// A citation sets this before switching the tab; the panel may mount after the
// request, hence `immediate`.
watch(
  kbPendingOpen,
  (req) => {
    if (!req) return
    consumeKBPendingOpen(req.token)
    openDocumentById(req.documentId, req.chunkId)
  },
  { immediate: true },
)

// The panel stays mounted (TabPanel uses v-show), so a project switch would
// otherwise leave the previous project's libraries on screen and 403 on add.
watch(
  () => store.state.projectRoot,
  () => {
    // A document opened from the previous project must not stay on screen.
    closeDocument()
    void reloadForProject()
  },
)

// ── Add documents ──
// The selection is owned by AttachmentPicker; this panel only applies it. A
// batch is added one file at a time (the API takes one path), so a single
// unsupported file does not throw away the rest of the batch.
const addOpen = ref(false)
const adding = ref(false)

function openAdd(): void {
  addOpen.value = true
}

function closeAdd(): void {
  addOpen.value = false
}

async function submitAdd(entries: FileEntry[]): Promise<void> {
  if (adding.value) return
  const files = entries.filter((e) => !e.isDir)
  const dirs = entries.filter((e) => e.isDir)
  if (files.length === 0 && dirs.length === 0) return
  adding.value = true
  const failed: string[] = []
  // Files and folders are counted together so one confirmation reads as one
  // outcome, whichever mix the user picked.
  const totals = { added: 0, duplicates: 0, skipped: 0, failed: 0, truncated: false }
  const skipReasons: Record<string, number> = {}
  const mergeSkipReasons = (src?: Record<string, number>): void => {
    for (const [code, count] of Object.entries(src || {})) {
      skipReasons[code] = (skipReasons[code] || 0) + count
    }
  }
  try {
    // Preview folders before pulling them in: a 500-file import is not
    // something to start without knowing what will be skipped. A failed
    // preview is advisory only — the real import reports its own errors.
    if (dirs.length > 0) {
      const preview: {
        added: number
        duplicates: number
        skipped: number
        failed: number
        truncated: boolean
        skip_reasons: Record<string, number>
      } = { added: 0, duplicates: 0, skipped: 0, failed: 0, truncated: false, skip_reasons: {} }
      let previewed = false
      for (const dir of dirs) {
        try {
          const p = await previewDirectory(dir.path)
          preview.added += p.added
          preview.duplicates += p.duplicates
          preview.skipped += p.skipped
          preview.failed += p.failed
          preview.truncated = preview.truncated || p.truncated
          for (const [code, count] of Object.entries(p.skip_reasons || {})) {
            preview.skip_reasons[code] = (preview.skip_reasons[code] || 0) + count
          }
          previewed = true
        } catch {
          // The import below will surface the same error with a real message.
        }
      }
      if (previewed) {
        const hint = skipReasonHint(preview.skip_reasons, tf)
        if (preview.added === 0) {
          closeAdd()
          toast.show(
            hint || (preview.duplicates > 0 ? t('kb.dirImportAllDuplicates') : t('kb.dirImportEmpty')),
            { icon: 'warning', type: 'error', duration: 3200 },
          )
          return
        }
        const parts = [t('kb.importPreviewMessage', preview)]
        if (preview.truncated) parts.push(t('kb.importPreviewTruncated'))
        if (hint) parts.push(hint)
        const ok = await dialog.confirm(parts.join('\n\n'), {
          title: t('kb.importPreviewTitle'),
          confirmText: t('kb.importContinue'),
        })
        // Keep the sheet open: the user may want to drop a folder or pick
        // another one instead of starting this import.
        if (!ok) return
      }
    }

    for (const entry of files) {
      try {
        await addFile(entry.path)
        totals.added++
      } catch (e) {
        totals.failed++
        failed.push(`${baseName(entry.path)}: ${e instanceof Error ? e.message : String(e)}`)
      }
    }
    for (const dir of dirs) {
      try {
        const res = await importDirectory(dir.path)
        totals.added += res.added
        totals.duplicates += res.duplicates
        totals.skipped += res.skipped
        totals.failed += res.failed
        totals.truncated = totals.truncated || res.truncated
        mergeSkipReasons(res.skip_reasons)
      } catch (e) {
        totals.failed++
        failed.push(`${baseName(dir.path)}: ${e instanceof Error ? e.message : String(e)}`)
      }
    }
  } finally {
    adding.value = false
  }

  if (totals.added > 0) startKBIndexWatch()

  if (dirs.length > 0) {
    // A folder import always closes: retrying the same folder would only
    // re-add what already landed. The summary says what happened to the rest.
    closeAdd()
    const messy = totals.skipped > 0 || totals.duplicates > 0 || totals.failed > 0
    if (totals.added === 0 && totals.skipped === 0 && totals.duplicates === 0 && totals.failed === 0) {
      toast.show(t('kb.dirImportEmpty'), { icon: 'warning', type: 'error', duration: 3200 })
      return
    }
    // "3 skipped" does not tell the user a legacy .doc only needs saving as
    // .docx; name the dominant reason. Fold it into the same toast because
    // show() replaces the visible message.
    const hint = skipReasonHint(skipReasons, tf)
    const parts = [messy ? t('kb.dirImportSummary', totals) : t('kb.dirImportDone', totals)]
    if (hint) parts.push(hint)
    toast.show(parts.join('\n\n'), {
      type: totals.added > 0 && totals.failed === 0 ? 'success' : 'error',
      duration: 4000,
    })
    if (totals.truncated) {
      toast.show(t('kb.dirImportTruncated'), { icon: 'warning', type: 'error', duration: 4000 })
    }
    return
  }

  if (failed.length === 0) {
    closeAdd()
    toast.show(t('kb.added'), { type: 'success', duration: 1800 })
    return
  }
  // Keep the sheet open on failure: the selection is still there, so the user
  // can drop the rejected file and retry without browsing again.
  const first = failed[0]
  toast.show(
    failed.length === 1 ? first : t('kb.addPartial', { first, count: failed.length - 1 }),
    { icon: 'warning', type: 'error', duration: 4000 },
  )
}

// ── Document actions ──
async function removeDoc(doc: KBDocument): Promise<void> {
  const ok = await dialog.confirm(t('kb.deleteDocumentConfirm', { name: doc.title }), { dangerous: true })
  if (!ok) return
  try {
    await deleteDocument(doc.id)
    offerUndo(doc.source_path, t('kb.deleted'))
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

/**
 * A deleted file is only recoverable from its original path (the stored copy is
 * gone with the row). Offer that when we have one; the toast click is the undo
 * affordance, and the re-add reports a clear failure if the source moved away.
 */
function offerUndo(sourcePath: string | undefined, fallbackMessage: string): void {
  const source = (sourcePath || '').trim()
  if (!source) {
    toast.show(fallbackMessage, { type: 'success', duration: 1800 })
    return
  }
  toast.show(t('kb.deletedUndo'), {
    type: 'success',
    duration: 6000,
    onClick: () => {
      void undoDelete(source)
    },
  })
}

async function undoDelete(path: string): Promise<void> {
  const col = selected.value
  if (!col) return
  try {
    await addFile(path)
    startKBIndexWatch()
    toast.show(t('kb.undoDone'), { type: 'success', duration: 2000 })
  } catch {
    toast.show(t('kb.undoFailed'), { icon: 'warning', type: 'error', duration: 3600 })
  }
}

async function reindexDoc(doc: KBDocument): Promise<void> {
  try {
    await reindexDocument(doc.id)
    startKBIndexWatch()
    toast.show(t('kb.reindexDocQueued'), { type: 'success', duration: 1800 })
  } catch (e) {
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

// ── Presentation helpers ──
// The neutral label → localized wording, highlight splitting and status
// mapping live in utils/kbFormat.ts so they can be unit-tested without mounting
// the panel (which pulls in the file picker and the drawer stack).
const tf: TFn = (key, named) => t(key, named ?? {})

function locationText(loc?: string): string {
  return formatLocation(loc, tf)
}

// ── Search result rendering ──
// The chunk text is markdown (the document IS markdown), so plain-text
// snippets showed raw `#` headings. Render the same way the session search
// drawer does, then re-apply the server's match positions as <mark> terms.
const renderedHits = computed<Record<number, string>>(() => {
  const map: Record<number, string> = {}
  for (const hit of searchResult.value?.results ?? []) {
    map[hit.chunk_id] = renderMarkdownHtml(hit.chunk_text, {
      skipEnhancements: true,
      wrapTables: false,
    })
  }
  return map
})

const hitRefs = new Map<number, HTMLElement>()
function setHitRef(id: number, el: unknown): void {
  if (el instanceof HTMLElement) hitRefs.set(id, el)
  else hitRefs.delete(id)
}

onBeforeUpdate(() => hitRefs.clear())

function applyHitHighlights(): void {
  for (const hit of searchResult.value?.results ?? []) {
    const el = hitRefs.get(hit.chunk_id)
    if (!el) continue
    highlightTermsInElement(el, termsFromRunePositions(hit.chunk_text, hit.match_positions), 'kb-mark')
  }
}

watch(searchResult, () => nextTick(applyHitHighlights))

function statusKindOf(doc: KBDocument): string {
  return statusKind(doc, vectorEnabled.value)
}

function statusLabelOf(doc: KBDocument): string {
  return statusLabel(doc, tf, vectorEnabled.value)
}

// A failed status refresh must not hide the panel; the collections call is the
// authoritative "is this usable" signal.
watch(available, (on) => {
  if (!on) appLog.w('KnowledgeBase', 'knowledge base is not available')
})

// Expose for tests.
defineExpose({
  formatLocation: locationText,
  statusKind: statusKindOf,
  statusLabel: statusLabelOf,
})
</script>

<style scoped>
/* ── Panel shell ── */
.kb-panel {
  position: relative; /* anchors DropOverlay's inset: 0 */
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: var(--panel-page-bg);
}

/* The shared bar is an inline flow child (file manager / terminal use it the
   same way); give it the panel's gutters so it does not touch the edges. */
.kb-upload-bar {
  margin: var(--space-3) var(--space-4) 0;
}

.kb-panel.is-drop-target {
  outline: 2px dashed var(--accent-color);
  outline-offset: calc(-6 * var(--dp));
}

.kb-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  height: var(--header-height);
  padding: 0 var(--space-2) 0 var(--space-6);
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color);
  flex: none;
}

.kb-title-line {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.kb-title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  white-space: nowrap;
}

.kb-count {
  flex: none;
  padding: 1px var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
}

.kb-header-actions {
  flex: 1 1 auto;
}

/* Only what the shared ActionBar row rules do NOT set. `display: flex` is one
   of them and it is not optional: a <button> defaults to inline-block, so the
   icon and the label line up on the text baseline and the icon sits visibly
   high next to the word once the bar is wide enough to show labels. */
.header-btn {
  border: none;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}

.kb-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.kb-split {
  flex: 1;
  min-height: 0;
}

/* ── Collection rail ── */
.kb-rail {
  height: 100%;
  overflow-y: auto;
  padding: var(--panel-body-pad);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  background: var(--bg-primary);
}

.kb-group {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.kb-group-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--space-2);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.kb-group-count {
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

.kb-rail-tools {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.kb-col-search {
  flex: 1;
  min-width: 0;
}

.kb-col-sort {
  flex: none;
  max-width: calc(92 * var(--dp));
}

.kb-col-row {
  display: flex;
  align-items: stretch;
  gap: 2px;
}

.kb-col-row .kb-col {
  flex: 1;
  min-width: 0;
}

.kb-col-pin {
  flex: none;
  width: calc(26 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.kb-col-pin.is-on {
  color: var(--accent-color);
}

@media (hover: hover) {
  .kb-col-pin {
    opacity: 0;
  }
  .kb-col-row:hover .kb-col-pin,
  .kb-col-pin.is-on,
  .kb-col-pin:focus-visible {
    opacity: 1;
  }
}

.kb-col {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-3) var(--space-4);
  border: 1px solid transparent;
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: background var(--duration-fast), border-color var(--duration-fast);
}

@media (hover: hover) {
  .kb-col:hover {
    border-color: var(--border-color);
    background: color-mix(in srgb, var(--text-primary) 4%, var(--bg-secondary));
  }
}

.kb-col.is-active {
  border-color: color-mix(in srgb, var(--accent-color) 45%, transparent);
  background: color-mix(in srgb, var(--accent-color) 10%, var(--bg-secondary));
}

.kb-col-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: calc(30 * var(--dp));
  height: calc(30 * var(--dp));
  border-radius: var(--radius-sm);
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}

.kb-col-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
}

.kb-col-name {
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-col-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px var(--space-2);
  min-width: 0;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

.kb-col-flag {
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

.kb-col-flag.is-bad {
  color: var(--color-red);
}

.kb-col-off {
  color: var(--color-orange);
}

.kb-col-chevron {
  flex: none;
  color: var(--text-muted);
}

/* ── Detail pane ── */
.kb-main {
  height: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
}

/* Identity + actions read as one card, like the document rows below: the
   header used to be bare text on the panel surface, separated by hairlines,
   which made it float above the list instead of belonging to it. */
.kb-main-head {
  display: flex;
  flex-direction: column;
  margin: var(--space-4) var(--panel-body-pad) 0;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  flex: none;
  overflow: hidden;
}

.kb-main-title-line {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
  padding: var(--space-4) var(--space-4) 0;
}

.kb-main-title-text {
  min-width: 0;
  flex: 1;
}

.kb-main-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: calc(30 * var(--dp));
  height: calc(30 * var(--dp));
  border-radius: calc(9 * var(--dp));
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}

.kb-main-name {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
  color: var(--text-primary);
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
}

.kb-main-name-text {
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-main-desc {
  margin-top: var(--space-1);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  line-height: 1.45;
}

.kb-scope {
  flex: none;
  display: inline-flex;
  align-items: center;
  height: calc(17 * var(--dp));
  padding: 0 var(--space-2);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-medium);
}

.kb-scope--global {
  color: var(--accent-color);
  border-color: color-mix(in srgb, var(--accent-color) 40%, transparent);
}

/* Whether chat retrieval includes this collection. A property of the
   collection, so it sits beside the scope tag rather than in the action bar. */
.kb-inject-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  flex: none;
  height: calc(17 * var(--dp));
  padding: 0 var(--space-2);
  border: 1px solid color-mix(in srgb, var(--accent-color) 35%, transparent);
  border-radius: var(--radius-full);
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  color: var(--accent-color);
  font-size: var(--font-size-2xs);
  font-family: inherit;
  cursor: pointer;
}

.kb-inject-chip.is-off {
  border-color: var(--border-color);
  background: transparent;
  color: var(--text-muted);
}

/* State reads as a dot, not a rule: a 1px vertical line at this size looks like
   a caret left in the label. */
.kb-inject-dot {
  flex: none;
  width: calc(4 * var(--dp));
  height: calc(4 * var(--dp));
  border-radius: 50%;
  background: currentColor;
  opacity: 0.7;
}

.kb-inject-state {
  opacity: 0.8;
}

.kb-main-actions {
  width: 100%;
  padding: var(--space-3) var(--space-4) var(--space-4);
}

.kb-back {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  padding: 0;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  cursor: pointer;
}

.kb-back:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* ── Health line ── */
.kb-health {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-5);
  border-bottom: 1px solid var(--border-color);
  flex: none;
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
}

.kb-health-total {
  font-variant-numeric: tabular-nums;
}

.kb-health-chip {
  padding: 1px var(--space-2);
  border: 1px solid transparent;
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-2xs);
  font-variant-numeric: tabular-nums;
  cursor: pointer;
}

.kb-health-chip.is-bad {
  color: var(--color-red);
}

.kb-health-chip.is-on {
  border-color: currentColor;
  background: transparent;
}

.kb-health-spacer {
  flex: 1 1 auto;
}

.kb-health-select {
  height: calc(24 * var(--dp));
  padding: 0 var(--space-1);
  font-size: var(--font-size-2xs);
}

.kb-load-more {
  display: block;
  margin: var(--space-3) auto 0;
}

/* ── Search ── */
.kb-search-wrap {
  flex: none;
  background: var(--bg-primary);
}

.kb-search-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-5);
  border-bottom: 1px solid var(--border-color);
  flex: none;
}

.kb-search {
  flex: 1 1 calc(170 * var(--dp));
  min-width: calc(130 * var(--dp));
}

.kb-search-controls {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: 0 0 auto;
}

.kb-select {
  height: calc(30 * var(--dp));
  padding: 0 var(--space-2);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-xs);
}

/* ── Result / document bodies ── */
.kb-results,
.kb-docs {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: var(--panel-body-pad);
}

.kb-results-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  margin-bottom: var(--space-3);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

.kb-results-count {
  font-variant-numeric: tabular-nums;
}

.kb-results-mode {
  padding: 1px var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
}

.kb-result-list,
.kb-doc-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

/* ── Search result card ── */
.kb-result {
  padding: var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  cursor: pointer;
}

@media (hover: hover) {
  .kb-result:hover {
    border-color: color-mix(in srgb, var(--accent-color) 40%, var(--border-color));
  }
}

.kb-result:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}

.kb-result-open {
  flex: none;
  color: var(--text-muted);
}

.kb-result-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.kb-result-title {
  min-width: 0;
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-result-loc {
  flex: none;
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-2xs);
}

.kb-result-col {
  min-width: 0;
  margin-left: auto;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-result-text {
  margin-top: var(--space-2);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.55;
  overflow-wrap: anywhere;
  /* A chunk can be a whole document section; cap the card and scroll inside
     instead of letting one result push the others (and the pane) down. */
  max-height: calc(320 * var(--dp));
  overflow-y: auto;
  overscroll-behavior: contain;
}

/* The global .markdown-body rules set reading-pane rhythm; this card is
   compact, so pull block margins in and drop the heading rule line. */
.kb-result-text :deep(h1),
.kb-result-text :deep(h2),
.kb-result-text :deep(h3),
.kb-result-text :deep(h4),
.kb-result-text :deep(p),
.kb-result-text :deep(ul),
.kb-result-text :deep(ol) {
  margin: calc(4 * var(--dp)) 0;
  font-size: inherit;
  line-height: inherit;
  color: inherit;
}

.kb-result-text :deep(h1),
.kb-result-text :deep(h2),
.kb-result-text :deep(h3),
.kb-result-text :deep(h4) {
  font-weight: var(--font-weight-semibold);
  border: 0;
  padding: 0;
}

/* The <mark> elements are injected into rendered HTML, so a scoped rule
   without :deep() would never match them. */
.kb-result-text :deep(mark.kb-mark) {
  background: color-mix(in srgb, var(--accent-color) 24%, transparent);
  color: inherit;
  border-radius: 2px;
  padding: 0 1px;
}

/* ── Document rows ── */
.kb-doc-list {
  gap: 0;
}

.kb-doc {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-2);
  border-bottom: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  transition: background var(--duration-fast);
}

.kb-doc:last-child {
  border-bottom: 0;
}

/* The clickable body: a real button so keyboard users get the same affordance,
   reset to look exactly like the old row. */
.kb-doc-open {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  flex: 1;
  min-width: 0;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.kb-doc-open:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 2px;
  border-radius: var(--radius-sm);
}

.kb-doc-check {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: calc(18 * var(--dp));
  height: calc(18 * var(--dp));
  margin-top: calc(7 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-xs);
  color: #fff;
}

.kb-doc-check.is-on {
  border-color: var(--accent-color);
  background: var(--accent-color);
}

.kb-doc.is-selected {
  background: color-mix(in srgb, var(--accent-color) 7%, transparent);
}

.kb-select-count {
  flex: none;
  padding: 1px var(--space-2);
  border-radius: var(--radius-full);
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
  color: var(--accent-color);
  font-size: var(--font-size-2xs);
  font-variant-numeric: tabular-nums;
}

.kb-doc-tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
  margin-top: 2px;
}

.kb-doc-tag {
  padding: 0 var(--space-2);
  border-radius: var(--radius-full);
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  color: var(--accent-color);
  font-size: var(--font-size-2xs);
}

.kb-health-tag {
  width: calc(96 * var(--dp));
}

.kb-doc-source {
  margin-top: 2px;
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (hover: hover) {
  .kb-doc:hover {
    background: color-mix(in srgb, var(--text-primary) 3%, transparent);
  }
}

.kb-doc-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: calc(32 * var(--dp));
  height: calc(32 * var(--dp));
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-secondary);
}

.kb-doc-icon--pdf { color: var(--color-red); background: color-mix(in srgb, var(--color-red) 10%, transparent); }
.kb-doc-icon--docx { color: var(--color-info); background: color-mix(in srgb, var(--color-info) 10%, transparent); }
.kb-doc-icon--xlsx { color: var(--color-green); background: color-mix(in srgb, var(--color-green) 10%, transparent); }
.kb-doc-icon--pptx { color: var(--color-orange); background: color-mix(in srgb, var(--color-orange) 10%, transparent); }
.kb-doc-icon--md { color: var(--color-purple); background: color-mix(in srgb, var(--color-purple) 10%, transparent); }

.kb-doc-main {
  flex: 1;
  min-width: 0;
}

.kb-doc-name-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.kb-doc-name {
  min-width: 0;
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kb-status {
  flex: none;
  padding: 1px var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  white-space: nowrap;
}

.kb-status--ready {
  background: color-mix(in srgb, var(--color-green) 12%, transparent);
  color: var(--color-green);
}

.kb-status--fts {
  background: color-mix(in srgb, var(--color-orange) 12%, transparent);
  color: var(--color-orange);
}

.kb-status--failed {
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
  color: var(--color-red);
}

.kb-status--working,
.kb-status--pending {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  color: var(--accent-color);
}

.kb-doc-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-2);
  margin-top: var(--space-1);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}

.kb-sep {
  opacity: 0.6;
}

.kb-muted {
  color: var(--text-muted);
}

.kb-stale {
  color: var(--color-orange);
}

.kb-warn {
  color: var(--color-orange);
}

.kb-doc-error {
  margin-top: var(--space-1);
  color: var(--color-red);
  font-size: var(--font-size-xs);
  overflow-wrap: anywhere;
}

.kb-doc-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex: none;
  opacity: 0;
  transition: opacity var(--duration-fast);
}

.kb-doc:hover .kb-doc-actions,
.kb-doc:focus-within .kb-doc-actions {
  opacity: 1;
}

@media (hover: none) {
  .kb-doc-actions { opacity: 1; }
}

.kb-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  padding: 0;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.kb-icon-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.kb-icon-btn.is-danger:hover {
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  color: var(--color-red);
}

/* ── Small helpers ── */
.kb-hint {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-5);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}

.kb-hint--error {
  color: var(--color-red);
}

.kb-link {
  border: 0;
  background: transparent;
  color: var(--accent-color);
  font-size: var(--font-size-xs);
  cursor: pointer;
}

.kb-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-8);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  text-align: center;
}

.kb-empty-icon {
  opacity: 0.6;
}

.kb-empty-desc {
  max-width: calc(320 * var(--dp));
  font-size: var(--font-size-xs);
}

/* ── Create form ── */
/* ── Create form ──
   The modal body has NO inset of its own (modal-card.css); every consumer
   supplies it. 12/16dp is the convention the other form dialogs use. */
.kb-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  padding: var(--space-6) var(--space-7);
}

.kb-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.kb-label {
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
}

.kb-input {
  width: 100%;
  box-sizing: border-box;
  height: calc(34 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  font-family: inherit;
  outline: none;
  transition: border-color var(--duration-base);
}

.kb-input:focus {
  border-color: var(--accent-color);
}

.kb-input::placeholder {
  color: var(--text-muted);
}

.kb-paste-body {
  min-height: calc(160 * var(--dp));
  resize: vertical;
}

.kb-textarea {
  height: auto;
  min-height: calc(76 * var(--dp));
  padding: var(--space-4) var(--space-5);
  line-height: var(--line-height-normal);
  resize: vertical;
}

.kb-seg {
  display: flex;
  gap: var(--space-1);
  padding: var(--space-1);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
}

.kb-seg-btn {
  flex: 1;
  min-width: 0;
  height: calc(30 * var(--dp));
  padding: 0 var(--space-2);
  border: 0;
  border-radius: calc(var(--radius-sm) - 1px);
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  font-family: inherit;
  cursor: pointer;
}

.kb-seg-btn.is-on {
  background: var(--accent-color);
  color: var(--bg-primary);
  font-weight: var(--font-weight-medium);
}

.kb-seg-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.kb-hint-text {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
}

.kb-form-error {
  color: var(--color-red);
  font-size: var(--font-size-sm);
}

</style>
