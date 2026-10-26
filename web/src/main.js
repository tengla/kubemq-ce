import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { loadSession } from './session'
import { startSnapshot } from './snapshot'
import './styles.css'

createApp(App).use(router).mount('#app')
// The first poll waits for the session, so a signed-out viewer gets the
// login page instead of a 401 error.
loadSession().finally(startSnapshot)
