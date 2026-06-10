import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h, defineComponent, ref, computed, watch } from 'vue'

// Recreate TabPanel using defineComponent (non-SFC)
const TabPanelNonSFC = defineComponent({
  props: {
    tabId: { type: String, required: true },
    activeTab: { type: String, required: true },
    title: { type: String, default: '' },
    noHeader: Boolean,
  },
  emits: ['header-click'],
  setup(props, { slots, emit }) {
    const isActive = computed(() => props.activeTab === props.tabId)
    const everOpened = ref(false)
    watch(isActive, (val) => {
      if (val) everOpened.value = true
    }, { immediate: true })
    function handleHeaderClick() {
      emit('header-click')
    }
    return { isActive, everOpened, handleHeaderClick }
  },
  template: `
    <div v-if="everOpened" v-show="isActive" class="tab-panel" :class="{ 'tab-panel-active': isActive }">
      <div v-if="!noHeader" class="bs-header" @click="handleHeaderClick">
        <slot name="header">
          <span class="bs-title">{{ title }}</span>
        </slot>
      </div>
      <div class="bs-body">
        <slot />
      </div>
      <footer v-if="$slots.footer" class="bs-footer">
        <slot name="footer" />
      </footer>
    </div>
  `,
})

describe('Non-SFC TabPanel', () => {
  it('renders with default slot', async () => {
    const wrapper = mount(TabPanelNonSFC, {
      props: { tabId: 'chat', activeTab: 'chat' },
      slots: { default: '<div class="inner">Hello</div>' },
    })
    console.log('html:', wrapper.html())
    expect(wrapper.find('.inner').text()).toBe('Hello')
  })
})
