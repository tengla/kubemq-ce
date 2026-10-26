<script setup>
import { computed } from 'vue'
import Icon from '../components/Icon.vue'
import NodeCard from '../components/NodeCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatTile from '../components/StatTile.vue'
import ThroughputChart from '../components/ThroughputChart.vue'
import { CHANNEL_TYPES, channelPath, typeKeyFromLabel } from '../channelTypes'
import { formatBytes, formatCompact, formatRate, snapshot } from '../snapshot'

const online = computed(() => !snapshot.error && snapshot.data != null)
const healthy = computed(() => online.value && snapshot.host?.status === 'Running')
const healthState = computed(() => (healthy.value ? 'good' : snapshot.starting ? 'wait' : 'bad'))
const latest = computed(() => snapshot.samples.at(-1))
const total = computed(() => snapshot.data?.total ?? {})
const trend = (key) => snapshot.samples.map((s) => s[key])

const tiles = computed(() => {
  const d = snapshot.data ?? {}
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
    const c = snapshot.byType?.[type.key] ?? {}
    return { ...type, ...c, rate: latest.value?.rates[type.key] ?? 0 }
  }),
)

const topChannels = computed(() =>
  (snapshot.data?.topChannels ?? []).map((c) => {
    const typeKey = typeKeyFromLabel(c.type)
    return { ...c, to: typeKey ? channelPath(typeKey, c.channel) : null }
  }),
)
</script>

<template>
  <div class="page">
    <PageHeader title="Home" icon="home">
      <template #actions>
        <p class="activity">
          <span class="muted">Last activity</span>
          <span class="divider" />
          <strong>{{ snapshot.data?.lastActivityHuman ?? '-' }}</strong>
        </p>
      </template>
    </PageHeader>

    <section class="health" aria-live="polite">
      <span class="health-icon" :class="healthState"><Icon :name="healthy ? 'check' : snapshot.starting ? 'clock' : 'cross'" :size="34" /></span>
      <div>
        <p class="health-title" :class="healthState">
          {{ snapshot.loading ? 'Connecting…' : healthy ? 'Healthy' : snapshot.starting ? 'Starting…' : online ? snapshot.host?.status ?? 'Not ready' : 'Unreachable' }}
        </p>
        <p class="muted">
          <template v-if="online">{{ snapshot.host?.role ?? 'Standalone' }} · 1 node</template>
          <template v-else-if="snapshot.starting">The broker is starting. This page updates when it's ready.</template>
          <template v-else-if="snapshot.error">Cannot load /api/snapshot: {{ snapshot.error }}</template>
        </p>
      </div>
    </section>

    <section class="tiles" aria-label="Key metrics">
      <StatTile v-for="t in tiles" :key="t.label" v-bind="t" />
    </section>

    <div class="row">
      <NodeCard :host="snapshot.host" :samples="snapshot.samples" />
      <section class="card panel">
        <h2 class="card-title">Throughput</h2>
        <p class="sub muted">Incoming messages per second by channel type</p>
        <ThroughputChart :samples="snapshot.samples" :series="CHANNEL_TYPES" />
      </section>
    </div>

    <div class="row two">
      <section class="card panel">
        <h2 class="card-title">By channel type</h2>
        <div class="table-scroll">
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
                  <RouterLink :to="r.path" class="type-link"><span class="swatch" :style="{ background: r.color }" />{{ r.label }}</RouterLink>
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
        <div class="table-scroll">
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
              <tr v-for="c in topChannels" :key="c.channelKey">
                <td class="strong">
                  <RouterLink v-if="c.to" :to="c.to" class="type-link">{{ c.channel }}</RouterLink>
                  <template v-else>{{ c.channel }}</template>
                </td>
                <td>{{ c.type }}</td>
                <td class="right num">{{ c.sent }}</td>
                <td class="right num">{{ c.delivered }}</td>
                <td class="right">{{ c.lastActivity }}</td>
              </tr>
              <tr v-if="!topChannels.length">
                <td colspan="5" class="muted">No channel activity yet.</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 22px;
}
.page :deep(.page-header) {
  margin-bottom: 0;
}
.activity {
  margin: 0;
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
.health-icon.wait,
.health-title.wait {
  color: var(--text-secondary);
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
/* The two tables need about 620px each, so they sit side by side only on wide screens. */
@media (max-width: 1600px) {
  .row.two {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 1200px) {
  .row {
    grid-template-columns: minmax(0, 1fr);
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
