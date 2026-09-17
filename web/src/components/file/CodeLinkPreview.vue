<template>
  <!-- Touch Device: BottomSheet mode -->
  <BottomSheet
    v-if="preview.visible.value && preview.mode.value === 'sheet'"
    :open="preview.visible.value"
    auto
    panel-class="code-preview-sheet-panel"
    class="code-preview-sheet"
    :title="sheetTitle"
    @close="preview.close()"
  >
    <!-- Standard drawer header (same structure as TocDrawer / FileDetailsDrawer):
         icon + file title + scrollable full path, with the high-frequency
         view tools (copy path / search / wrap) on the right. -->
    <template #header>
      <span class="bs-header-icon">
        <FileIcon :path="targetFilePath" :size="18" />
      </span>
      <span ref="sheetTitleRef" class="bs-header-title code-preview-sheet-title">
        {{ fileBaseName }}<span v-if="lineRangeText" class="code-preview-line-ref">{{ lineRangeText }}</span>
      </span>
      <div
        v-if="fileDirPath && !titleOverflows"
        class="bs-header-description code-preview-sheet-dir-marquee"
      >
        <!-- Parent directory only (no file name). Hidden when the file name
             needs the space; otherwise draggable to reveal the full path. -->
        <HeaderMarquee :text="fileDirPath">{{ fileDirPath }}</HeaderMarquee>
      </div>
    </template>

    <div class="code-preview-sheet-body">
      <!-- Second-row toolbar: file meta info plus the code view tools
           (Search, Wrap, Copy Code, Reveal in Tree). Only the copy-path
           shortcut lives in the drawer header. -->
      <div class="code-preview-sheet-row2">
        <div class="code-preview-sheet-meta-info">
          <span v-if="contextMeta">{{ contextMeta }}</span>
        </div>

        <div class="code-preview-sheet-tools">
          <!-- Copy Path -->
          <button
            class="code-preview-btn icon-only copy-path-btn"
            :class="{ 'is-copied': isPathCopied }"
            :title="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
            :aria-label="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
            @click="handleCopyPath"
          >
            <Check v-if="isPathCopied" :size="13" />
            <Link v-else :size="13" />
          </button>
          <!-- Rendered / Source toggle (Markdown only, no line range) -->
          <button
            v-if="showTextTools && showRenderToggle"
            class="code-preview-btn icon-only"
            :class="{ 'is-active': isRenderedView }"
            :title="isRenderedView ? t('file.codePreview.sourceView') : t('file.codePreview.renderedView')"
            :aria-label="isRenderedView ? t('file.codePreview.sourceView') : t('file.codePreview.renderedView')"
            :aria-pressed="isRenderedView"
            @click="toggleRenderView"
          >
            <Eye :size="13" />
          </button>
          <!-- Word Wrap Toggle (code-slice view only) -->
          <button
            v-if="showTextTools && !isRenderedView"
            class="code-preview-btn icon-only"
            :class="{ 'is-active': isWordWrap }"
            :title="isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap')"
            :aria-label="isWordWrap ? t('file.codePreview.unwrap') : t('file.codePreview.wrap')"
            @click="toggleWordWrap"
          >
            <TextWrap :size="13" />
          </button>
          <!-- Line Numbers Toggle (code-slice view only) -->
          <button
            v-if="showTextTools && !isRenderedView"
            class="code-preview-btn icon-only"
            :class="{ 'is-active': showLineNumbers }"
            :title="t('file.header.lineNumbers')"
            :aria-label="t('file.header.lineNumbers')"
            :aria-pressed="showLineNumbers"
            @click="toggleLineNumbers"
          >
            <Hash :size="13" />
          </button>
          <!-- Copy Code (code-slice view only) -->
          <button
            v-if="showTextTools && !isRenderedView"
            class="code-preview-btn icon-only"
            :class="{ 'is-copied': copied }"
            :title="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
            :aria-label="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
            @click="handleCopy"
          >
            <Check v-if="copied" :size="13" />
            <Copy v-else :size="13" />
          </button>
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
        {{ t('file.codePreview.truncatedNotice', { size: '512KB' }) }}
      </div>
      <div v-if="preview.windowTruncated.value" class="code-preview-notice notice-warning">
        {{ t('file.codePreview.windowTruncatedNotice') }}
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
        <button class="code-preview-btn icon-only" :disabled="totalMatches === 0" :title="t('file.codePreview.findPrev')" @click="findPrev">
          <ChevronUp :size="12" />
        </button>
        <button class="code-preview-btn icon-only" :disabled="totalMatches === 0" :title="t('file.codePreview.findNext')" @click="findNext">
          <ChevronDown :size="12" />
        </button>
        <button class="code-preview-btn icon-only" :title="t('file.codePreview.findClose')" @click="closeSearch">
          <X :size="12" />
        </button>
      </div>

      <!-- Content Area: directory listing / media / rendered Markdown / code -->
      <DirPreviewBody
        v-if="isDirView"
        chromeless
        :entries="preview.dirEntries.value"
        :loading="preview.dirLoading.value"
        :error="preview.dirError.value"
        :visible="preview.dirEntryVisible"
        :dir-name="dirViewName"
        :dir-path="dirViewPath"
        @open-file="preview.openDirFile"
        @open-dir="preview.openDirChild"
        @open-self="preview.openDirChild('')"
        @closed="preview.close()"
      />
      <MediaPreviewBody
        v-else-if="isMediaView"
        ref="bodyRef"
        :path="targetFilePath"
        :kind="mediaKind"
        :refresh-nonce="preview.mediaRefreshNonce?.value ?? 0"
        @loaded="onMediaLoaded"
      />
      <MarkdownPreviewBody
        v-else-if="isRenderedView"
        ref="bodyRef"
        :status="preview.status.value"
        :error-message-text="errorMessageText"
        :error-code="preview.errorCode.value"
        :rendered-html="renderedHtml"
        :file-path="targetFilePath"
        :remaining-above="remainingAbove"
        :remaining-below="remainingBelow"
        :loading-direction="loadingDirection"
        :load-more-blocked="loadMoreBlocked"
        :load-more-above="loadMoreAbove"
        :load-more-below="loadMoreBelow"
        @refresh="preview.refresh()"
      />
      <CodePreviewBody
        v-else
        ref="bodyRef"
        :status="preview.status.value"
        :error-message-text="errorMessageText"
        :error-code="preview.errorCode.value"
        :is-word-wrap="isWordWrap"
        :show-line-numbers="showLineNumbers"
        :code-lines="codeLines"
        :matching-line-indices="matchingLineIndices"
        :active-match-index="activeMatchIndex"
        :remaining-above="remainingAbove"
        :remaining-below="remainingBelow"
        :loading-direction="loadingDirection"
        :load-more-blocked="loadMoreBlocked"
        :load-more-above="loadMoreAbove"
        :load-more-below="loadMoreBelow"
        @refresh="preview.refresh()"
      />
    </div>

    <!-- Bottom Action Bar (Thumb area - Left-hand optimized) -->
    <template #footer>
      <div class="code-preview-sheet-footer">
        <!-- Refresh: icon-only round button -->
        <button
          class="code-preview-footer-btn icon-btn fbtn refresh-btn"
          :class="{ 'is-loading': preview.status.value === 'loading' }"
          :title="t('file.codePreview.refresh')"
          :aria-label="t('file.codePreview.refresh')"
          @click="preview.refresh()"
        >
          <RefreshCw :size="15" />
        </button>

        <!-- Search in preview: icon-only, code-slice view only -->
        <button
          v-if="showTextTools && !isRenderedView"
          class="code-preview-footer-btn icon-btn fbtn"
          :class="{ 'is-active': isSearchOpen }"
          :title="t('file.codePreview.findInPreview')"
          :aria-label="t('file.codePreview.findInPreview')"
          @click="toggleSearch"
        >
          <Search :size="15" />
        </button>

        <!-- Reveal in file tree: icon-only -->
        <button
          class="code-preview-footer-btn icon-btn fbtn reveal-btn"
          :title="t('file.codePreview.revealInTree')"
          :aria-label="t('file.codePreview.revealInTree')"
          @click="handleRevealInTree"
        >
          <Folder :size="15" />
        </button>

        <!-- Open Full / View Details — primary action. Both cases render the
             same control: an oversize file opens the same way (the label already
             reads "Full file"). -->
        <button
          v-if="!isDirView"
          class="code-preview-footer-btn action-btn fbtn fbtn-primary primary-btn"
          @click="preview.openFull()"
        >
          <ExternalLink :size="15" />
          <span>{{ t('file.codePreview.openFileShort') }}</span>
        </button>

        <!-- Quote to Chat (text files only — media has no quotable text) -->
        <button
          v-if="showTextTools"
          class="code-preview-footer-btn action-btn fbtn quote-btn"
          :title="t('file.codePreview.quoteToChat')"
          @click="handleQuoteToChat"
        >
          <MessageSquare :size="15" />
          <span>{{ t('file.codePreview.quoteShort') }}</span>
        </button>
      </div>
    </template>
  </BottomSheet>

  <!-- Desktop Floating: Teleport to body. When docked, Teleport is disabled so
       the very same card markup renders in place inside the caller's pane —
       one template, no duplication. -->
  <Teleport
    v-else-if="preview.visible.value && preview.mode.value !== 'sheet'"
    :disabled="docked"
    to="body"
  >
    <div
      ref="cardRef"
      class="code-link-preview-floating"
      :class="{
        'is-dragging': isDraggingCard,
        'is-media': isMediaView,
        'is-docked': docked,
      }"
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

      <!-- Titlebar / Drag Handle: Row 1 (File Path + Copy Path Button).
           Rendered in every non-sheet mode, including docked: the docked pane
           reuses this row so the tool row below has the full pane width. The
           only docked difference is that Pin is suppressed (nothing floats to
           pin — see the guard on that button). -->
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
          <!-- Window Controls: Pin + Close live in the header on wide screens.
               Pin is meaningless when docked (nothing floats to pin). -->
          <button
            v-if="!docked"
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
            <Pin :size="12" :fill="preview.isPinned.value ? 'currentColor' : 'none'" />
          </button>
          <button
            class="code-preview-btn close"
            :title="t('file.codePreview.close')"
            :aria-label="t('file.codePreview.close')"
            :data-tooltip="t('file.codePreview.close')"
            @pointerenter="showTooltip($event, t('file.codePreview.close'))"
            @pointerleave="hideTooltip()"
            @click="handleClose()"
          >
            <X :size="12" />
          </button>
        </div>
      </div>

      <!-- Row 2: File Meta & Remaining Action Tools. -->
      <div class="code-preview-meta" @pointerdown="onDragPointerDown">
        <div class="code-preview-meta-info">
          <span>{{ contextMeta || t('file.codePreview.title') }}</span>
        </div>

        <div class="code-preview-actions" @pointerdown.stop>
          <!-- Rendered / Source toggle (Markdown only, no line range) -->
          <button
            v-if="showRenderToggle"
            class="code-preview-btn"
            :class="{ 'is-active': isRenderedView }"
            :aria-pressed="isRenderedView"
            :title="isRenderedView ? t('file.codePreview.sourceView') : t('file.codePreview.renderedView')"
            :aria-label="isRenderedView ? t('file.codePreview.sourceView') : t('file.codePreview.renderedView')"
            :data-tooltip="isRenderedView ? t('file.codePreview.sourceView') : t('file.codePreview.renderedView')"
            @pointerenter="showTooltip($event, isRenderedView ? t('file.codePreview.sourceView') : t('file.codePreview.renderedView'))"
            @pointerleave="hideTooltip()"
            @click="toggleRenderView"
          >
            <Eye :size="12" />
          </button>
          <!-- Viewer Tools: Find, Wrap, Line Numbers, Refresh (code-slice view only) -->
          <button
            v-if="showTextTools && !isRenderedView"
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
            <Search :size="12" />
          </button>
          <button
            v-if="showTextTools && !isRenderedView"
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
            <TextWrap :size="12" />
          </button>
          <button
            v-if="showTextTools && !isRenderedView"
            class="code-preview-btn"
            :class="{ 'is-active': showLineNumbers }"
            :aria-pressed="showLineNumbers"
            :title="t('file.header.lineNumbers')"
            :aria-label="t('file.header.lineNumbers')"
            :data-tooltip="t('file.header.lineNumbers')"
            @pointerenter="showTooltip($event, t('file.header.lineNumbers'))"
            @pointerleave="hideTooltip()"
            @click="toggleLineNumbers"
          >
            <Hash :size="12" />
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
            <RefreshCw :size="12" />
          </button>

          <span class="code-preview-actions-divider" />

          <!-- Actions: Quote, Copy Code (code view). Then the contiguous file
               tools — Copy Path / Open Directory (reveal) / Open File — with no
               dividers between them, in that left-to-right order. -->
          <button
            v-if="showTextTools"
            class="code-preview-btn"
            :title="t('file.codePreview.quoteToChat')"
            :aria-label="t('file.codePreview.quoteToChat')"
            :data-tooltip="t('file.codePreview.quoteToChat')"
            @pointerenter="showTooltip($event, t('file.codePreview.quoteToChat'))"
            @pointerleave="hideTooltip()"
            @click="handleQuoteToChat"
          >
            <MessageSquare :size="12" />
          </button>
          <button
            v-if="showTextTools && !isRenderedView"
            class="code-preview-btn"
            :class="{ 'is-copied': copied }"
            :title="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
            :aria-label="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
            :data-tooltip="copied ? t('file.codePreview.copied') : t('file.codePreview.copy')"
            @pointerenter="showTooltip($event, copied ? t('file.codePreview.copied') : t('file.codePreview.copy'))"
            @pointerleave="hideTooltip()"
            @click="handleCopy"
          >
            <Check v-if="copied" :size="12" />
            <Copy v-else :size="12" />
          </button>

          <!-- Copy Path -->
          <button
            class="code-preview-btn copy-path-btn"
            :class="{ 'is-copied': isPathCopied }"
            :title="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
            :aria-label="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
            :data-tooltip="isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath')"
            @pointerenter="showTooltip($event, isPathCopied ? t('file.codePreview.pathCopied') : t('file.codePreview.copyPath'))"
            @pointerleave="hideTooltip()"
            @click="handleCopyPath"
          >
            <Check v-if="isPathCopied" :size="12" />
            <Link v-else :size="12" />
          </button>
          <!-- Open Directory (reveal in tree) -->
          <button
            class="code-preview-btn"
            :title="t('file.codePreview.revealInTree')"
            :aria-label="t('file.codePreview.revealInTree')"
            :data-tooltip="t('file.codePreview.revealInTree')"
            @pointerenter="showTooltip($event, t('file.codePreview.revealInTree'))"
            @pointerleave="hideTooltip()"
            @click="handleRevealInTree"
          >
            <Folder :size="12" />
          </button>
          <!-- Open File / View Details.
               A too-large file used to swap this for a wide text button reading
               "View details / Download", which was 4x the width of every other
               control in the row (111px vs 26px) and broke the icon strip. It
               also did nothing different: it called the same openFull() as the
               normal case. So the icon is used in both cases, and only the
               tooltip changes — it carries the "download" affordance for an
               oversize file. -->
          <button
            v-if="!isDirView"
            class="code-preview-btn"
            :title="tooLarge ? t('file.codePreview.viewDetails') : t('file.codePreview.openFull')"
            :aria-label="tooLarge ? t('file.codePreview.viewDetails') : t('file.codePreview.openFull')"
            :data-tooltip="tooLarge ? t('file.codePreview.viewDetails') : t('file.codePreview.openFull')"
            @pointerenter="showTooltip($event, tooLarge ? t('file.codePreview.viewDetails') : t('file.codePreview.openFull'))"
            @pointerleave="hideTooltip()"
            @click="preview.openFull()"
          >
            <ExternalLink :size="12" />
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
          <ChevronUp :size="12" />
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
          <ChevronDown :size="12" />
        </button>
        <button
          class="code-preview-btn"
          :title="t('file.codePreview.findClose')"
          :data-tooltip="t('file.codePreview.findClose')"
          @pointerenter="showTooltip($event, t('file.codePreview.findClose'))"
          @pointerleave="hideTooltip()"
          @click="closeSearch"
        >
          <X :size="12" />
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
          {{ t('file.codePreview.truncatedNotice', { size: '512KB' }) }}
        </div>
        <div v-if="preview.windowTruncated.value" class="code-preview-notice notice-warning">
          {{ t('file.codePreview.windowTruncatedNotice') }}
        </div>
      </div>

      <!-- Body / Scroll pane: directory listing / media / rendered Markdown /
           source code slice -->
      <DirPreviewBody
        v-if="isDirView"
        chromeless
        :entries="preview.dirEntries.value"
        :loading="preview.dirLoading.value"
        :error="preview.dirError.value"
        :visible="preview.dirEntryVisible"
        :dir-name="dirViewName"
        :dir-path="dirViewPath"
        @open-file="preview.openDirFile"
        @open-dir="preview.openDirChild"
        @open-self="preview.openDirChild('')"
        @closed="preview.close()"
      />
      <MediaPreviewBody
        v-else-if="isMediaView"
        ref="bodyRef"
        :path="targetFilePath"
        :kind="mediaKind"
        :refresh-nonce="preview.mediaRefreshNonce?.value ?? 0"
        @loaded="onMediaLoaded"
      />
      <MarkdownPreviewBody
        v-else-if="isRenderedView"
        ref="bodyRef"
        :status="preview.status.value"
        :error-message-text="errorMessageText"
        :error-code="preview.errorCode.value"
        :rendered-html="renderedHtml"
        :file-path="targetFilePath"
        :remaining-above="remainingAbove"
        :remaining-below="remainingBelow"
        :loading-direction="loadingDirection"
        :load-more-blocked="loadMoreBlocked"
        :load-more-above="loadMoreAbove"
        :load-more-below="loadMoreBelow"
        @refresh="preview.refresh()"
      />
      <CodePreviewBody
        v-else
        ref="bodyRef"
        :status="preview.status.value"
        :error-message-text="errorMessageText"
        :error-code="preview.errorCode.value"
        :is-word-wrap="isWordWrap"
        :show-line-numbers="showLineNumbers"
        :code-lines="codeLines"
        :matching-line-indices="matchingLineIndices"
        :active-match-index="activeMatchIndex"
        :remaining-above="remainingAbove"
        :remaining-below="remainingBelow"
        :loading-direction="loadingDirection"
        :load-more-blocked="loadMoreBlocked"
        :load-more-above="loadMoreAbove"
        :load-more-below="loadMoreBelow"
        @refresh="preview.refresh()"
      />
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted, onBeforeUnmount, nextTick, inject, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, ChevronDown, ChevronUp, Copy, Eye, ExternalLink, Folder, Hash, Link, MessageSquare, Pin, RefreshCw, Search, TextWrap, X } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import CodePreviewBody from '@/components/file/CodePreviewBody.vue'
import MarkdownPreviewBody from '@/components/file/MarkdownPreviewBody.vue'
import MediaPreviewBody from '@/components/file/MediaPreviewBody.vue'
import DirPreviewBody from '@/components/file/DirPreviewBody.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import HeaderMarquee from '@/components/common/HeaderMarquee.vue'
import { highlightCode } from '@/utils/globals'
import { getFileType } from '@/utils/fileType'
import { clampCardPosition, splitHighlightedHtml, getAppHeaderBottom, SCROLL_LOAD_STEP } from '@/utils/codeLinkPreview'
import { toFixedCSS, useSettingsConfig, getZoomedViewport } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'
import { useChatContext } from '@/composables/useChatContext'
import { store } from '@/stores/app'
import { navToFileInManager } from '@/composables/useFilePathAnnotation'
import type { useCodeLinkPreview } from '@/composables/useCodeLinkPreview'
import '@/assets/code-link-preview.css'

