<script setup>
import { computed, onBeforeUnmount, ref } from 'vue'
import LiveList from '../LiveList.vue'
import { clientId, formatBody, socketUrl } from '../../api'
import { useLiveList } from '../../liveList'

const props = defineProps({
  channel: { type: String, required: true },
  type: { type: String, required: true }, // events | events_store
})

const isStore = computed(() => props.type === 'events_store')
const START_OPTIONS = [
  { value: '1', label: 'New messages only' },
  { value: '2', label: 'From the first message' },
  { value: '3', label: 'From the last message' },
  { value: '4', label: 'From a sequence number', input: 'number', placeholder: 'e.g. 100' },
  { value: '5', label: 'From a point in time', input: 'datetime-local' },
  { value: '6', label: 'From a number of seconds ago', input: 'number', placeholder: 'e.g. 3600' },
]

const group = ref('')
const startType = ref('1')
const startValue = ref('')
const startOption = computed(() => START_OPTIONS.find((o) => o.value === startType.value))
const state = ref('idle')
const error = ref('')
const list = useLiveList()
let socket = null

function start() {
  error.value = ''
  const option = startOption.value
  if (isStore.value && option.input && startValue.value === '') {
    error.value = 'Enter a start value.'
    return
  }
  const value = option.input === 'datetime-local' && startValue.value ? new Date(startValue.value).toISOString() : startValue.value
  state.value = 'connecting'
  socket = new WebSocket(
    socketUrl('/api/subscribe/pubsub', {
      subscribe_type: props.type,
      channel: props.channel,
      group: group.value.trim(),
      client_id: clientId('dashboard'),
      ...(isStore.value ? { events_store_type_data: startType.value, events_store_type_value: option.input ? value : '' } : {}),
    }),
  )
  socket.onopen = () => (state.value = 'live')
  socket.onmessage = (event) => {
    let message
    try {
      message = JSON.parse(event.data)
    } catch {
      error.value = String(event.data)
      return
    }
    list.add(message, `${message.messageId} ${message.metadata} ${formatBody(message.body)}`)
  }
  socket.onclose = () => {
    if (state.value === 'connecting') error.value = 'Could not subscribe. Check the start value and that the broker is ready.'
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
    <form class="controls" @submit.prevent="state === 'idle' ? start() : stop()">
      <label class="field">
        <span>Group <span class="hint">optional, load-balances with other subscribers</span></span>
        <input v-model="group" class="input" :disabled="state !== 'idle'" />
      </label>
      <template v-if="isStore">
        <label class="field">
          <span>Start</span>
          <select v-model="startType" class="select" :disabled="state !== 'idle'">
            <option v-for="o in START_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </label>
        <label v-if="startOption.input" class="field">
          <span>{{ startOption.input === 'datetime-local' ? 'Time' : startType === '4' ? 'Sequence' : 'Seconds ago' }}</span>
          <input v-model="startValue" class="input" :type="startOption.input" min="0" :placeholder="startOption.placeholder" :disabled="state !== 'idle'" />
        </label>
      </template>
      <button type="submit" class="btn" :class="{ 'btn-primary': state === 'idle' }">{{ state === 'idle' ? 'Subscribe' : 'Unsubscribe' }}</button>
    </form>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>
    <LiveList :list="list" :live="state === 'live'" empty-text="Subscribe to see messages as they arrive." :details="(m) => [{ label: 'Sequence', value: m.sequence }]" />
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
  align-items: flex-end;
  gap: 12px 14px;
}
.controls .field {
  flex: 1 1 200px;
  max-width: 320px;
}
</style>
