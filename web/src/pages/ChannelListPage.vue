<script setup>
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import AddChannelDialog from '../components/AddChannelDialog.vue'
import Icon from '../components/Icon.vue'
import PageHeader from '../components/PageHeader.vue'
import SendMessageDialog from '../components/SendMessageDialog.vue'
import StatStrip from '../components/StatStrip.vue'
import { TYPES_BY_KEY, channelPath } from '../channelTypes'
import { channelRate, channelsOfType, formatBytes, formatCompact, formatRate, snapshot } from '../snapshot'

const route = useRoute()
const typeKey = route.meta.type
const type = TYPES_BY_KEY[typeKey]
const isQueue = typeKey === 'queues'

const search = ref('')
const activeOnly = ref(false)
const sortKey = ref('lastActivity')
const sortDesc = ref(true)
const sendOpen = ref(false)
const addOpen = ref(false)

const rows = computed(() =>
  channelsOfType(typeKey).map((c) => ({
    name: c.name,
    to: channelPath(typeKey, c.name),
    active: c.isActive,
    rate: channelRate(typeKey, c.name),
    sent: c.incoming?.messages ?? 0,
    sentVolume: c.incoming?.volume ?? 0,
    delivered: c.outgoing?.messages ?? 0,
    deliveredVolume: c.outgoing?.volume ?? 0,
    waiting: c.outgoing?.waiting ?? 0,
    expired: c.total?.expired ?? 0,
    delayed: c.total?.delayed ?? 0,
    errors: c.total?.errors ?? 0,
    clients: c.clients?.length ?? 0,
    lastActivity: c.lastActivity ?? 0,
    lastActivityHuman: c.lastActivityHuman ?? '',
  })),
)

const columns = computed(() => [
  { key: 'name', label: 'Channel' },
  { key: 'rate', label: 'Msg/s', numeric: true },
  { key: 'sent', label: 'Sent', numeric: true },
  { key: 'delivered', label: 'Delivered', numeric: true },
  ...(isQueue
    ? [
        { key: 'waiting', label: 'Waiting', numeric: true },
        { key: 'expired', label: 'Expired', numeric: true },
        { key: 'delayed', label: 'Delayed', numeric: true },
      ]
    : []),
  { key: 'errors', label: 'Errors', numeric: true },
  { key: 'clients', label: 'Clients', numeric: true },
  { key: 'lastActivity', label: 'Last activity', numeric: true },
])

const visible = computed(() => {
  const query = search.value.trim().toLowerCase()
  const list = rows.value.filter((r) => (!activeOnly.value || r.active) && (!query || r.name.toLowerCase().includes(query)))
  const dir = sortDesc.value ? -1 : 1
  return list.sort((a, b) => {
    const x = a[sortKey.value]
    const y = b[sortKey.value]
    return (typeof x === 'string' ? x.localeCompare(y) : x - y) * dir
  })
})

function sortBy(key) {
  if (sortKey.value === key) sortDesc.value = !sortDesc.value
  else {
    sortKey.value = key
    sortDesc.value = key !== 'name'
  }
}

const totals = computed(() => {
  const list = rows.value
  const sum = (k) => list.reduce((acc, r) => acc + r[k], 0)
  return {
    channels: list.length,
    active: list.filter((r) => r.active).length,
    sent: sum('sent'),
    sentVolume: sum('sentVolume'),
    delivered: sum('delivered'),
    deliveredVolume: sum('deliveredVolume'),
    waiting: sum('waiting'),
    errors: sum('errors'),
    rate: sum('rate'),
  }
})

const stats = computed(() => {
  const t = totals.value
  return [
    { label: 'Channels', value: `${t.active} / ${t.channels}`, caption: 'active / total' },
    { label: 'Throughput', value: `${formatRate(t.rate)} msg/s`, caption: 'incoming' },
    { label: 'Sent', value: formatCompact(t.sent), caption: formatBytes(t.sentVolume) },
    { label: 'Delivered', value: formatCompact(t.delivered), caption: formatBytes(t.deliveredVolume) },
    ...(isQueue ? [{ label: 'Waiting', value: formatCompact(t.waiting), caption: 'messages' }] : []),
    { label: 'Errors', value: formatCompact(t.errors), caption: 'all time' },
  ]
})