const props = withDefaults(defineProps<{
  preview: ReturnType<typeof useCodeLinkPreview>
  /** Render inline in the caller's pane (file manager bottom pane) instead of
   *  as a body-teleported floating card. Suppresses drag + float placement. */
  docked?: boolean
}>(), {
  docked: false,
})

const { t } = useI18n()
const { localConfig, setLocalConfig } = useSettingsConfig()
const switchTab = inject<(tab: string) => void>('switchTab', () => {})
const activeTab = inject<Ref<string> | undefined>('activeTab', undefined)

/**
 * The docked pane reuses the floating card's chrome — the same header row
 * (directory path + Pin + Close) and the same meta row (line/size summary +
 * tools). Only Pin is suppressed, because there is nothing to float or pin.
 *
 * It previously rendered a special ONE-row layout: the header was hidden and
 * the file name, tool strip and Close were all crammed into the meta row, with
 * the strip scrolling horizontally when it could not fit. That existed to save
 * ~27px in a short pane, but it forced the tool row to compete with the file
 * name for width and made the tool set reachable only by sideways scrolling.
 * With a full header row the tool row gets the whole pane width instead.
 *
 * The shape is identical on every platform — do NOT reintroduce a
 * `(pointer: coarse)` / `is-compact` split.
 */

const emit = defineEmits<{
  /** Fired after the preview is dismissed. Docked callers use it to collapse
   *  their pane; floating callers ignore it. */
  (e: 'closed'): void
}>()

