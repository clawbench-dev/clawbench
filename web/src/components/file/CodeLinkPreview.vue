<template>
  <!-- Touch Device: BottomSheet mode -->
  <BottomSheet
    v-if="preview.visible.value && preview.mode.value === 'sheet'"
    :open="preview.visible.value"
    auto
    :handle-only="true"
    panel-class="code-preview-sheet-panel"
    class="code-preview-sheet"
    :title="sheetTitle"
    @close="preview.close()"
  >
    <div class="code-preview-sheet-body">
      <!-- Mobile Header: Row 1 & Row 2 (Directly below 12px drag handle) -->
      <div class="code-preview-sheet-header">
        <!-- Mobile Row 1: File Path (Up to 2 lines, click to copy) + Copy Path Btn -->
        <div
          class="code-preview-sheet-row1"
          :title="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
          @click="handleCopyPath"
        >
          <div class="code-preview-sheet-title-group code-preview-sheet-path-box">
            <span v-if="fileDirPath" class="code-preview-sheet-dir">{{ fileDirPath }}/</span>
            <span class="code-preview-sheet-file">
              <span class="code-preview-sheet-filename">{{ fileBaseName }}</span>
              <span v-if="lineRangeText" class="code-preview-line-ref">{{ lineRangeText }}</span>
            </span>
          </div>

          <div class="code-preview-sheet-row1-actions">
            <button
              class="code-preview-btn icon-only copy-path-btn"
              :class="{ 'is-copied': isPathCopied }"
              :title="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
              :aria-label="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
              @click.stop="handleCopyPath"
            >
              <svg v-if="isPathCopied" viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="20 6 9 17 4 12" />
              </svg>
              <svg v-else viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
                <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
              </svg>
            </button>
          </div>
        </div>

        <!-- Mobile Row 2: File Meta Info + Tools (Search, Wrap, Copy Code, Reveal in Tree) -->
        <div class="code-preview-sheet-row2">
          <div class="code-preview-sheet-meta-info">
            <span v-if="contextMeta">{{ contextMeta }}</span>
          </div>

          <div class="code-preview-sheet-tools">
            <!-- Search in Preview -->
            <button
              class="code-preview-btn icon-only"
              :class="{ 'is-active': isSearchOpen }"
              :title="t('file.codePreview.findInPreview')"
              :aria-label="t('file.codePreview.findInPreview')"
              @click="toggleSearch"
            >
              <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="11" cy="11" r="8" />
                <line x1="21" y1="21" x2="16.65" y2="16.65" />
              </svg>
            </button>
            <!-- Word Wrap Toggle -->
            <button
              class="code-preview-btn icon-only"
              :class="{ 'is-active': isWordWrap }"
              :title="isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap')"
              :aria-label="isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap')"
              @click="toggleWordWrap"
            >
              <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M4 6h16M4 12h10a3 3 0 0 1 3 3v0a3 3 0 0 1-3 3H11m0 0l3-3m-3 3l3 3M4 18h4" />
              </svg>
            </button>
            <!-- Copy Code -->
            <button
              class="code-preview-btn icon-only"
              :class="{ 'is-copied': copied }"
              :title="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
              :aria-label="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
              @click="handleCopy"
            >
              <svg v-if="copied" viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="20 6 9 17 4 12" />
              </svg>
              <svg v-else viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
              </svg>
            </button>
            <!-- Reveal in Tree (Folder) -->
            <button
              class="code-preview-btn icon-only"
              :title="t('file.codePreview.revealInTree')"
              :aria-label="t('file.codePreview.revealInTree')"
              @click="handleRevealInTree"
            >
              <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
              </svg>
            </button>
          </div>
        </div>
      </div>
      <!-- Notices -->
      <div v-if="preview.isLargeFile.value" class="code-preview-notice notice-warning">
        {{ t('file.codePreview.largeFileNotice') }}
      </div>
      <div v-if="preview.slicedCode.value?.lineOutOfRange" class="code-preview-notice notice-warning">
        {{ t('file.codePreview.lineOutOfRange') }}
      </div>
      <div v-if="preview.slicedCode.value?.renderTruncated" class="code-preview-notice notice-info">
        {{ t('file.codePreview.truncatedNotice', { n: 200, size: '512KB' }) }}
      </div>

      <!-- Mobile In-Preview Search Bar -->
      <div v-if="isSearchOpen" class="code-preview-search-bar">
        <input
          ref="sheetSearchInputRef"
          v-model="searchQuery"
          class="code-preview-search-input"
          :placeholder="t('file.codePreview.findPlaceholder')"
          @keydown.enter.exact.prevent="findNext"
          @keydown.shift.enter.exact.prevent="findPrev"
          @keydown.esc.prevent="closeSearch"
        />
        <span class="code-preview-search-count">
          {{ searchQuery ? (totalMatches > 0 ? t('file.codePreview.matchIndex', { current: activeMatchIndex + 1, total: totalMatches }) : t('file.codePreview.noMatches')) : '' }}
        </span>
        <button class="code-preview-btn" :disabled="totalMatches === 0" :title="t('file.codePreview.findPrev')" @click="findPrev">
          ▲
        </button>
        <button class="code-preview-btn" :disabled="totalMatches === 0" :title="t('file.codePreview.findNext')" @click="findNext">
          ▼
        </button>
        <button class="code-preview-btn" :title="t('file.codePreview.findClose')" @click="closeSearch">
          &times;
        </button>
      </div>

      <!-- Content Area -->
      <CodePreviewBody
        ref="bodyRef"
        :status="preview.status.value"
        :error-message-text="errorMessageText"
        :error-code="preview.errorCode.value"
        :is-word-wrap="isWordWrap"
        :code-lines="codeLines"
        :matching-line-indices="matchingLineIndices"
        :active-match-index="activeMatchIndex"
        :remaining-above="remainingAbove"
        :remaining-below="remainingBelow"
        :step-above="stepAbove"
        :step-below="stepBelow"
        :expand-above-lines="expandAbove"
        :expand-below-lines="expandBelow"
        @refresh="preview.refresh()"
      />
    </div>

    <!-- Bottom Action Bar (Thumb area - Left-hand optimized) -->
    <template #footer>
      <div class="code-preview-sheet-footer">
        <!-- Collapse Sheet (Leftmost: natural left-hand thumb rest) -->
        <button
          class="code-preview-footer-btn icon-btn collapse-btn"
          :title="t('file.codePreview.collapse')"
          :aria-label="t('file.codePreview.collapse')"
          @click="preview.close()"
        >
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="6 9 12 15 18 9" />
          </svg>
        </button>

        <!-- Refresh Code (Next to collapse: easy reach for left thumb) -->
        <button
          class="code-preview-footer-btn icon-btn refresh-btn"
          :class="{ 'is-loading': preview.status.value === 'loading' }"
          :title="t('file.codePreview.refresh')"
          :aria-label="t('file.codePreview.refresh')"
          @click="preview.refresh()"
        >
          <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
          </svg>
        </button>

        <!-- Quote to Chat -->
        <button
          class="code-preview-footer-btn action-btn quote-btn"
          :title="t('file.codePreview.quoteToChat')"
          @click="handleQuoteToChat"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
          </svg>
          <span>{{ t('file.codePreview.quoteToChat') }}</span>
        </button>

        <!-- Open Full / View Details -->
        <button
          v-if="preview.errorCode.value === 'too-large'"
          class="code-preview-footer-btn action-btn primary-btn"
          @click="handleViewDetails"
        >
          {{ t('file.codePreview.viewDetails') }}
        </button>
        <button
          v-else
          class="code-preview-footer-btn action-btn primary-btn"
          @click="preview.openFull()"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6M15 3h6v6M10 14L21 3" />
          </svg>
          <span>{{ t('file.codePreview.openFull') }}</span>
        </button>
      </div>
    </template>
  </BottomSheet>

  <!-- Desktop Floating: Teleport to body -->
  <Teleport v-else-if="preview.visible.value && preview.mode.value !== 'sheet'" to="body">
    <div
      ref="cardRef"
      class="code-link-preview-floating"
      :class="{ 'is-dragging': isDraggingCard }"
      role="dialog"
      :aria-label="t('file.codePreview.title')"
      :style="cardStyle"
      tabindex="-1"
      @pointerenter="preview.onCardPointerEnter"
      @pointerleave="onCardPointerLeave"
      @focusin="preview.onCardFocusIn"
      @focusout="preview.onCardFocusOut"
      @keydown.esc.stop.prevent="handleEscape"
    >
      <!-- Custom Fast Tooltip -->
      <Transition name="code-preview-tooltip-fade">
        <div
          v-if="tooltipState.visible && !isDraggingCard && tooltipState.text"
          class="code-preview-tooltip"
          :style="tooltipStyle"
          role="tooltip"
          aria-hidden="true"
        >
          {{ tooltipState.text }}
        </div>
      </Transition>

      <!-- Titlebar / Drag Handle -->
      <!-- Titlebar / Drag Handle: Row 1 (File Path + Copy Path Button) -->
      <div class="code-preview-header" @pointerdown="onDragPointerDown">
        <div
          class="code-preview-title"
          :data-tooltip="fullPathTooltipText"
          @pointerenter="showTooltip($event, fullPathTooltipText, { isFast: true })"
          @pointerleave="hideTooltip()"
        >
          <div class="code-preview-title-path">
            <span v-if="fileDirPath" class="code-preview-title-dir">{{ fileDirPath }}/</span>
            <span class="code-preview-title-file">
              <span class="code-preview-filename">{{ fileBaseName }}</span>
              <span v-if="lineRangeText" class="code-preview-line-ref">{{ lineRangeText }}</span>
            </span>
          </div>
        </div>

        <div class="code-preview-header-actions" @pointerdown.stop>
          <button
            class="code-preview-btn copy-path-btn"
            :class="{ 'is-copied': isPathCopied }"
            :title="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
            :aria-label="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
            :data-tooltip="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
            @pointerenter="showTooltip($event, isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath'))"
            @pointerleave="hideTooltip()"
            @click.stop="handleCopyPath"
          >
            <svg v-if="isPathCopied" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="20 6 9 17 4 12" />
            </svg>
            <svg v-else viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
              <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
            </svg>
          </button>
        </div>
      </div>

      <!-- Row 2: File Meta & Remaining Action Tools -->
      <div class="code-preview-meta" @pointerdown="onDragPointerDown">
        <div class="code-preview-meta-info">
          <span>{{ contextMeta || t('file.codePreview.title') }}</span>
        </div>

        <div class="code-preview-actions" @pointerdown.stop>
          <!-- Viewer Tools: Find, Wrap, Refresh -->
          <button
            ref="firstActionBtnRef"
            class="code-preview-btn"
            :class="{ 'is-active': isSearchOpen }"
            :title="t('file.codePreview.findInPreview')"
            :aria-label="t('file.codePreview.findInPreview')"
            :data-tooltip="t('file.codePreview.findInPreview')"
            @pointerenter="showTooltip($event, t('file.codePreview.findInPreview'))"
            @pointerleave="hideTooltip()"
            @click="toggleSearch"
          >
            <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8" />
              <line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>
          </button>
          <button
            class="code-preview-btn"
            :class="{ 'is-active': isWordWrap }"
            :title="isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap')"
            :aria-label="isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap')"
            :aria-pressed="isWordWrap"
            :data-tooltip="isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap')"
            @pointerenter="showTooltip($event, isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap'))"
            @pointerleave="hideTooltip()"
            @click="toggleWordWrap"
          >
            <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M4 6h16M4 12h10a3 3 0 0 1 3 3v0a3 3 0 0 1-3 3H11m0 0l3-3m-3 3l3 3M4 18h4" />
            </svg>
          </button>
          <button
            class="code-preview-btn"
            :title="t('file.codePreview.refresh')"
            :aria-label="t('file.codePreview.refresh')"
            :data-tooltip="t('file.codePreview.refresh')"
            @pointerenter="showTooltip($event, t('file.codePreview.refresh'))"
            @pointerleave="hideTooltip()"
            @click="preview.refresh()"
          >
            <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
            </svg>
          </button>

          <span class="code-preview-actions-divider" />

          <!-- Actions: Quote, Copy Code, Reveal -->
          <button
            class="code-preview-btn"
            :title="t('file.codePreview.quoteToChat')"
            :aria-label="t('file.codePreview.quoteToChat')"
            :data-tooltip="t('file.codePreview.quoteToChat')"
            @pointerenter="showTooltip($event, t('file.codePreview.quoteToChat'))"
            @pointerleave="hideTooltip()"
            @click="handleQuoteToChat"
          >
            <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
            </svg>
          </button>
          <button
            class="code-preview-btn"
            :class="{ 'is-copied': copied }"
            :title="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
            :aria-label="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
            :data-tooltip="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
            @pointerenter="showTooltip($event, copied ? t('file.codePreview.copied') : t('file.codePreview.copy'))"
            @pointerleave="hideTooltip()"
            @click="handleCopy"
          >
            <svg v-if="copied" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="20 6 9 17 4 12" />
            </svg>
            <svg v-else viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
              <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
            </svg>
          </button>
          <button
            class="code-preview-btn"
            :title="t('file.codePreview.revealInTree')"
            :aria-label="t('file.codePreview.revealInTree')"
            :data-tooltip="t('file.codePreview.revealInTree')"
            @pointerenter="showTooltip($event, t('file.codePreview.revealInTree'))"
            @pointerleave="hideTooltip()"
            @click="handleRevealInTree"
          >
            <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
            </svg>
          </button>

          <span class="code-preview-actions-divider" />

          <!-- Window Controls: Pin, Open Full, Close -->
          <button
            class="code-preview-btn"
            :class="{ 'is-pinned': preview.isPinned.value }"
            :aria-pressed="preview.isPinned.value"
            :title="preview.isPinned.value ? t('file.codePreview.unpin') : t('file.codePreview.pin')"
            :aria-label="preview.isPinned.value ? t('file.codePreview.unpin') : t('file.codePreview.pin')"
            :data-tooltip="preview.isPinned.value ? t('file.codePreview.unpin') : t('file.codePreview.pin')"
            @pointerenter="showTooltip($event, preview.isPinned.value ? t('file.codePreview.unpin') : t('file.codePreview.pin'))"
            @pointerleave="hideTooltip()"
            @click="handleTogglePin"
          >
            <svg
              viewBox="0 0 24 24"
              width="12"
              height="12"
              :fill="preview.isPinned.value ? 'currentColor' : 'none'"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path d="M12 17v5" />
              <path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z" />
            </svg>
          </button>
          <button
            v-if="preview.errorCode.value === 'too-large'"
            class="code-preview-btn"
            :title="t('file.codePreview.viewDetails')"
            :aria-label="t('file.codePreview.viewDetails')"
            :data-tooltip="t('file.codePreview.viewDetails')"
            @pointerenter="showTooltip($event, t('file.codePreview.viewDetails'))"
            @pointerleave="hideTooltip()"
            @click="handleViewDetails"
          >
            {{ t('file.codePreview.viewDetails') }}
          </button>
          <button
            v-else
            class="code-preview-btn"
            :title="t('file.codePreview.openFull')"
            :aria-label="t('file.codePreview.openFull')"
            :data-tooltip="t('file.codePreview.openFull')"
            @pointerenter="showTooltip($event, t('file.codePreview.openFull'))"
            @pointerleave="hideTooltip()"
            @click="preview.openFull()"
          >
            <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6M15 3h6v6M10 14L21 3" />
            </svg>
          </button>
          <button
            class="code-preview-btn close"
            :title="t('file.codePreview.close')"
            :aria-label="t('file.codePreview.close')"
            :data-tooltip="t('file.codePreview.close')"
            @pointerenter="showTooltip($event, t('file.codePreview.close'))"
            @pointerleave="hideTooltip()"
            @click="preview.close()"
          >
            &times;
          </button>
        </div>
      </div>

      <!-- Desktop In-Preview Search Bar -->
      <div v-if="isSearchOpen" class="code-preview-search-bar" @pointerdown.stop>
        <input
          ref="searchInputRef"
          v-model="searchQuery"
          class="code-preview-search-input"
          :placeholder="t('file.codePreview.findPlaceholder')"
          @keydown.enter.exact.prevent="findNext"
          @keydown.shift.enter.exact.prevent="findPrev"
          @keydown.esc.prevent="closeSearch"
        />
        <span class="code-preview-search-count">
          {{ searchQuery ? (totalMatches > 0 ? t('file.codePreview.matchIndex', { current: activeMatchIndex + 1, total: totalMatches }) : t('file.codePreview.noMatches')) : '' }}
        </span>
        <button
          class="code-preview-btn"
          :disabled="totalMatches === 0"
          :title="t('file.codePreview.findPrev')"
          :data-tooltip="t('file.codePreview.findPrev')"
          @pointerenter="showTooltip($event, t('file.codePreview.findPrev'))"
          @pointerleave="hideTooltip()"
          @click="findPrev"
        >
          ▲
        </button>
        <button
          class="code-preview-btn"
          :disabled="totalMatches === 0"
          :title="t('file.codePreview.findNext')"
          :data-tooltip="t('file.codePreview.findNext')"
          @pointerenter="showTooltip($event, t('file.codePreview.findNext'))"
          @pointerleave="hideTooltip()"
          @click="findNext"
        >
          ▼
        </button>
        <button
          class="code-preview-btn"
          :title="t('file.codePreview.findClose')"
          :data-tooltip="t('file.codePreview.findClose')"
          @pointerenter="showTooltip($event, t('file.codePreview.findClose'))"
          @pointerleave="hideTooltip()"
          @click="closeSearch"
        >
          &times;
        </button>
      </div>

      <!-- Notices -->
      <div class="code-preview-notices">
        <div v-if="preview.isLargeFile.value" class="code-preview-notice notice-warning">
          {{ t('file.codePreview.largeFileNotice') }}
        </div>
        <div v-if="preview.slicedCode.value?.lineOutOfRange" class="code-preview-notice notice-warning">
          {{ t('file.codePreview.lineOutOfRange') }}
        </div>
        <div v-if="preview.slicedCode.value?.renderTruncated" class="code-preview-notice notice-info">
          {{ t('file.codePreview.truncatedNotice', { n: 200, size: '512KB' }) }}
        </div>
      </div>

      <!-- Body / Scroll pane -->
      <CodePreviewBody
        ref="bodyRef"
        :status="preview.status.value"
        :error-message-text="errorMessageText"
        :error-code="preview.errorCode.value"
        :is-word-wrap="isWordWrap"
        :code-lines="codeLines"
        :matching-line-indices="matchingLineIndices"
        :active-match-index="activeMatchIndex"
        :remaining-above="remainingAbove"
        :remaining-below="remainingBelow"
        :step-above="stepAbove"
        :step-below="stepBelow"
        :expand-above-lines="expandAbove"
        :expand-below-lines="expandBelow"
        @refresh="preview.refresh()"
      />
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted, onBeforeUnmount, nextTick, inject, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import CodePreviewBody from '@/components/file/CodePreviewBody.vue'
import { highlightCode } from '@/utils/globals'
import { getFileType } from '@/utils/fileType'
import { clampCardPosition, splitHighlightedHtml, getAppHeaderBottom } from '@/utils/codeLinkPreview'
import { toFixedCSS, useSettingsConfig, getZoomedViewport } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'
import { useChatContext } from '@/composables/useChatContext'
import { store } from '@/stores/app'
import { dirName } from '@/utils/path'
import type { useCodeLinkPreview } from '@/composables/useCodeLinkPreview'
import '@/assets/code-link-preview.css'

