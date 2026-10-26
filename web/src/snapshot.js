import { onBeforeUnmount, onMounted, reactive } from 'vue'

// The server rebuilds its snapshot every 5s (services/api saveInterval) and
// serves the cached copy, so polling faster only catches each one sooner.
const POLL_MS = 2500
const HISTORY_SAMPLES = 72 // 6 minutes at the server's 5s cadence

export const CHANNEL_TYPES = [
  { key: 'queues', label: 'Queues', group: 'queues', href: '/queues', icon: 'queue', color: 'var(--series-1)' },
  { key: 'events', label: 'Events', group: 'pubsub', href: '/pubsub', icon: 'zap', color: 'var(--series-2)' },
  { key: 'events_store', label: 'Events Store', group: 'pubsub', href: '/pubsub', icon: 'database', color: 'var(--series-3)' },
  { key: 'commands', label: 'Commands', group: 'commandsQueries', href: '/cqrs', icon: 'arrow-right', color: 'var(--series-4)' },
  { key: 'queries', label: 'Queries', group: 'commandsQueries', href: '/cqrs', icon: 'arrows', color: 'var(--series-5)' },
]

// lastUpdate is "YYYY-MM-DD HH:MM:SS" in the server's local time. Only
// differences between two stamps are used, so the time zone cancels out.
function parseServerTime(value) {
  return value ? new Date(value.replace(' ', 'T')).getTime() : NaN
}

// Sums each type's cumulative counters across its channels.
function countByType(data) {
  const counts = {}
  for (const type of CHANNEL_TYPES) {
    counts[type.key] = { channels: 0, active: 0, messages: 0, volume: 0, errors: 0, waiting: 0 }
  }
  for (const group of ['queues', 'pubsub', 'commandsQueries']) {
    for (const channel of data[group]?.channelsList ?? []) {
      const c = counts[channel.type]
      if (!c) continue
      c.channels += 1
      if (channel.isActive) c.active += 1
      c.messages += channel.incoming?.messages ?? 0
      c.volume += channel.total?.volume ?? 0
      c.errors += channel.total?.errors ?? 0
      c.waiting += channel.outgoing?.waiting ?? 0
    }
  }
  return counts
}

export function useSnapshot() {
  const state = reactive({
    data: null,
    host: null,
    byType: null,
    samples: [],
    error: null,
    loading: true,
  })

  let previous = null
  let timer = null
  let inFlight = false

  function record(data) {
    const host = data.hostsInfo?.[0] ?? null
    const serverTime = parseServerTime(host?.lastUpdate)
    const byType = countByType(data)
    state.data = data
    state.host = host
    state.byType = byType

    if (previous && previous.stamp === host?.lastUpdate) return

    const rates = {}
    let totalRate = 0
    if (previous) {
      const seconds = (serverTime - previous.serverTime) / 1000
      for (const type of CHANNEL_TYPES) {
        const delta = byType[type.key].messages - previous.byType[type.key].messages
        // Counters reset when the server restarts; treat that as no traffic.
        const rate = seconds > 0 && delta > 0 ? delta / seconds : 0
        rates[type.key] = rate
        totalRate += rate
      }
    }

    const total = data.total ?? {}
    if (previous) {
      state.samples.push({
        t: Date.now(),
        rates,
        totalRate,
        clients: data.clients ?? 0,
        backlog: data.queues?.total?.waiting ?? 0,
        activeChannels: data.activeChannels ?? 0,
        errorRate: total.messages ? (total.errors / total.messages) * 100 : 0,
        volume: total.volume ?? 0,
      })
      if (state.samples.length > HISTORY_SAMPLES) state.samples.shift()
    }
    previous = { stamp: host?.lastUpdate, serverTime, byType }
  }

  async function poll() {
    if (inFlight) return
    inFlight = true
    try {
      const response = await fetch('/api/snapshot', { cache: 'no-store' })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const body = await response.json()
      if (body.error) throw new Error(body.error_string || 'snapshot error')
      record(body.data)
      state.error = null
    } catch (err) {
      state.error = err.message || String(err)
    } finally {
      state.loading = false
      inFlight = false
    }
  }

  onMounted(() => {
    poll()
    timer = setInterval(poll, POLL_MS)
  })
  onBeforeUnmount(() => clearInterval(timer))

  return state
}

const compact = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })

export function formatCompact(value) {
  return compact.format(value ?? 0)
}

export function formatRate(value) {
  if (!value) return '0'
  if (value < 10) return value.toFixed(1)
  return formatCompact(Math.round(value))
}

export function formatBytes(bytes) {
  const units = ['B', 'kB', 'MB', 'GB', 'TB']
  let value = bytes ?? 0
  let unit = 0
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000
    unit += 1
  }
  return `${unit === 0 ? value : value.toFixed(1)} ${units[unit]}`
}