/** Close the preview. Docked callers get a `closed` event so they can collapse
 *  the pane (the pane is otherwise always visible while preview mode is on). */
function handleClose() {
  props.preview.close()
  emit('closed')
}

// Reuse the global "show line numbers" file-viewer setting so the code-link
// preview follows the same preference as the main editor.
const showLineNumbers = computed(() => localConfig.lineNumbers !== false)

function toggleLineNumbers() {
  setLocalConfig('lineNumbers', !showLineNumbers.value)
}

// ── Rendered Markdown document view ────────────────────────────────────────
// A Markdown file previewed WITHOUT a line range defaults to a read-only
// rendered document (see useCodeLinkPreview.showPreview). The render mode is
// switchable via the eye toggle, but only when the current target qualifies:
// non-Markdown files and line-annotated Markdown paths stay on the source
// slice view.

// HTML for the rendered document view, produced lazily through the shared
// markdown pipeline. Built as a string whenever the source slice changes and
// the rendered view is visible. The builder is imported lazily so the heavy
// markdown/katex/mermaid pipeline is only loaded when an actual Markdown file
// renders (code-only previews and unit tests that mock the pipeline never pull
// those modules in).
const renderedHtml = ref('')

let renderedSliceKey = ''

const isRenderedView = computed(() => props.preview.effectiveRenderMode?.value === 'rendered')
const showRenderToggle = computed(() => Boolean(props.preview.canRenderMarkdown?.value))

