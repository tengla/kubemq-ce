<script setup>
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'
import PageHeader from '../components/PageHeader.vue'
import SendMessageDialog from '../components/SendMessageDialog.vue'
import Sparkline from '../components/Sparkline.vue'
import StatStrip from '../components/StatStrip.vue'
import ClientsTab from '../components/tabs/ClientsTab.vue'
import QueueReceiveTab from '../components/tabs/QueueReceiveTab.vue'
import QueueStreamTab from '../components/tabs/QueueStreamTab.vue'
import RespondTab from '../components/tabs/RespondTab.vue'
import SubscribeTab from '../components/tabs/SubscribeTab.vue'
import WatchTab from '../components/tabs/WatchTab.vue'
import { TYPES_BY_KEY } from '../channelTypes'
import { apiRequest } from '../api'
import { channelHistory, channelRate, findChannel, formatBytes, formatCompact, formatRate, pollSnapshot, snapshot } from '../snapshot'
import { notify } from '../toast'

const props = defineProps({
  name: { type: String, required: true },
})

const route = useRoute()
const router = useRouter()
const typeKey = route.meta.type
const type = TYPES_BY_KEY[typeKey]
const isQueue = typeKey === 'queues'

const channel = computed(() => findChannel(typeKey, props.name))
const rate = computed(() => channelRate(typeKey, props.name))
const history = computed(() => channelHistory(typeKey, props.name))

const TABS = {
  queues: [
    { id: 'peek', label: 'Peek' },
    { id: 'pull', label: 'Pull' },
    { id: 'stream', label: 'Stream' },
    { id: 'watch', label: 'Watch' },
    { id: 'clients', label: 'Clients' },
  ],
  events: [
    { id: 'subscribe', label: 'Subscribe' },
    { id: 'watch', label: 'Watch' },
    { id: 'clients', label: 'Clients' },
  ],
  commands: [
    { id: 'respond', label: 'Respond' },
    { id: 'watch', label: 'Watch' },
    { id: 'clients', label: 'Clients' },
  ],
}
const tabs = TABS[typeKey] ?? (typeKey === 'events_store' ? TABS.events : TABS.commands)
const activeTab = computed(() => (tabs.some((t) => t.id === route.query.tab) ? route.query.tab : tabs[0].id))

function selectTab(id) {
  router.replace({ query: { ...route.query, tab: id } })
}

function onTabKey(event, index) {
  const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
  if (!step) return
  const next = tabs[(index + step + tabs.length) % tabs.length]
  selectTab(next.id)
  document.getElementById(`tab-${next.id}`)?.focus()
}

const stats = computed(() => {
  const c = channel.value
  if (!c) return []
  return [
    { label: 'Throughput', value: `${formatRate(rate.value)} msg/s`, caption: 'incoming' },
    { label: 'Sent', value: formatCompact(c.incoming?.messages), caption: formatBytes(c.incoming?.volume) },
    { label: 'Delivered', value: formatCompact(c.outgoing?.messages), caption: formatBytes(c.outgoing?.volume) },
    ...(isQueue
      ? [
          { label: 'Waiting', value: formatCompact(c.outgoing?.waiting), caption: 'messages' },
          { label: 'Expired', value: formatCompact(c.total?.expired), caption: 'messages' },
          { label: 'Delayed', value: formatCompact(c.total?.delayed), caption: 'messages' },
        ]
      : []),
    { label: 'Errors', value: formatCompact(c.total?.errors), caption: 'all time' },
    { label: 'Clients', value: formatCompact(c.clients?.length), caption: 'seen' },
  ]
})

const sendOpen = ref(false)
const purgeOpen = ref(false)
const purging = ref(false)

async function purge() {
  purging.value = true
  try {
    const res = await apiRequest('purge_queue_channel', { channel: props.name })
    notify(`Cleared ${res?.count ?? 0} ${res?.count === 1 ? 'message' : 'messages'} from ${props.name}`)
    purgeOpen.value = false
    pollSnapshot()
  } catch (err) {
    notify(`Could not clear the queue: ${err.message}`, 'error')
  } finally {
    purging.value = false
  }
}
</script>