const props = defineProps<{
  preview: ReturnType<typeof useCodeLinkPreview>
}>()

const { t } = useI18n()
const { localConfig } = useSettingsConfig()
const switchTab = inject<(tab: string) => void>('switchTab', () => {})
const activeTab = inject<Ref<string> | undefined>('activeTab', undefined)

const STORAGE_KEY_WRAP = 'clawbench:code-preview-word-wrap'

const cardRef = ref<HTMLElement | null>(null)
// The currently-rendered code pane (sheet mode OR floating mode — only one
// renders at a time), typed as the exposed instance of CodePreviewBody.
const bodyRef = ref<{ scrollToTargetLine: () => void; scrollLineIntoView: (i: number) => void; scrollContainer: HTMLElement | null } | null>(null)
const firstActionBtnRef = ref<HTMLButtonElement | null>(null)
const copied = ref(false)
const isWordWrap = ref<boolean>(true)

try {
  const saved = localStorage.getItem(STORAGE_KEY_WRAP)
  if (saved !== null) {
    isWordWrap.value = saved === 'true'
  }
} catch {
  // ignore
}

// In-preview Search
const isSearchOpen = ref(false)
const searchQuery = ref('')
const activeMatchIndex = ref(0)
const searchInputRef = ref<HTMLInputElement | null>(null)
const sheetSearchInputRef = ref<HTMLInputElement | null>(null)