// ── Media body (image / SVG / video / audio / PDF) ─────────────────────────
// When the target is a media file the card renders MediaPreviewBody instead of
// the code/markdown bodies, and all text-viewer tools (search, wrap, line
// numbers, copy code, rendered/source toggle) are hidden — they are meaningless
// for media.
const isMediaView = computed(() => Boolean(props.preview.isMediaTarget?.value))
const mediaKind = computed<'image' | 'video' | 'audio' | 'pdf' | null>(() => {
  const p = props.preview
  if (p.isImageTarget?.value) return 'image'
  if (p.isVideoTarget?.value) return 'video'
  if (p.isAudioTarget?.value) return 'audio'
  if (p.isPdfTarget?.value) return 'pdf'
  return null
})
// Text-slice tools are only meaningful when a code/markdown body is showing.
const showTextTools = computed(() => !isMediaView.value && !isDirView.value)

// ── Directory body ─────────────────────────────────────────────────────────
// A directory annotation has no file content, so the card lists it with the
// same control the file manager's docked pane uses (DirPreviewBody). The
// listing replaces the code/markdown/media bodies entirely, and every
// text-viewer tool is hidden — none of them mean anything for a directory.
const isDirView = computed(() => Boolean(props.preview.isDirTarget?.value))

/** Base name of the listed directory, for the card's title. */
const dirViewName = computed(() => {
  const p = props.preview.target.value?.filePath || ''
  const base = p.replace(/\/+$/, '').split('/').pop()
  return base || p
})