<template>
  <div>
    <PageHeader :title="name" :icon="type.icon" :back="{ to: type.path, label: type.label }">
      <template #subtitle>
        <span v-if="channel" class="badge"><span class="status-dot" :style="{ background: channel.isActive ? 'var(--good)' : 'var(--axis)' }" />{{ channel.isActive ? 'Active' : 'Idle' }}</span>
        <span v-if="channel">Last activity {{ channel.lastActivityHuman }}</span>
        <span v-if="channel && history.length > 1" class="spark" aria-hidden="true"><Sparkline :values="history" :height="24" /></span>
      </template>
      <template #actions>
        <button v-if="isQueue" type="button" class="btn btn-danger" @click="purgeOpen = true"><Icon name="trash" :size="16" />Clear queue</button>
        <button type="button" class="btn btn-primary" @click="sendOpen = true"><Icon name="send" :size="16" />Send message</button>
      </template>
    </PageHeader>

    <StatStrip v-if="channel" :items="stats" />
    <p v-else-if="!snapshot.loading" class="card notice muted">
      This channel isn't in the latest snapshot yet. New channels show up within a few seconds of their first message; you can still send, receive and
      subscribe below.
    </p>

    <section class="card panel">
      <div class="tabs" role="tablist" :aria-label="`${name} tools`">
        <button
          v-for="(tab, index) in tabs"
          :id="`tab-${tab.id}`"
          :key="tab.id"
          type="button"
          role="tab"
          class="tab-button"
          :class="{ active: activeTab === tab.id }"
          :aria-selected="activeTab === tab.id"
          :aria-controls="`panel-${tab.id}`"
          :tabindex="activeTab === tab.id ? 0 : -1"
          @click="selectTab(tab.id)"
          @keydown="onTabKey($event, index)"
        >
          {{ tab.label }}
        </button>
      </div>
      <div :id="`panel-${activeTab}`" role="tabpanel" :aria-labelledby="`tab-${activeTab}`" class="tab-panel">
        <!-- KeepAlive so a running subscription or stream survives switching tabs. -->
        <KeepAlive>
          <QueueReceiveTab v-if="activeTab === 'peek'" key="peek" :channel="name" mode="peek" />
          <QueueReceiveTab v-else-if="activeTab === 'pull'" key="pull" :channel="name" mode="pull" />
          <QueueStreamTab v-else-if="activeTab === 'stream'" :channel="name" />
          <SubscribeTab v-else-if="activeTab === 'subscribe'" :channel="name" :type="typeKey" />
          <RespondTab v-else-if="activeTab === 'respond'" :channel="name" :type="typeKey" />
          <WatchTab v-else-if="activeTab === 'watch'" :channel="name" :kind="type.monitorKind" />
          <ClientsTab v-else :clients="channel?.clients ?? []" />
        </KeepAlive>
      </div>
    </section>

    <SendMessageDialog :open="sendOpen" :type="typeKey" :channel="name" @close="sendOpen = false" />
    <ConfirmDialog :open="purgeOpen" :title="`Clear ${name}?`" confirm-label="Clear queue" :busy="purging" @close="purgeOpen = false" @confirm="purge">
      <p class="confirm-text">
        This permanently deletes every message waiting in <strong>{{ name }}</strong
        ><template v-if="channel"> ({{ formatCompact(channel.outgoing?.waiting) }} right now)</template>. It can't be undone.
      </p>
    </ConfirmDialog>
  </div>
</template>

<style scoped>
.spark {
  width: 120px;
}
.notice {
  margin: 0 0 18px;
  padding: 14px 18px;
  font-size: 0.875rem;
}
.panel {
  padding: 6px 20px 20px;
}
.tabs {
  display: flex;
  gap: 4px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  margin: 0 -20px 18px;
  padding: 0 14px;
}
.tab-button {
  position: relative;
  padding: 12px 12px;
  border: none;
  background: none;
  color: var(--text-secondary);
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
}
.tab-button:hover {
  color: var(--text-primary);
}
.tab-button.active {
  color: var(--text-primary);
}
.tab-button.active::after {
  content: '';
  position: absolute;
  left: 8px;
  right: 8px;
  bottom: -1px;
  height: 2px;
  border-radius: 2px;
  background: var(--accent);
}
.confirm-text {
  margin: 0;
}
</style>
