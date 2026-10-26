<script setup>
import { computed, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import Sidebar from './components/Sidebar.vue'
import Toasts from './components/Toasts.vue'
import { BRAND } from './router'
import { snapshot } from './snapshot'

const route = useRoute()
const online = computed(() => !snapshot.error && snapshot.data != null)

// Theme: follows the OS until the viewer picks one; the pick is remembered.
const systemDark = window.matchMedia('(prefers-color-scheme: dark)')
const chosenTheme = ref(null)
const osDark = ref(systemDark.matches)
const dark = computed(() => (chosenTheme.value ? chosenTheme.value === 'dark' : osDark.value))
const onSystemChange = (e) => (osDark.value = e.matches)

function applyTheme() {
  if (chosenTheme.value) document.documentElement.dataset.theme = chosenTheme.value
  else delete document.documentElement.dataset.theme
}

function toggleTheme() {
  chosenTheme.value = dark.value ? 'light' : 'dark'
  applyTheme()
  try {
    localStorage.setItem('theme', chosenTheme.value)
  } catch {
    // Storage can be unavailable (private mode); the toggle still works for this visit.
  }
}

onMounted(() => {
  try {
    const saved = localStorage.getItem('theme')
    if (saved === 'light' || saved === 'dark') chosenTheme.value = saved
  } catch {
    // Ignore unavailable storage.
  }
  applyTheme()
  systemDark.addEventListener('change', onSystemChange)
})
onBeforeUnmount(() => systemDark.removeEventListener('change', onSystemChange))

const menuOpen = ref(false)
watch(() => route.fullPath, () => (menuOpen.value = false))
provide('openMenu', () => (menuOpen.value = true))
</script>

<template>
  <div class="layout">
    <Sidebar
      :brand="BRAND"
      :host="snapshot.host"
      :by-type="snapshot.byType"
      :online="online"
      :dark="dark"
      :open="menuOpen"
      @toggle-theme="toggleTheme"
    />
    <div v-if="menuOpen" class="scrim" @click="menuOpen = false" />
    <main class="main">
      <RouterView v-slot="{ Component }">
        <component :is="Component" :key="route.path" />
      </RouterView>
    </main>
    <Toasts />
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  min-height: 100vh;
}
.main {
  flex: 1;
  min-width: 0;
  padding: 28px 32px 48px;
}
.scrim {
  display: none;
}
@media (max-width: 900px) {
  .main {
    padding: 16px 16px 40px;
  }
  .scrim {
    display: block;
    position: fixed;
    inset: 0;
    z-index: 10;
    background: rgba(0, 0, 0, 0.4);
  }
}
</style>