/** Project-relative path of the listed directory, for thumbnail URLs. */
const dirViewPath = computed(() => props.preview.target.value?.filePath || '')

function toggleRenderView() {
  props.preview.toggleRenderMode?.()
}

watch(
  () => [
    props.preview.status.value,
    props.preview.slicedCode.value?.code,
    props.preview.target.value?.filePath,
    isRenderedView.value,
  ],
  async () => {
    // Leaving the rendered view (closed, switched target, toggled to source)
    // clears the cached slice so re-opening the same file re-renders it.
    if (!isRenderedView.value || props.preview.status.value !== 'ready') {
      renderedHtml.value = ''
      renderedSliceKey = ''
      return
    }
    const filePath = props.preview.target.value?.filePath || ''
    const code = props.preview.slicedCode.value?.code
    if (!filePath || code === undefined) return
    const sliceKey = `${filePath}::${code.length}::${code.slice(0, 120)}`
    if (sliceKey === renderedSliceKey) return
    renderedSliceKey = sliceKey
    try {
      const { buildPreviewMarkdownHtml } = await import('@/utils/previewMarkdown')
      renderedHtml.value = buildPreviewMarkdownHtml({
        content: code,
        path: filePath,
      })
    } catch {
      // Fall back to the source slice if rendering fails for any reason.
      renderedHtml.value = ''
    }
  },
  { immediate: true }
)

