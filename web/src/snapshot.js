import { reactive } from 'vue'
import { CHANNEL_TYPES, TYPES_BY_KEY } from './channelTypes'

// The server rebuilds its snapshot every 5s (services/api saveInterval) and
// serves the cached copy, so polling faster only catches each one sooner.
const POLL_MS = 2500
const HISTORY_SAMPLES = 72 // 6 minutes at the server's 5s cadence
const GROUPS = ['queues', 'pubsub', 'commandsQueries']

// One shared store for every page, so history survives navigation.
export const snapshot = reactive({
  data: null,
  host: null,
  byType: null,
  samples: [],
  channelRates: {}, // "type/name" -> msg/s
  channelHistory: {}, // "type/name" -> [msg/s, ...]
  error: null,
  starting: false, // the server is up but the broker is not ready yet (503)
  loading: true,
})

let previous = null
let inFlight = false
let started = false

// lastUpdate is "YYYY-MM-DD HH:MM:SS" in the server's local time. Only
// differences between two stamps are used, so the time zone cancels out.
function parseServerTime(value) {
  return value ? new Date(value.replace(' ', 'T')).getTime() : NaN
}

function channelKey(channel) {
  return `${channel.type}/${channel.name}`
}

function allChannels(data) {
  return GROUPS.flatMap((group) => data[group]?.channelsList ?? [])
}

// Sums each type's cumulative counters across its channels.
function countByType(data) {
  const counts = {}
  for (const type of CHANNEL_TYPES) {
    counts[type.key] = { channels: 0, active: 0, messages: 0, volume: 0, errors: 0, waiting: 0 }
  }
  for (const channel of allChannels(data)) {
    const c = counts[channel.type]
    if (!c) continue
    c.channels += 1
    if (channel.isActive) c.active += 1
    c.messages += channel.incoming?.messages ?? 0
    c.volume += channel.total?.volume ?? 0
    c.errors += channel.total?.errors ?? 0
    c.waiting += channel.outgoing?.waiting ?? 0
  }
  return counts
}

function rate(current, before, seconds) {
  const delta = current - before
  // Counters reset when the server restarts; treat that as no traffic.
  return seconds > 0 && delta > 0 ? delta / seconds : 0
}

function record(data) {
  const host = data.hostsInfo?.[0] ?? null
  const serverTime = parseServerTime(host?.lastUpdate)
  const byType = countByType(data)
  snapshot.data = data
  snapshot.host = host
  snapshot.byType = byType

  if (previous && previous.stamp === host?.lastUpdate) return

  const channelCounts = {}
  for (const channel of allChannels(data)) channelCounts[channelKey(channel)] = channel.incoming?.messages ?? 0

  if (previous) {
    const seconds = (serverTime - previous.serverTime) / 1000
    const rates = {}
    let totalRate = 0
    for (const type of CHANNEL_TYPES) {
      rates[type.key] = rate(byType[type.key].messages, previous.byType[type.key].messages, seconds)
      totalRate += rates[type.key]
    }

    const channelRates = {}
    for (const [key, count] of Object.entries(channelCounts)) {
      channelRates[key] = rate(count, previous.channelCounts[key] ?? count, seconds)
      const history = snapshot.channelHistory[key] ?? []
      history.push(channelRates[key])
      if (history.length > HISTORY_SAMPLES) history.shift()
      snapshot.channelHistory[key] = history
    }
    snapshot.channelRates = channelRates

    const total = data.total ?? {}
    snapshot.samples.push({
      t: Date.now(),
      rates,
      totalRate,
      clients: data.clients ?? 0,
      backlog: data.queues?.total?.waiting ?? 0,
      activeChannels: data.activeChannels ?? 0,
      errorRate: total.messages ? (total.errors / total.messages) * 100 : 0,
      volume: total.volume ?? 0,
    })
    if (snapshot.samples.length > HISTORY_SAMPLES) snapshot.samples.shift()
  }
  previous = { stamp: host?.lastUpdate, serverTime, byType, channelCounts }
}

export async function pollSnapshot() {
  if (inFlight) return
  inFlight = true
  try {
    const response = await fetch('/api/snapshot', { cache: 'no-store' })
    snapshot.starting = response.status === 503
    const body = await response.json().catch(() => null)
    if (!response.ok || !body || body.error) throw new Error(body?.error_string || `HTTP ${response.status}`)
    record(body.data)
    snapshot.error = null
  } catch (err) {
    snapshot.error = err.message || String(err)
  } finally {
    snapshot.loading = false
    inFlight = false
  }
}

export function startSnapshot() {
  if (started) return
  started = true
  pollSnapshot()
  setInterval(pollSnapshot, POLL_MS)
}

export function channelsOfType(typeKey) {
  const group = TYPES_BY_KEY[typeKey].group
  return (snapshot.data?.[group]?.channelsList ?? []).filter((c) => c.type === typeKey)
}

export function findChannel(typeKey, name) {
  return channelsOfType(typeKey).find((c) => c.name === name) ?? null
}

export function channelRate(typeKey, name) {
  return snapshot.channelRates[`${typeKey}/${name}`] ?? 0
}

export function channelHistory(typeKey, name) {
  return snapshot.channelHistory[`${typeKey}/${name}`] ?? []
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
