import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'
import { LongPressDirective } from './directives/longPress.ts'
import { configureMarkedRenderer } from './utils/markedConfig.ts'

configureMarkedRenderer()

createApp(App).use(i18n).directive('long-press', LongPressDirective).mount('#app')