const STORAGE_KEY_WRAP = 'clawbench:code-preview-word-wrap'

const cardRef = ref<HTMLElement | null>(null)
// The currently-rendered code pane (sheet mode OR floating mode — only one
// renders at a time), typed as the exposed instance of CodePreviewBody.
const bodyRef = ref<{ scrollToTargetLine: () => void; scrollLineIntoView: (i: number) => void } | null>(null)
const firstActionBtnRef = ref<HTMLButtonElement | null>(null)
const copied = ref(false)
const isWordWrap = ref<boolean>(true)

// ── Sheet header title overflow detection ──
// The drawer header shows the file name first and the parent-dir path second.
// When the file name would not fit even with the path hidden, the path is not
// rendered at all so the name gets every pixel available.
const sheetTitleRef = ref<HTMLElement | null>(null)
const titleOverflows = ref(false)
let titleResizeObserver: ResizeObserver | null = null

function measureSheetTitle() {
  const title = sheetTitleRef.value
  if (!title) return
  const header = title.parentElement // the .bs-header flex row
  if (!header) return
  // Reserve the leading file icon + paddings. A parent-dir marquee that would
  // only get a sliver (< 60px) is not worth showing — the file name should
  // never be starved for space.
  const MIN_PATH_PX = 60
  const avail = header.clientWidth - 40 // icon (24) + gaps/padding
  // No layout (e.g. tests / display:none): keep the path visible by default.
  if (!(header.clientWidth > 0)) {
    titleOverflows.value = false
    return
  }
  const titleNeeds = title.scrollWidth
  titleOverflows.value = titleNeeds + MIN_PATH_PX > Math.max(1, avail)
}

