<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from './components/Icon.vue'
import NodeCard from './components/NodeCard.vue'
import Sidebar from './components/Sidebar.vue'
import StatTile from './components/StatTile.vue'
import ThroughputChart from './components/ThroughputChart.vue'
import { CHANNEL_TYPES, formatBytes, formatCompact, formatRate, useSnapshot } from './snapshot'

const BRAND = 'kubemq-ce'

const snap = useSnapshot()
const online = computed(() => !snap.error && snap.data != null)
const healthy = computed(() => online.value && snap.host?.status === 'Running')
const latest = computed(() => snap.samples.at(-1))
const total = computed(() => snap.data?.total ?? {})
const trend = (key) => snap.samples.map((s) => s[key])

const tiles = computed(() => {
  const d = snap.data ?? {}
  const t = total.value
  const errorRate = t.messages ? (t.errors / t.messages) * 100 : 0
  return [
    { label: 'Throughput', icon: 'activity', value: formatRate(latest.value?.totalRate ?? 0), unit: 'msg/s', caption: 'incoming, all channels', trend: trend('totalRate') },
    { label: 'Clients', icon: 'users', value: formatCompact(d.clients), caption: 'seen by the broker', trend: trend('clients') },
    { label: 'Backlog', icon: 'layers', value: formatCompact(d.queues?.total?.waiting), caption: 'waiting in queues', trend: trend('backlog') },
    { label: 'Channels', icon: 'radio', value: formatCompact(d.activeChannels), unit: `/ ${formatCompact(d.channels)}`, caption: 'active of total', trend: trend('activeChannels') },
    { label: 'Error rate', icon: 'alert', value: `${errorRate.toFixed(2)}%`, caption: `${formatCompact(t.errors)} of ${formatCompact(t.messages)} messages`, trend: trend('errorRate') },
    { label: 'Volume', icon: 'disk', value: formatBytes(t.volume), caption: `${formatCompact(t.messages)} messages`, trend: trend('volume') },
  ]
})

const typeRows = computed(() =>
  CHANNEL_TYPES.map((type) => {
    const c = snap.byType?.[type.key] ?? {}
    return { ...type, ...c, rate: latest.value?.rates[type.key] ?? 0 }
  }),
)

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
</script>