const toggleSearch = () => {
  isSearchOpen.value = !isSearchOpen.value
  if (isSearchOpen.value) {
    nextTick(() => {
      const el = props.preview.mode.value === 'sheet' ? sheetSearchInputRef.value : searchInputRef.value
      el?.focus()
      el?.select()
    })
  } else {
    searchQuery.value = ''
    activeMatchIndex.value = 0
  }
}

const closeSearch = () => {
  isSearchOpen.value = false
  searchQuery.value = ''
  activeMatchIndex.value = 0
}

const matchingLineIndices = computed<number[]>(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return []
  const code = props.preview.slicedCode.value?.code || ''
  const lines = code.split('\n')
  const indices: number[] = []
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].toLowerCase().includes(q)) {
      indices.push(i)
    }
  }
  return indices
})

const totalMatches = computed(() => matchingLineIndices.value.length)

const scrollToMatch = (matchIdx: number) => {
  const lineIdx = matchingLineIndices.value[matchIdx]
  if (lineIdx === undefined) return
  bodyRef.value?.scrollLineIntoView(lineIdx)
}

const findNext = () => {
  if (totalMatches.value === 0) return
  activeMatchIndex.value = (activeMatchIndex.value + 1) % totalMatches.value
  scrollToMatch(activeMatchIndex.value)
}

