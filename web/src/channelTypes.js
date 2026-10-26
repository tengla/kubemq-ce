// One entry per channel type. `group` is the snapshot section that lists the
// type's channels; `monitorKind` is the kind the /api/monitor socket expects.
export const CHANNEL_TYPES = [
  { key: 'queues', label: 'Queues', noun: 'queue', path: '/queues', group: 'queues', icon: 'queue', color: 'var(--series-1)', monitorKind: 'queue' },
  { key: 'events', label: 'Events', noun: 'events channel', path: '/events', group: 'pubsub', icon: 'zap', color: 'var(--series-2)', monitorKind: 'events' },
  { key: 'events_store', label: 'Events Store', noun: 'events store channel', path: '/events-store', group: 'pubsub', icon: 'database', color: 'var(--series-3)', monitorKind: 'events_store' },
  { key: 'commands', label: 'Commands', noun: 'commands channel', path: '/commands', group: 'commandsQueries', icon: 'arrow-right', color: 'var(--series-4)', monitorKind: 'commands' },
  { key: 'queries', label: 'Queries', noun: 'queries channel', path: '/queries', group: 'commandsQueries', icon: 'arrows', color: 'var(--series-5)', monitorKind: 'queries' },
]

export const TYPES_BY_KEY = Object.fromEntries(CHANNEL_TYPES.map((t) => [t.key, t]))

// The snapshot's topChannels list labels types differently.
const TOP_CHANNEL_TYPES = { Queue: 'queues', Events: 'events', 'Events-Store': 'events_store', Commands: 'commands', Queries: 'queries' }

export function typeKeyFromLabel(label) {
  return TOP_CHANNEL_TYPES[label]
}

export function channelPath(typeKey, name) {
  return `${TYPES_BY_KEY[typeKey].path}/${encodeURIComponent(name)}`
}
