import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { startSnapshot } from './snapshot'
import './styles.css'

startSnapshot()
createApp(App).use(router).mount('#app')