const findPrev = () => {
  if (totalMatches.value === 0) return
  activeMatchIndex.value = (activeMatchIndex.value - 1 + totalMatches.value) % totalMatches.value
  scrollToMatch(activeMatchIndex.value)
}

watch(matchingLineIndices, (indices) => {
  if (indices.length === 0) {
    activeMatchIndex.value = 0
  } else {
    if (activeMatchIndex.value >= indices.length) {
      activeMatchIndex.value = 0
    }
    scrollToMatch(activeMatchIndex.value)
  }
})

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

const contextMeta = computed(() => {
  const filePath = props.preview.target.value?.filePath
  if (!filePath) return ''
  const ft = getFileType(filePath)
  const langLabel = ft.label || ft.lang || 'Text'
  const total = props.preview.slicedCode.value?.totalLines
  const size = props.preview.fileContent.value?.size
  const parts = [langLabel]
  if (total) {
    parts.push(t('file.codePreview.linesCount', { n: total }))
  }
  if (size !== undefined && size > 0) {
    parts.push(formatFileSize(size))
  }
  return parts.join(' · ')
})

const isPathCopied = ref(false)
let pathCopiedTimer: ReturnType<typeof setTimeout> | null = null

const handleCopyPath = async () => {
  const target = props.preview.target.value
  if (!target?.filePath) return
  let pathText = target.filePath
  if (target.lineStart) {
    pathText += `:${target.lineStart}`
    if (target.lineEnd && target.lineEnd !== target.lineStart) {
      pathText += `-${target.lineEnd}`
    }
  }
  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(pathText)
    } else {
      const textarea = document.createElement('textarea')
      textarea.value = pathText
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.select()
      document.execCommand('copy')
      textarea.remove()
    }
    if (pathCopiedTimer) clearTimeout(pathCopiedTimer)
    isPathCopied.value = true
    updateTooltipText(t('file.codePreview.pathCopied'))
    pathCopiedTimer = setTimeout(() => {
      isPathCopied.value = false
      pathCopiedTimer = null
    }, 1500)
    useToast().show(t('file.codePreview.pathCopied'), { icon: '📋', type: 'success', duration: 1500 })
  } catch {
    // ignore
  }
}

