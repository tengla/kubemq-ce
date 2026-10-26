<script setup>
import { onBeforeUnmount, ref } from 'vue'
import LiveList from '../LiveList.vue'
import MessageView from '../MessageView.vue'
import { socketUrl } from '../../api'
import { useLiveList } from '../../liveList'

const props = defineProps({
  channel: { type: String, required: true },
  kind: { type: String, required: true }, // monitor kind: queue | events | events_store | commands | queries
})

const state = ref('idle')
const status = ref('')
const list = useLiveList()
let socket = null

// Requests carry a timeout and responses an executed flag; label them so a
// request and its reply are easy to tell apart.
function role(message) {
  if ('executed' in message) return 'Response'
  if ('timeout' in message) return 'Request'
  return ''
}

function start() {
  status.value = ''
  state.value = 'connecting'
  socket = new WebSocket(socketUrl('/api/monitor', { channel: props.channel, kind: props.kind }))
  // The monitor only forwards traffic after the client sends "start".
  socket.onopen = () => {
    socket.send('start')
    state.value = 'live'
  }
  socket.onmessage = (event) => {
    const text = String(event.data)
    try {
      const message = JSON.parse(text)
      if (message && typeof message === 'object') {
        list.add(message, text)
        return
      }
    } catch {
      // Plain-text frames are status lines such as "<host> Broker connected."
    }
    status.value = text
  }
  socket.onclose = () => {
    if (state.value === 'connecting') status.value = 'Could not connect to the monitor.'
    socket = null
    state.value = 'idle'
  }
}

function stop() {
  socket?.close()
}

onBeforeUnmount(stop)
</script>

<template>
  <div class="tab">
    <div class="controls">
      <button v-if="state === 'idle'" type="button" class="btn btn-primary" @click="start">Start watching</button>
      <button v-else type="button" class="btn" @click="stop">Stop</button>
      <p class="muted note">Shows a copy of each message passing through this channel without consuming it.</p>
    </div>
    <p v-if="status" class="muted status">{{ status }}</p>
    <LiveList :list="list" :live="state === 'live'" empty-text="Start watching to see traffic on this channel.">
      <template #default="{ entry }">
        <MessageView :message="entry.item" :details="[{ label: 'Sequence', value: entry.item.sequence }]">
          <template v-if="role(entry.item)" #status>
            <span class="badge">{{ role(entry.item) }}</span>
          </template>
        </MessageView>
      </template>
    </LiveList>
  </div>
</template>

<style scoped>
.tab {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px 16px;
}
.note,
.status {
  margin: 0;
  font-size: 0.85rem;
}
</style>
