<script setup>
import Icon from './Icon.vue'
import { CHANNEL_TYPES } from '../channelTypes'

defineProps({
  brand: { type: String, required: true },
  host: { type: Object, default: null },
  byType: { type: Object, default: null },
  online: { type: Boolean, default: false },
  starting: { type: Boolean, default: false },
  dark: { type: Boolean, default: false },
  open: { type: Boolean, default: false },
})
defineEmits(['toggle-theme'])
</script>

<template>
  <aside class="sidebar" :class="{ open }">
    <div class="brand">
      <Icon name="layers" :size="26" />
      <span>{{ brand }}</span>
    </div>
    <div class="node">
      <span class="status-dot" :style="{ background: online ? 'var(--good)' : starting ? 'var(--warning)' : 'var(--critical)' }" />
      <div>
        <div class="node-name" :title="host?.host">{{ host?.host ?? 'Connecting…' }}</div>
        <div class="muted small">{{ online ? host?.role ?? 'Standalone' : starting ? 'Starting…' : 'Unreachable' }}</div>
      </div>
    </div>

    <nav aria-label="Main">
      <RouterLink to="/dashboard" class="item"><Icon name="home" />Home</RouterLink>
      <p class="section muted">Channels</p>
      <RouterLink v-for="t in CHANNEL_TYPES" :key="t.key" :to="t.path" class="item">
        <Icon :name="t.icon" />
        <span class="grow">{{ t.label }}</span>
        <span v-if="byType" class="count muted num" :title="`${byType[t.key].active} active of ${byType[t.key].channels}`">{{ byType[t.key].active }}/{{ byType[t.key].channels }}</span>
      </RouterLink>
    </nav>

    <footer class="foot">
      <span class="muted small">{{ host?.version || '' }}</span>
      <button type="button" class="icon-btn" :aria-label="dark ? 'Switch to light theme' : 'Switch to dark theme'" @click="$emit('toggle-theme')">
        <Icon :name="dark ? 'sun' : 'moon'" />
      </button>
    </footer>
  </aside>
</template>

<style scoped>
.sidebar {
  position: sticky;
  top: 0;
  height: 100vh;
  width: 248px;
  flex: none;
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 20px 14px 16px;
  background: var(--sidebar);
  border-right: 1px solid var(--border);
  overflow-y: auto;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 8px;
  font-size: 1.2rem;
  font-weight: 700;
  color: var(--accent);
}
.brand span {
  color: var(--text-primary);
}
.node {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 8px;
  min-width: 0;
}
.node > div {
  min-width: 0;
}
.node-name {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.small {
  font-size: 0.8rem;
}
nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.section {
  margin: 14px 8px 4px;
  font-size: 0.78rem;
  font-weight: 500;
}
.item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 9px 10px;
  border-radius: 10px;
  color: var(--text-secondary);
}
.item:hover {
  background: var(--surface-2);
  color: var(--text-primary);
}
.item.router-link-active {
  background: var(--accent-wash);
  color: var(--text-primary);
  font-weight: 500;
}
.grow {
  flex: 1;
}
.count {
  font-size: 0.8rem;
}
.foot {
  margin-top: auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 8px;
}
.icon-btn {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: transparent;
  cursor: pointer;
  color: var(--text-secondary);
}
.icon-btn:hover {
  color: var(--text-primary);
  background: var(--surface-2);
}

@media (max-width: 900px) {
  .sidebar {
    position: fixed;
    inset: 0 auto 0 0;
    z-index: 20;
    transform: translateX(-100%);
    transition: transform 0.2s ease;
  }
  .sidebar.open {
    transform: none;
    box-shadow: 0 0 40px rgba(0, 0, 0, 0.35);
  }
}
</style>