const handleQuoteToChat = () => {
  const target = props.preview.target.value
  const sliced = props.preview.slicedCode.value
  if (!target?.filePath) return
  const code = sliced?.code || props.preview.fileContent.value?.content || ''
  const ft = getFileType(target.filePath)
  const startLine = sliced?.startLine ?? target.lineStart ?? 1
  const endLine = sliced?.endLine ?? target.lineEnd ?? (startLine + Math.max(0, code.split('\n').length - 1))

  const { addStagedQuote } = useChatContext()
  addStagedQuote({
    text: code,
    filePath: target.filePath,
    language: ft.lang || '',
    startLine,
    endLine,
  })
  // A staged quote already carries the file path and line range. The send
  // pipeline derives the file attachment from staged quotes when needed, so
  // adding a second attachment here would render duplicate chips in the draft.
  props.preview.close()
  switchTab('chat')
  useToast().show(t('file.codePreview.quotedToChat'), { icon: '💬', type: 'success', duration: 1500 })
}

const handleRevealInTree = async () => {
  const filePath = props.preview.target.value?.filePath
  if (!filePath) return
  const parentDir = dirName(filePath)
  props.preview.close()
  await store.loadFiles(parentDir)
  switchTab('browse')
  useToast().show(t('file.codePreview.revealedInTree', { dir: parentDir || '/' }), { icon: '📂', type: 'info', duration: 2000 })
}

const toggleWordWrap = () => {
  isWordWrap.value = !isWordWrap.value
  updateTooltipText(isWordWrap.value ? t('file.codePreview.unwrap') : t('file.codePreview.wrap'))
  try {
    localStorage.setItem(STORAGE_KEY_WRAP, String(isWordWrap.value))
  } catch {
    // ignore
  }
  scrollToTargetLine()
}

const handleTogglePin = () => {
  props.preview.togglePin()
  updateTooltipText(props.preview.isPinned.value ? t('file.codePreview.unpin') : t('file.codePreview.pin'))
}

const onCardPointerLeave = () => {
  hideTooltip(true)
  props.preview.onCardPointerLeave()
}

// Dragging coordinates in viewport pixels
const dragX = ref<number | null>(null)
const dragY = ref<number | null>(null)
const isDraggingCard = ref(false)
let isDragging = false
let isDragMoved = false
let cachedCardWidth = 0
let cachedCardHeight = 0
let dragTarget: HTMLElement | null = null
let dragPointerId: number | null = null
let startPointerX = 0
let startPointerY = 0
let startCardX = 0
let startCardY = 0
let resizeObserver: ResizeObserver | null = null
let dragEffectiveMaxY = 0
let dragEffectiveMinY = 0
let dragEffectiveMaxX = 0
let dragEffectiveMinX = 0
let dragRafId: number | null = null
let pendingPointerX = 0
let pendingPointerY = 0

const targetFilePath = computed(() => props.preview.target.value?.filePath || '')

const fileBaseName = computed(() => {
  const p = targetFilePath.value
  if (!p) return t('file.codePreview.title')
  const idx = p.lastIndexOf('/')
  return idx >= 0 ? p.slice(idx + 1) : p
})

const fileDirPath = computed(() => {
  const p = targetFilePath.value
  const idx = p.lastIndexOf('/')
  return idx >= 0 ? p.slice(0, idx) : ''
})

const lineRangeText = computed(() => {
  const start = props.preview.target.value?.lineStart
  const end = props.preview.target.value?.lineEnd
  if (!start) return ''
  return end && end !== start ? `:${start}-${end}` : `:${start}`
})

const sheetTitle = computed(() => {
  const p = targetFilePath.value
  const range = lineRangeText.value
  return p ? `${p}${range}` : t('file.codePreview.title')
})

const fullPathTooltipText = computed(() => {
  const p = targetFilePath.value
  const range = lineRangeText.value
  return p ? `${p}${range}` : `${fileBaseName.value}${range}`
})

// ── Custom Fast Tooltip System ──
interface TooltipState {
  visible: boolean
  text: string
  x: number
  y: number
  placement: 'bottom' | 'top'
}

const tooltipState = reactive<TooltipState>({
  visible: false,
  text: '',
  x: 0,
  y: 0,
  placement: 'bottom',
})

let tooltipShowTimer: ReturnType<typeof setTimeout> | null = null
let tooltipWarmTimer: ReturnType<typeof setTimeout> | null = null
let isTooltipWarm = false

const tooltipStyle = computed(() => {
  const cardW = cachedCardWidth || (cardRef.value ? cardRef.value.offsetWidth : 600)
  const x = tooltipState.x
  const y = tooltipState.y
  const isTop = tooltipState.placement === 'top'

  const posStyle: Record<string, string> = {}
  if (isTop) {
    posStyle.bottom = `calc(100% - ${y}px)`
  } else {
    posStyle.top = `${y}px`
  }

  if (x < 90) {
    posStyle.left = '8px'
    posStyle.transform = 'none'
  } else if (cardW > 0 && x > cardW - 90) {
    posStyle.right = '8px'
    posStyle.left = 'auto'
    posStyle.transform = 'none'
  } else {
    posStyle.left = `${x}px`
    posStyle.transform = 'translateX(-50%)'
  }

  return posStyle
})

const updateTooltipText = (newText: string) => {
  if (tooltipState.visible) {
    tooltipState.text = newText
  }
}

const showTooltip = (e: Event, text: string, options: { delay?: number; isFast?: boolean } = {}) => {
  if (isDraggingCard.value || !text) return
  if (tooltipShowTimer) {
    clearTimeout(tooltipShowTimer)
    tooltipShowTimer = null
  }
  if (tooltipWarmTimer) {
    clearTimeout(tooltipWarmTimer)
    tooltipWarmTimer = null
  }

  const targetEl = (e.currentTarget || e.target) as HTMLElement | null
  if (!targetEl || !cardRef.value) return

  const computePos = () => {
    if (!cardRef.value || isDraggingCard.value) return
    const cardRect = cardRef.value.getBoundingClientRect()
    const targetRect = targetEl.getBoundingClientRect()
    const isTitle = targetEl.classList.contains('code-preview-title') || Boolean(targetEl.closest('.code-preview-title'))
    const targetCenterX = targetRect.left - cardRect.left + (targetRect.width / 2)

    tooltipState.x = isTitle ? 12 : targetCenterX
    tooltipState.y = targetRect.bottom - cardRect.top + 6
    tooltipState.placement = 'bottom'
    tooltipState.text = text
    tooltipState.visible = true
    isTooltipWarm = true
  }

  if (isTooltipWarm && tooltipState.visible) {
    computePos()
    return
  }

  // Fast delay for filename and folder path (70ms), responsive delay for buttons (120ms)
  const delay = options.delay ?? (options.isFast ? 70 : 120)
  tooltipShowTimer = setTimeout(computePos, delay)
}

