<template>
  <BottomSheet :open="open" auto :title="t('toc.title')" @close="$emit('close')">
    <template #header>
      <List :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('toc.title') }}</span>
      <div v-if="file?.path" class="bs-header-description">
        <HeaderMarquee :text="file.path">{{ file.path }}</HeaderMarquee>
      </div>
    </template>

    <div class="toc-body">
      <SearchInput v-model="searchQuery" :placeholder="t('toc.searchPlaceholder')" @dblclick="clearSearch" />
      <div class="toc-list">
        <div v-if="filteredToc.length === 0" class="toc-empty">{{ searchQuery ? t('toc.noMatch') : t('toc.noHeadings') }}</div>
        <a
          v-for="item in filteredToc"
          :key="item.id"
          class="toc-item"
          :class="{ active: activeId === item.id }"
          :data-level="item.level"
          @click.prevent="scrollTo(item)"
        >
          <component
            v-if="item.kind"
            :is="kindIcon(item.kind).icon"
            :size="13"
            class="toc-kind-icon"
            :class="kindIcon(item.kind).cls"
          />
          <span v-if="isPdfOutline" class="toc-page-badge">P{{ item.line }}</span>
          {{ item.text }}
        </a>
      </div>
    </div>

  </BottomSheet>
</template>

<script setup>
import { List, Braces, Box, Boxes, FileCode2, SquareAsterisk, ListOrdered, Variable, Hash, Package, FolderTree, CircleDot, Settings2, Hammer, Layers, Puzzle, Zap, Code2, Heading } from 'lucide-vue-next'
import { ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import HeaderMarquee from '@/components/common/HeaderMarquee.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import { extractToc, slugify } from '@/utils/toc.ts'
import { getFileType } from '@/utils/fileType.ts'
import { fetchCodeSymbols } from '@/composables/useCodeSymbols'

const { t } = useI18n()

/** Map symbol kind → { icon component, CSS class } */
const KIND_ICON_MAP = {
  function:     { icon: Braces,        cls: 'kind-function' },
  method:       { icon: Braces,        cls: 'kind-method' },
  constructor:  { icon: Hammer,        cls: 'kind-constructor' },
  class:        { icon: Box,           cls: 'kind-class' },
  struct:       { icon: Boxes,         cls: 'kind-struct' },
  interface:    { icon: FileCode2,     cls: 'kind-interface' },
  type:         { icon: SquareAsterisk, cls: 'kind-type' },
  enum:         { icon: ListOrdered,   cls: 'kind-enum' },
  variable:     { icon: Variable,      cls: 'kind-variable' },
  constant:     { icon: Hash,          cls: 'kind-constant' },
  module:       { icon: Package,       cls: 'kind-module' },
  namespace:    { icon: FolderTree,    cls: 'kind-namespace' },
  field:        { icon: CircleDot,     cls: 'kind-field' },
  property:     { icon: Settings2,     cls: 'kind-property' },
  trait:        { icon: Layers,        cls: 'kind-trait' },
  impl:         { icon: Puzzle,        cls: 'kind-impl' },
  macro:        { icon: Zap,           cls: 'kind-macro' },
  heading:      { icon: Heading,       cls: 'kind-heading' },
}
const KIND_FALLBACK = { icon: Code2, cls: 'kind-other' }

const props = defineProps({
    file: Object,
    pdfOutline: { type: Array, default: () => [] },
    open: Boolean,
})
const emit = defineEmits(['close', 'jump', 'jumpPage'])

const toc = ref([])
const activeId = ref('')
const isCode = ref(false)
const isPdfOutline = ref(false)
const searchQuery = ref('')
const filteredToc = ref([])

watch([() => props.file, () => props.pdfOutline], ([file, pdfOut]) => {
    // PDF outline
    if (file && pdfOut && pdfOut.length > 0) {
        isPdfOutline.value = true
        isCode.value = false
        toc.value = pdfOut
        activeId.value = toc.value[0]?.id || ''
        searchQuery.value = ''
        filteredToc.value = toc.value
        return
    }
    isPdfOutline.value = false

    // Text-based TOC
    if (!file?.content) {
        toc.value = []
        filteredToc.value = []
        isCode.value = false
        return
    }
    const lang = getFileType(file.name)?.lang || 'plaintext'
    isCode.value = lang !== 'markdown'

    // For code files and markdown, try backend tree-sitter API first, then fallback to regex
    if (file?.path) {
        fetchCodeSymbols(file.path).then(result => {
            if (result && result.symbols.length > 0) {
                // Convert backend symbols to TocItem format
                toc.value = result.symbols.map(s => ({
                    level: s.level,
                    text: s.name,
                    kind: s.kind,
                    id: s.kind === 'heading' ? slugify(s.name) : 'toc-l' + s.line,
                    line: s.line,
                }))
            } else {
                // Fallback to regex-based extraction
                toc.value = extractToc(file.content, lang)
            }
            activeId.value = toc.value[0]?.id || ''
            searchQuery.value = ''
            filteredToc.value = toc.value
        }).catch(() => {
            // Fallback to regex-based extraction on error
            toc.value = extractToc(file.content, lang)
            activeId.value = toc.value[0]?.id || ''
            searchQuery.value = ''
            filteredToc.value = toc.value
        })
    } else {
        toc.value = extractToc(file.content, lang)
        activeId.value = toc.value[0]?.id || ''
        searchQuery.value = ''
        filteredToc.value = toc.value
    }
}, { immediate: true })

watch(searchQuery, () => handleSearch())

function handleSearch() {
    const query = searchQuery.value.toLowerCase().trim()
    if (!query) {
        filteredToc.value = toc.value
        return
    }
    filteredToc.value = toc.value.filter(item =>
        item.text.toLowerCase().includes(query)
    )
}

function clearSearch() {
    searchQuery.value = ''
    filteredToc.value = toc.value
}

function kindIcon(kind) {
    return KIND_ICON_MAP[kind] || KIND_FALLBACK
}

function scrollTo(item) {
    // PDF: jump to page number
    if (isPdfOutline.value && item.line > 0) {
        emit('jumpPage', item.line)
        activeId.value = item.id
        emit('close')
        return
    }

    const elById = document.getElementById(item.id)
    if (elById) {
        elById.scrollIntoView({ behavior: 'smooth', block: 'start' })
        elById.classList.add('line-flash')
        elById.addEventListener('animationend', () => elById.classList.remove('line-flash'), { once: true })
        activeId.value = item.id
        emit('close')
        return
    }
    if (item.line) {
        emit('jump', item.line)
    }
    emit('close')
}

let observer = null
watch(() => props.open, (val) => {
    if (!val) {
        observer?.disconnect()
        return
    }
    // No IntersectionObserver for PDF outline (pages are in PdfPreview's scroll container)
    if (isPdfOutline.value) return

    nextTick(() => {
        observer?.disconnect()
        if (isCode.value) {
            observer = new IntersectionObserver((entries) => {
                for (const entry of entries) {
                    if (entry.isIntersecting) {
                        const line = entry.target.getAttribute('data-line')
                        const match = toc.value.find(t => t.line == line)
                        if (match) activeId.value = match.id
                        break
                    }
                }
            }, { rootMargin: '-60px 0px -70% 0px' })
            toc.value.forEach(item => {
                const el = document.querySelector(`[data-line="${item.line}"]`)
                if (el) observer.observe(el)
            })
        } else {
            observer = new IntersectionObserver((entries) => {
                for (const entry of entries) {
                    if (entry.isIntersecting) {
                        activeId.value = entry.target.id
                        break
                    }
                }
            }, { rootMargin: '-60px 0px -70% 0px' })
            toc.value.forEach(item => {
                const el = document.getElementById(item.id)
                if (el) observer.observe(el)
            })
        }
    })
})
</script>

<style scoped>
.toc-header-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
}