function cell(row, key) {
  switch (key) {
    case 'rate':
      return formatRate(row.rate)
    case 'sent':
      return `${formatCompact(row.sent)} · ${formatBytes(row.sentVolume)}`
    case 'delivered':
      return `${formatCompact(row.delivered)} · ${formatBytes(row.deliveredVolume)}`
    case 'lastActivity':
      return row.lastActivityHuman
    default:
      return formatCompact(row[key])
  }
}
</script>

<template>
  <div>
    <PageHeader :title="type.label" :icon="type.icon">
      <template #subtitle>
        <span>{{ totals.channels }} {{ totals.channels === 1 ? 'channel' : 'channels' }}</span>
      </template>
      <template #actions>
        <button type="button" class="btn" @click="addOpen = true"><Icon name="plus" :size="16" />New channel</button>
        <button type="button" class="btn btn-primary" @click="sendOpen = true"><Icon name="send" :size="16" />Send message</button>
      </template>
    </PageHeader>

    <StatStrip :items="stats" />

    <section class="card panel">
      <div class="toolbar">
        <label class="search">
          <Icon name="search" :size="16" />
          <span class="sr-only">Search channels</span>
          <input v-model="search" class="input" type="search" placeholder="Search channels" />
        </label>
        <label class="checkbox"><input v-model="activeOnly" type="checkbox" />Active only</label>
      </div>

      <div v-if="snapshot.loading" class="empty-state">Loading…</div>
      <div v-else-if="!rows.length" class="empty-state">
        <p>No {{ type.label.toLowerCase() }} channels yet.</p>
        <button type="button" class="btn" @click="addOpen = true"><Icon name="plus" :size="16" />New channel</button>
      </div>
      <div v-else class="table-scroll">
        <table class="data">
          <thead>
            <tr>
              <th v-for="col in columns" :key="col.key" :class="{ right: col.numeric }" :aria-sort="sortKey === col.key ? (sortDesc ? 'descending' : 'ascending') : 'none'">
                <button type="button" class="sort" @click="sortBy(col.key)">
                  {{ col.label }}
                  <span class="arrow" aria-hidden="true">{{ sortKey === col.key ? (sortDesc ? '↓' : '↑') : '' }}</span>
                </button>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in visible" :key="row.name">
              <td class="strong">
                <RouterLink :to="row.to" class="channel">
                  <span class="status-dot" :style="{ background: row.active ? 'var(--good)' : 'var(--axis)' }" />
                  <span class="name">{{ row.name }}</span>
                  <span class="sr-only">{{ row.active ? '(active)' : '(idle)' }}</span>
                </RouterLink>
              </td>
              <td v-for="col in columns.slice(1)" :key="col.key" class="right num">{{ cell(row, col.key) }}</td>
            </tr>
            <tr v-if="!visible.length">
              <td :colspan="columns.length" class="muted">No channels match.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <SendMessageDialog :open="sendOpen" :type="typeKey" @close="sendOpen = false" />
    <AddChannelDialog :open="addOpen" :type="typeKey" @close="addOpen = false" />
  </div>
</template>

<style scoped>
.panel {
  padding: 16px 20px 10px;
}
.toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px 18px;
}
.search {
  position: relative;
  display: flex;
  align-items: center;
  flex: 0 1 320px;
  color: var(--text-muted);
}
.search :deep(svg) {
  position: absolute;
  left: 12px;
  pointer-events: none;
}
.search .input {
  padding-left: 36px;
}
.sort {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0;
  border: none;
  background: none;
  color: inherit;
  font: inherit;
  cursor: pointer;
}
th.right .sort {
  flex-direction: row-reverse;
}
.arrow {
  width: 10px;
  color: var(--text-primary);
}
.channel {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  max-width: 320px;
}
.channel:hover .name {
  text-decoration: underline;
}
.name {
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