watch(
  () => [props.preview.target.value?.filePath, props.preview.target.value?.lineStart, props.preview.target.value?.lineEnd, props.preview.target.value?.lineRanges, props.preview.visible.value],
  () => {
    // Let the DOM settle with the new title before measuring.
    nextTick(() => measureSheetTitle())
  }
)

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
  // Search only makes sense when a text body is showing. Guarding here (rather
  // than only at the button) also covers the Ctrl+F shortcut, which is handled
  // on window and would otherwise open a dead search bar over a directory
  // listing or a media file — neither has searchable lines.
  if (!showTextTools.value) return
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
  // A directory has no line count or size; show how many entries it holds.
  // Count only the VISIBLE entries — the same filter DirPreviewBody renders
  // with — so the number always matches the grid below it (and the docked
  // pane, which also counts `shown`).
  if (isDirView.value) {
    const shown = props.preview.dirEntries.value.filter(e => props.preview.dirEntryVisible(e))
    return t('file.dirPreview.count', { n: shown.length })
  }
  const total = props.preview.slicedCode.value?.totalLines
  const size = props.preview.fileContent.value?.size
  // File type/language label is omitted: the file-name extension already
  // conveys it. Keep line count + size, which the name does not show.
  const parts: string[] = []
  if (total) {
    parts.push(t('file.codePreview.linesCount', { n: total }))
  }
  if (size !== undefined && size > 0) {
    parts.push(formatFileSize(size))
  }
  // Media targets have no fetched content (no line count / size), so surface
  // the file type label instead of leaving the meta row blank.
  if (parts.length === 0 && isMediaView.value) {
    const label = getFileType(filePath).label
    if (label) parts.push(label)
  }
  return parts.join(' · ')
})

const isPathCopied = ref(false)
let pathCopiedTimer: ReturnType<typeof setTimeout> | null = null