const hideTooltip = (immediate = false) => {
  if (tooltipShowTimer) {
    clearTimeout(tooltipShowTimer)
    tooltipShowTimer = null
  }
  if (immediate) {
    if (tooltipWarmTimer) {
      clearTimeout(tooltipWarmTimer)
      tooltipWarmTimer = null
    }
    isTooltipWarm = false
    tooltipState.visible = false
    return
  }
  tooltipState.visible = false
  if (tooltipWarmTimer) clearTimeout(tooltipWarmTimer)
  tooltipWarmTimer = setTimeout(() => {
    isTooltipWarm = false
    tooltipWarmTimer = null
  }, 300)
}

const errorMessageText = computed(() => {
  const code = props.preview.errorCode.value
  if (code === 'binary') return t('file.codePreview.binaryNotSupported')
  if (code === 'too-large') return t('file.codePreview.fileTooLarge')
  if (code === 'not-file') return t('file.codePreview.dirNotSupported')
  if (code === 'not-found') return t('file.codePreview.notFound')
  if (code === 'access-denied') return t('file.codePreview.accessDenied')
  return props.preview.errorMessage.value || t('file.codePreview.loadError')
})

const isTargetLine = (lineNum: number): boolean => {
  const sliced = props.preview.slicedCode.value
  if (!sliced?.highlightStart) return false
  const start = sliced.highlightStart
  const end = sliced.highlightEnd ?? start
  return lineNum >= start && lineNum <= end
}

export interface FormattedCodeLine {
  lineNum: number
  html: string
  isTarget: boolean
}

const codeLines = computed<FormattedCodeLine[]>(() => {
  const sliced = props.preview.slicedCode.value
  if (!sliced?.code) return []
  const filePath = props.preview.target.value?.filePath || ''
  const lang = getFileType(filePath).lang || 'plaintext'
  const fullHtml = highlightCode(sliced.code, lang)
  const lineHtmls = splitHighlightedHtml(fullHtml)

  const result: FormattedCodeLine[] = []
  const start = sliced.startLine
  for (let i = 0; i < lineHtmls.length; i++) {
    const lineNum = start + i
    result.push({
      lineNum,
      html: lineHtmls[i],
      isTarget: isTargetLine(lineNum),
    })
  }
  return result
})

const remainingAbove = computed(() => {
  const sliced = props.preview.slicedCode.value
  if (!sliced) return 0
  return Math.max(0, sliced.startLine - 1)
})

const remainingBelow = computed(() => {
  const sliced = props.preview.slicedCode.value
  if (!sliced) return 0
  return Math.max(0, sliced.totalLines - sliced.endLine)
})

const stepAbove = computed(() => Math.min(10, remainingAbove.value))
const stepBelow = computed(() => Math.min(10, remainingBelow.value))

let isDirectionalExpanding = false

// Handlers passed down to CodePreviewBody as `expand-above-lines` /
// `expand-below-lines`. They only mutate the slice via the preview composable;
// CodePreviewBody anchors the scroll position around its own scroll container.
// The isDirectionalExpanding flag suppresses the codeLines watcher below so an
// expansion does not trigger a target-line recenter that would fight the
// scroll anchoring performed by the body.
const expandAbove = async (lines: number) => {
  isDirectionalExpanding = true
  try {
    await props.preview.expandAbove(lines)
    await nextTick()
  } finally {
    nextTick(() => {
      isDirectionalExpanding = false
    })
  }
}

const expandBelow = async (lines: number) => {
  isDirectionalExpanding = true
  try {
    await props.preview.expandBelow(lines)
    await nextTick()
  } finally {
    nextTick(() => {
      isDirectionalExpanding = false
    })
  }
}

const scrollToTargetLine = () => {
  bodyRef.value?.scrollToTargetLine()
}

const cardStyle = computed(() => {
  if (dragX.value !== null && dragY.value !== null) {
    const style: Record<string, string> = {
      left: `${toFixedCSS(dragX.value)}px`,
      top: `${toFixedCSS(dragY.value)}px`,
    }
    // 拖拽期间锁定高度尺寸，作为整体刚体平移，彻底消除每帧 reflow 和底部粘滞拉伸感
    if (isDraggingCard.value && cachedCardHeight > 0) {
      style.height = `${toFixedCSS(cachedCardHeight)}px`
      style.maxHeight = `${toFixedCSS(cachedCardHeight)}px`
    } else {
      const vp = typeof window !== 'undefined' ? getZoomedViewport() : { width: 1024, height: 768 }
      const edgeMargin = 12
      const availableBelow = Math.max(120, vp.height - dragY.value - edgeMargin)
      const plcMaxHeight = props.preview.placement.value?.maxHeight
      const dynamicMaxHeight = plcMaxHeight
        ? Math.min(availableBelow, Math.max(plcMaxHeight, availableBelow))
        : availableBelow
      style.maxHeight = `min(65vh, 480px, ${toFixedCSS(dynamicMaxHeight)}px)`
    }
    return style
  }
  const plc = props.preview.placement.value
  if (plc) {
    const style: Record<string, string> = {
      left: plc.cssLeft,
      top: plc.cssTop,
    }
    if (plc.maxHeight && plc.maxHeight > 0) {
      style.maxHeight = `min(65vh, 480px, ${toFixedCSS(plc.maxHeight)}px)`
    }
    return style
  }
  const anchor = props.preview.target.value?.anchorEl
  if (anchor && typeof anchor.getBoundingClientRect === 'function') {
    const r = anchor.getBoundingClientRect()
    return {
      left: `${toFixedCSS(Math.max(16, r.left))}px`,
      top: `${toFixedCSS(r.bottom + 8)}px`,
    }
  }
  return {
    left: '16px',
    top: '60px',
  }
})