.toc-body {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-height: 0;
    padding: 8px 6px 0;
}

.toc-list {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    -webkit-overflow-scrolling: touch;
    padding-bottom: 8px;
}

.toc-empty {
    text-align: center;
    padding: 32px 16px;
    color: var(--text-muted);
    font-size: 13px;
}

.toc-item {
    display: block;
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: 13px;
    color: var(--text-secondary);
    transition: background 0.15s, color 0.15s;
    border-left: 2px solid transparent;
    white-space: nowrap;
    text-decoration: none;
    overflow: hidden;
    text-overflow: ellipsis;
}
.toc-item:hover { background: var(--bg-tertiary); color: var(--accent-color); }
.toc-item.active { color: var(--accent-color); border-left-color: var(--accent-color); background: var(--bg-tertiary); border-radius: 0; }
.toc-item[data-level="2"] { padding-left: 20px; }
.toc-item[data-level="3"] { padding-left: 32px; }
.toc-item[data-level="4"] { padding-left: 44px; }
.toc-item[data-level="5"] { padding-left: 56px; }
.toc-item[data-level="6"] { padding-left: 68px; }

.toc-page-badge {
    display: inline-block;
    font-size: 10px;
    font-weight: 600;
    background: var(--bg-tertiary);
    color: var(--text-muted);
    padding: 1px 5px;
    border-radius: 3px;
    margin-right: 4px;
    flex-shrink: 0;
    vertical-align: middle;
}

.toc-item.active .toc-page-badge {
    background: rgba(255,255,255,0.15);
    color: var(--accent-color);
}

.toc-kind-icon {
    flex-shrink: 0;
    margin-right: 5px;
    vertical-align: middle;
    opacity: 0.75;
}
.toc-item.active .toc-kind-icon { opacity: 1; }

.kind-function, .kind-method     { color: #c586c0; }
.kind-constructor                { color: #dcdcaa; }
.kind-class                      { color: #e06c75; }
.kind-struct                     { color: #e5a54a; }
.kind-interface                  { color: #4ec9b0; }
.kind-type                       { color: #2ec4b6; }
.kind-enum                       { color: #d4a017; }
.kind-variable                   { color: #75aadb; }
.kind-constant                   { color: #8899aa; }
.kind-module, .kind-namespace    { color: #569cd6; }
.kind-field, .kind-property      { color: #9cdcfe; }
.kind-trait                      { color: #6a9955; }
.kind-impl                       { color: #4fb3bf; }
.kind-macro                      { color: #e5c07b; }
.kind-heading                    { color: #56b6c2; }
.kind-other                      { color: var(--text-muted); }

</style>