const handleCopyPath = async () => {
  const target = props.preview.target.value
  if (!target?.filePath) return
  const pathText = target.filePath + (lineRangeSuffix.value || '')
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
  // A directory card reveals the directory ITSELF (navigate into it), which is
  // what "open directory" means for a directory. navToFileInManager would
  // instead reveal its parent, which is wrong here.
  if (props.preview.isDirTarget?.value) {
    props.preview.openDirChild('')
    return
  }
  props.preview.close()
  // Shared "reveal in file manager" behavior (same as file-search results):
  // navigates to the containing directory and highlights the file there.
  // No toast — the resulting file-manager navigation is self-evident.
  await navToFileInManager(filePath)
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

/** Canonical `:90-91,309,938-943` suffix (empty when no line target). */
const lineRangeSuffix = computed(() => {
  const target = props.preview.target.value
  if (target?.lineRanges) return `:${target.lineRanges}`
  const start = target?.lineStart
  if (!start) return ''
  const end = target?.lineEnd
  return end && end !== start ? `:${start}-${end}` : `:${start}`
})

const lineRangeText = computed(() => lineRangeSuffix.value)

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

/**
 * The file exceeded the preview size cap. The open control still opens the file
 * the same way, but its tooltip says "View details / Download" so the user knows
 * that path is also where the download lives.
 */
const tooLarge = computed(() => props.preview.errorCode.value === 'too-large')

const isTargetLine = (lineNum: number): boolean => {
  const sliced = props.preview.slicedCode.value
  if (!sliced) return false
  // Multi-range annotations carry the authoritative per-line ranges (already
  // clamped to the rendered window); fall back to the single min/max span.
  if (sliced.highlightRanges && sliced.highlightRanges.length > 0) {
    return sliced.highlightRanges.some(r => lineNum >= r.start && lineNum <= r.end)
  }
  if (!sliced.highlightStart) return false
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

/**
 * Whether asking for more lines can still help. A byte ceiling (an oversized
 * line, or the 512 KiB budget) caps the slice at a fixed point, so the bodies
 * must stop auto-loading — otherwise every scroll event would fire a fetch
 * that cannot change anything. The "N lines remaining" hints stay visible.
 */
const loadMoreBlocked = computed(() => props.preview.loadMoreBlocked.value)

/** Which side is loading, so only the matching bar shows a spinner. */
const loadingDirection = ref<'above' | 'below' | null>(null)

let isDirectionalExpanding = false

/**
 * Pull in the next chunk below the slice. Passed to the body as
 * `load-more-below`; the body anchors its own scroll position around the
 * insertion. The isDirectionalExpanding flag suppresses the codeLines watcher
 * below so a load does not trigger a target-line recenter that would fight the
 * body's scroll anchoring.
 */
const loadMoreBelow = async () => {
  isDirectionalExpanding = true
  loadingDirection.value = 'below'
  try {
    await props.preview.expandBelow(SCROLL_LOAD_STEP)
    await nextTick()
  } finally {
    loadingDirection.value = null
    nextTick(() => {
      isDirectionalExpanding = false
    })
  }
}

/**
 * Pull in the next chunk above the slice. Only reached by scrolling to the very
 * top of a slice that has content above it — i.e. a line-annotated open, where
 * the window starts at the annotation rather than at line 1.
 */
const loadMoreAbove = async () => {
  isDirectionalExpanding = true
  loadingDirection.value = 'above'
  try {
    await props.preview.expandAbove(SCROLL_LOAD_STEP)
    await nextTick()
  } finally {
    loadingDirection.value = null
    nextTick(() => {
      isDirectionalExpanding = false
    })
  }
}

const scrollToTargetLine = () => {
  // The body component may not be mounted yet when this runs (e.g. a cache-hit
  // open flips visible/status/codeLines within the same flush, so the parent's
  // watchers fire before CodePreviewBody has mounted). Defer one tick so the
  // exposed scroll helper exists when invoked.
  nextTick(() => {
    // Guard with typeof: a media body has no target line, and a <script setup>
    // component without defineExpose still resolves to a truthy empty proxy.
    if (typeof bodyRef.value?.scrollToTargetLine === 'function') {
      bodyRef.value.scrollToTargetLine()
    }
  })
}

// Max-height for the card. Code slices are capped at 65vh/480px so the card
// stays compact; media cards hold the file itself, so they are allowed to grow
// to the full placement box (which already accounts for the viewport).
const mediaMaxHeightCss = (clampPx: number) =>
  isMediaView.value
    ? `${clampPx}px`
    : `min(65vh, 480px, ${clampPx}px)`

const cardStyle = computed(() => {
  // Docked: the pane owns position/size (flex + split ratio), so no inline
  // float styles — CSS handles it entirely.
  if (props.docked) return {}
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
      style.maxHeight = mediaMaxHeightCss(toFixedCSS(dynamicMaxHeight))
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
      style.maxHeight = mediaMaxHeightCss(toFixedCSS(plc.maxHeight))
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

// Media bodies (image/video/audio/PDF) size themselves from the file's own
// intrinsic dimensions. Once loaded, re-measure the card so the placement clamp
// and drag bounds use the real height instead of the pre-load estimate.
const onMediaLoaded = () => {
  nextTick(() => clampCurrentPosition())
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

// Outside-click dismissal for the desktop floating card. A click that lands
// anywhere outside the card closes it — but only while it is unpinned
// (transient). Pinned cards are explicitly dismissed (Esc / × / pin toggle),
// and the touch BottomSheet never uses this path (its own scrim handles
// dismissal). pointerdown is used so a drag that starts on the titlebar/meta
// row never races this check (the down target is already inside the card).
const onDocumentPointerDown = (e: PointerEvent) => {
  // A docked pane is a layout pane, not a dismissible popover: it must not
  // close on an outside click. Beyond being the wrong interaction, closing on
  // pointerdown here would unmount the pane mid-gesture — e.g. grabbing the
  // split divider would remove the divider from the DOM, and the browser would
  // then start a native drag on the file row revealed underneath.
  if (props.docked) return
  if (!props.preview.visible.value) return
  if (props.preview.mode.value === 'sheet') return
  if (props.preview.isPinned.value) return
  const card = cardRef.value
  if (!card) return
  const target = e.target as Node | null
  if (target && card.contains(target)) return
  // Caller-declared elements (e.g. file-manager rows) retarget the card in
  // place on click instead of dismissing it — leave them alone.
  const ignore = props.preview.outsideClickIgnoreSelector
  if (ignore && target instanceof Element && target.closest(ignore)) return
  props.preview.close()
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
  // A docked pane is not draggable — it is laid out by the caller's split.
  if (props.docked) return
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
  // Docked panes have no float placement to sync.
  if (props.docked) return
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
  // Docked panes are sized by the split layout, never clamped to the viewport.
  if (props.docked) return
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
  document.addEventListener('pointerdown', onDocumentPointerDown, true)

  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      clampCurrentPosition()
    })
    if (cardRef.value) {
      resizeObserver.observe(cardRef.value)
    }
  }

  // Re-measure the sheet title when the header row or the title itself
  // changes size (resize, different file, etc.).
  if (typeof ResizeObserver !== 'undefined') {
    titleResizeObserver = new ResizeObserver(() => measureSheetTitle())
    nextTick(() => {
      const header = sheetTitleRef.value?.parentElement
      if (header) titleResizeObserver?.observe(header)
      measureSheetTitle()
    })
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
  document.removeEventListener('pointerdown', onDocumentPointerDown, true)
  if (resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver = null
  }
  if (titleResizeObserver) {
    titleResizeObserver.disconnect()
    titleResizeObserver = null
  }
})
</script>

<style scoped>
.code-preview-sheet-body {
  display: flex;
  flex-direction: column;
  height: 100%;
  max-height: 82dvh;
  overflow: hidden;
}

/* Sheet header keeps a touch-friendlier height than the compact default so the
   copy-path / search / wrap buttons in .bs-header-actions stay easy to hit. */
:deep(.code-preview-sheet .bs-header) {
  min-height: 44px;
}
</style>