const handleCopy = async () => {
  const code = props.preview.slicedCode.value?.code
  if (!code) return

  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(code)
    } else {
      const textarea = document.createElement('textarea')
      textarea.value = code
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.select()
      document.execCommand('copy')
      textarea.remove()
    }
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 1500)
  } catch {
    // ignore
  }
}

const handleViewDetails = () => {
  // If file is too large, trigger full open which in Clawbench leads to details/download
  props.preview.openFull()
}

const handleEscape = () => {
  if (isSearchOpen.value) {
    closeSearch()
    return
  }
  const anchor = props.preview.target.value?.anchorEl
  props.preview.close()
  if (anchor && typeof anchor.focus === 'function' && document.body.contains(anchor)) {
    anchor.focus()
  }
}

// Drag handling
const updateDragPosition = () => {
  dragRafId = null
  if (!cardRef.value || !isDragging) return
  const deltaX = pendingPointerX - startPointerX
  const deltaY = pendingPointerY - startPointerY
  const nextX = startCardX + deltaX
  const nextY = startCardY + deltaY

  const vp = typeof window !== 'undefined' ? getZoomedViewport() : { width: 1024, height: 768 }
  const topSafe = (typeof window !== 'undefined' ? getAppHeaderBottom() : 40) + 12
  const normalMaxY = Math.max(topSafe, vp.height - cachedCardHeight - 12)
  const normalMaxX = Math.max(12, vp.width - cachedCardWidth - 12)

  // Monotonic convergence towards normal bounds: prevent step jump / jitter on first frame
  if (nextY <= normalMaxY) {
    dragEffectiveMaxY = normalMaxY
  } else {
    dragEffectiveMaxY = Math.min(dragEffectiveMaxY, Math.max(normalMaxY, nextY))
  }

  if (nextY >= topSafe) {
    dragEffectiveMinY = topSafe
  } else {
    dragEffectiveMinY = Math.max(dragEffectiveMinY, Math.min(topSafe, nextY))
  }

  if (nextX <= normalMaxX) {
    dragEffectiveMaxX = normalMaxX
  } else {
    dragEffectiveMaxX = Math.min(dragEffectiveMaxX, Math.max(normalMaxX, nextX))
  }

  if (nextX >= 12) {
    dragEffectiveMinX = 12
  } else {
    dragEffectiveMinX = Math.max(dragEffectiveMinX, Math.min(12, nextX))
  }

  const clampedX = Math.min(Math.max(nextX, dragEffectiveMinX), dragEffectiveMaxX)
  const clampedY = Math.min(Math.max(nextY, dragEffectiveMinY), dragEffectiveMaxY)

  dragX.value = clampedX
  dragY.value = clampedY
}

const onDragPointerDown = (e: PointerEvent) => {
  if (e.button !== 0 && e.button !== undefined) return
  if (!cardRef.value) return

  // Ignore clicks on actionable buttons/inputs
  const targetEl = e.target as HTMLElement | null
  if (targetEl?.closest('button, input, a, .code-preview-btn')) return

  hideTooltip(true)
  props.preview.pin()

  const cardRect = cardRef.value.getBoundingClientRect()
  cachedCardWidth = cardRect.width
  cachedCardHeight = cardRect.height
  startPointerX = e.clientX
  startPointerY = e.clientY
  pendingPointerX = e.clientX
  pendingPointerY = e.clientY
  startCardX = dragX.value !== null ? dragX.value : cardRect.left
  startCardY = dragY.value !== null ? dragY.value : cardRect.top

  dragX.value = startCardX
  dragY.value = startCardY
  isDragging = true
  isDraggingCard.value = true
  isDragMoved = false

  const vp = typeof window !== 'undefined' ? getZoomedViewport() : { width: 1024, height: 768 }
  const topSafe = (typeof window !== 'undefined' ? getAppHeaderBottom() : 40) + 12
  const normalMaxY = Math.max(topSafe, vp.height - cachedCardHeight - 12)
  const normalMaxX = Math.max(12, vp.width - cachedCardWidth - 12)

  // Initialize monotonic convergence bounds: if already outside safe area, smoothly converge without step jump
  dragEffectiveMaxY = Math.max(normalMaxY, startCardY)
  dragEffectiveMinY = Math.min(topSafe, startCardY)
  dragEffectiveMaxX = Math.max(normalMaxX, startCardX)
  dragEffectiveMinX = Math.min(12, startCardX)

  dragTarget = e.currentTarget as HTMLElement
  dragPointerId = e.pointerId
  try {
    dragTarget?.setPointerCapture?.(e.pointerId)
  } catch {
    // ignore
  }

  dragTarget?.addEventListener('pointerup', onDragPointerUp, { once: true })
  dragTarget?.addEventListener('pointercancel', onDragPointerCancel, { once: true })
  window.addEventListener('pointermove', onDragPointerMove)
  window.addEventListener('pointerup', onDragPointerUp, { once: true })
  window.addEventListener('pointercancel', onDragPointerCancel, { once: true })

  document.body.classList.add('code-preview-dragging')
}

const onDragPointerMove = (e: PointerEvent) => {
  if (!isDragging || !cardRef.value) return
  pendingPointerX = e.clientX
  pendingPointerY = e.clientY
  if (!isDragMoved && (Math.abs(e.clientX - startPointerX) > 5 || Math.abs(e.clientY - startPointerY) > 5)) {
    isDragMoved = true
  }

  if (dragRafId === null) {
    dragRafId = requestAnimationFrame(updateDragPosition)
  }
}

const stopDragging = (e?: PointerEvent) => {
  if (!isDragging) return
  isDragging = false
  isDraggingCard.value = false
  document.body.classList.remove('code-preview-dragging')
  window.removeEventListener('pointermove', onDragPointerMove)

  if (dragRafId !== null) {
    cancelAnimationFrame(dragRafId)
    dragRafId = null
  }

  const target = (e?.currentTarget as HTMLElement) || dragTarget
  const pointerId = e?.pointerId ?? dragPointerId
  if (target && pointerId !== null) {
    try {
      target?.releasePointerCapture?.(pointerId)
    } catch {
      // ignore
    }
  }
  dragTarget = null
  dragPointerId = null
}