<template>
  <div class="layout">
    <Sidebar
      :brand="BRAND"
      :host="snap.host"
      :by-type="snap.byType"
      :online="online"
      :dark="dark"
      :open="menuOpen"
      @toggle-theme="toggleTheme"
    />
    <div v-if="menuOpen" class="scrim" @click="menuOpen = false" />

    <main class="main">
      <header class="top">
        <button type="button" class="menu" aria-label="Open navigation" @click="menuOpen = true"><Icon name="menu" /></button>
        <h1><Icon name="home" :size="26" />Home</h1>
        <p class="activity">
          <span class="muted">Last activity</span>
          <span class="divider" />
          <strong>{{ snap.data?.lastActivityHuman ?? '-' }}</strong>
        </p>
      </header>

      <section class="health" aria-live="polite">
        <span class="health-icon" :class="healthy ? 'good' : 'bad'"><Icon :name="healthy ? 'check' : 'cross'" :size="34" /></span>
        <div>
          <p class="health-title" :class="healthy ? 'good' : 'bad'">
            {{ snap.loading ? 'Connecting…' : healthy ? 'Healthy' : online ? snap.host?.status ?? 'Not ready' : 'Unreachable' }}
          </p>
          <p class="muted">
            <template v-if="online">{{ snap.host?.role ?? 'Standalone' }} · 1 node</template>
            <template v-else-if="snap.error">Cannot load /api/snapshot: {{ snap.error }}</template>
          </p>
        </div>
      </section>

      <section class="tiles" aria-label="Key metrics">
        <StatTile v-for="t in tiles" :key="t.label" v-bind="t" />
      </section>

      <div class="row">
        <NodeCard :host="snap.host" :samples="snap.samples" />
        <section class="card panel">
          <h2 class="card-title">Throughput</h2>
          <p class="sub muted">Incoming messages per second by channel type</p>
          <ThroughputChart :samples="snap.samples" :series="CHANNEL_TYPES" />
        </section>
      </div>

      <div class="row two">
        <section class="card panel">
          <h2 class="card-title">By channel type</h2>
          <div class="scroll">
            <table class="data">
              <thead>
                <tr>
                  <th>Type</th>
                  <th class="right">Channels</th>
                  <th class="right">Msg/s</th>
                  <th class="right">Messages</th>
                  <th class="right">Volume</th>
                  <th class="right">Waiting</th>
                  <th class="right">Errors</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="r in typeRows" :key="r.key">
                  <td class="strong">
                    <a :href="r.href" class="type-link"><span class="swatch" :style="{ background: r.color }" />{{ r.label }}</a>
                  </td>
                  <td class="right num">{{ r.active ?? 0 }}/{{ r.channels ?? 0 }}</td>
                  <td class="right num">{{ formatRate(r.rate) }}</td>
                  <td class="right num">{{ formatCompact(r.messages) }}</td>
                  <td class="right num">{{ formatBytes(r.volume) }}</td>
                  <td class="right num">{{ formatCompact(r.waiting) }}</td>
                  <td class="right num">{{ formatCompact(r.errors) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section class="card panel">
          <h2 class="card-title">Top channels</h2>
          <div class="scroll">
            <table class="data">
              <thead>
                <tr>
                  <th>Channel</th>
                  <th>Type</th>
                  <th class="right">Sent</th>
                  <th class="right">Delivered</th>
                  <th class="right">Last activity</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="c in snap.data?.topChannels ?? []" :key="c.channelKey">
                  <td class="strong">{{ c.channel }}</td>
                  <td>{{ c.type }}</td>
                  <td class="right num">{{ c.sent }}</td>
                  <td class="right num">{{ c.delivered }}</td>
                  <td class="right">{{ c.lastActivity }}</td>
                </tr>
                <tr v-if="!(snap.data?.topChannels ?? []).length">
                  <td colspan="5" class="muted">No channel activity yet.</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </main>
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
  display: flex;
  flex-direction: column;
  gap: 22px;
}
.top {
  display: flex;
  align-items: center;
  gap: 16px;
}
.top h1 {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0;
  font-size: 1.9rem;
  font-weight: 700;
}
.menu {
  display: none;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface-1);
  width: 38px;
  height: 38px;
  place-items: center;
  cursor: pointer;
}
.activity {
  margin: 0 0 0 auto;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 18px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface-1);
  font-size: 0.875rem;
  white-space: nowrap;
}
.divider {
  width: 1px;
  height: 16px;
  background: var(--border);
}
.health {
  display: flex;
  align-items: center;
  gap: 14px;
}
.health p {
  margin: 0;
}
.health-icon.good,
.health-title.good {
  color: var(--good-text);
}
.health-icon.bad,
.health-title.bad {
  color: var(--critical);
}
.health-icon {
  display: grid;
}
.health-title {
  font-size: 1.3rem;
  font-weight: 600;
}
.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  gap: 14px;
}
.row {
  display: grid;
  grid-template-columns: minmax(300px, 360px) minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}
.row.two {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
}
.panel {
  padding: 18px 20px;
  min-width: 0;
}
.sub {
  margin: 2px 0 14px;
  font-size: 0.85rem;
}
.scroll {
  overflow-x: auto;
  margin: 10px -12px 0;
}
.type-link {
  display: inline-flex;
  align-items: center;
  gap: 10px;
}
.type-link:hover {
  text-decoration: underline;
}
.swatch {
  width: 10px;
  height: 10px;
  border-radius: 3px;
  flex: none;
}
.scrim {
  display: none;
}

@media (max-width: 1200px) {
  .row,
  .row.two {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 900px) {
  .main {
    padding: 16px 16px 40px;
  }
  .menu {
    display: grid;
  }
  .top h1 {
    font-size: 1.5rem;
  }
  .scrim {
    display: block;
    position: fixed;
    inset: 0;
    z-index: 10;
    background: rgba(0, 0, 0, 0.4);
  }
}
@media (max-width: 560px) {
  .activity .muted,
  .activity .divider {
    display: none;
  }
  .tiles {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