const onDragPointerUp = (e: PointerEvent) => {
  stopDragging(e)
}

const onDragPointerCancel = (e: PointerEvent) => {
  stopDragging(e)
}

const syncPlacementWithCard = (force = false) => {
  if (!cardRef.value || dragX.value !== null || dragY.value !== null) return
  const isPinned = props.preview.mode.value === 'pinned'
  const cardRect = cardRef.value.getBoundingClientRect()
  if (cardRect.width <= 0 || cardRect.height <= 0) return

  // If pinned and already has placement, keep current position so it doesn't jump on document scroll.
  // Exception: if forced (e.g. content first loaded/measured) or card overflows viewport bounds, adjust it.
  if (!force && isPinned && props.preview.placement.value) {
    const vp = typeof window !== 'undefined' ? getZoomedViewport() : { width: 1024, height: 768 }
    const edgeMargin = 12
    const isOverflowing = cardRect.bottom > vp.height - edgeMargin || cardRect.right > vp.width - edgeMargin
    if (!isOverflowing) {
      return
    }
  }

  const anchor = props.preview.target.value?.anchorEl
  if (!anchor || typeof anchor.getBoundingClientRect !== 'function') return
  props.preview.updatePlacement(anchor, cardRect.width, cardRect.height)
}

const clampCurrentPosition = () => {
  if (!cardRef.value) return
  const cardRect = cardRef.value.getBoundingClientRect()
  if (dragX.value !== null && dragY.value !== null) {
    const clamped = clampCardPosition(dragX.value, dragY.value, cardRect.width, cardRect.height)
    dragX.value = clamped.viewportX
    dragY.value = clamped.viewportY
  } else {
    syncPlacementWithCard()
  }
}

// Reset custom dragged position when closing
watch(
  () => props.preview.visible.value,
  (vis) => {
    if (!vis) {
      // The card (and its pointerleave event) is removed synchronously on
      // close, so explicitly clear any pending/visible tooltip state. Without
      // this, a tooltip shown over the close button can reappear on the next
      // preview instance.
      hideTooltip(true)
      dragX.value = null
      dragY.value = null
      stopDragging()
    } else {
      if (props.preview.status.value === 'ready') {
        scrollToTargetLine()
      }
      nextTick(() => {
        syncPlacementWithCard()
      })
    }
  }
)

// When target changes, clear drag position if not pinned and re-sync placement
watch(
  () => props.preview.target.value,
  () => {
    if (props.preview.mode.value !== 'pinned') {
      dragX.value = null
      dragY.value = null
    }
    nextTick(() => {
      syncPlacementWithCard()
    })
  }
)

// Re-sync placement when code loads or context changes (card height changes)
watch(
  () => props.preview.status.value,
  (st) => {
    if (st === 'ready') {
      scrollToTargetLine()
      nextTick(() => {
        syncPlacementWithCard(true)
      })
    } else {
      nextTick(() => {
        syncPlacementWithCard()
      })
    }
  }
)

watch(
  codeLines,
  () => {
    if (isDirectionalExpanding) return
    if (props.preview.visible.value && props.preview.status.value === 'ready') {
      scrollToTargetLine()
    }
  }
)

watch(
  () => props.preview.contextExpansion.value,
  () => {
    nextTick(() => {
      syncPlacementWithCard()
    })
  }
)

// Dynamically observe cardRef with ResizeObserver whenever it mounts/unmounts
watch(
  () => cardRef.value,
  (newEl, oldEl) => {
    if (oldEl && resizeObserver) {
      resizeObserver.unobserve(oldEl)
    }
    if (newEl && resizeObserver) {
      resizeObserver.observe(newEl)
      nextTick(() => {
        syncPlacementWithCard()
      })
    }
  }
)

// Watch uiScale and window resize to maintain clamped position
watch(
  () => localConfig.uiScale,
  () => {
    nextTick(() => clampCurrentPosition())
  }
)

// Auto-close preview when switching global tab away from current document, or when active file changes
if (activeTab) {
  watch(
    () => activeTab.value,
    (newTab, oldTab) => {
      if (newTab !== oldTab && props.preview.visible.value) {
        if (props.preview.mode.value === 'sheet' || !props.preview.isPinned.value) {
          props.preview.close()
        }
      }
    }
  )
}

watch(
  () => store.state.currentFile?.path,
  (newPath, oldPath) => {
    if (newPath !== oldPath && props.preview.visible.value) {
      props.preview.close()
    }
  }
)

const onWindowResize = () => {
  clampCurrentPosition()
}

// Focus handling for F2 and Ctrl+F search shortcut
const onKeyDown = (e: KeyboardEvent) => {
  if (!props.preview.visible.value) return
  if (e.key === 'F2') {
    e.preventDefault()
    firstActionBtnRef.value?.focus()
  } else if ((e.ctrlKey || e.metaKey) && (e.key === 'f' || e.key === 'F')) {
    e.preventDefault()
    toggleSearch()
  }
}

onMounted(() => {
  window.addEventListener('resize', onWindowResize)
  window.addEventListener('keydown', onKeyDown)

  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      clampCurrentPosition()
    })
    if (cardRef.value) {
      resizeObserver.observe(cardRef.value)
    }
  }
})

onBeforeUnmount(() => {
  stopDragging()
  hideTooltip(true)
  if (pathCopiedTimer) {
    clearTimeout(pathCopiedTimer)
    pathCopiedTimer = null
  }
  window.removeEventListener('resize', onWindowResize)
  window.removeEventListener('keydown', onKeyDown)
  if (resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver = null
  }
})
</script>

<style scoped>
:deep(.bs-header) {
  height: auto;
  min-height: 48px;
  padding: 8px 10px 6px 12px;
}

.code-preview-sheet-body {
  display: flex;
  flex-direction: column;
  height: 100%;
  max-height: 82dvh;
  overflow: hidden;
}


.code-preview-spinner {
  width: 20px;
  height: 20px;
  border: 2px solid rgba(0, 0, 0, 0.1);
  border-top-color: var(--primary-color, #1a73e8);
  border-radius: 50%;
  animation: code-preview-spin 0.8s linear infinite;
}

@keyframes code-preview-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
