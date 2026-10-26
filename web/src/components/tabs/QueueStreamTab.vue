<script setup>
import { onBeforeUnmount, ref } from 'vue'
import MessageView from '../MessageView.vue'
import { socketUrl } from '../../api'
import { pollSnapshot } from '../../snapshot'

const props = defineProps({
  channel: { type: String, required: true },
})

// idle -> waiting (for a message) -> holding (a message to ack/reject) -> waiting ...
const state = ref('idle')
const current = ref(null)
const handled = ref([])
const error = ref('')
const autoNext = ref(true)
let socket = null

function send(frame) {
  socket?.send(JSON.stringify({ channel: props.channel, ...frame }))
}

function requestNext() {
  current.value = null
  state.value = 'waiting'
  // The server rejects polls without positive visibility and wait values.
  send({ requestType: 'stream_queue_messages', visibilitySeconds: 60, waitSeconds: 3600 })
}

function onFrame(event) {
  let frame
  try {
    frame = JSON.parse(event.data)
  } catch {
    error.value = String(event.data)
    return
  }
  if (frame.isError) {
    error.value = frame.error || 'Stream error'
    state.value = current.value ? 'holding' : 'ready'
    return
  }
  error.value = ''
  if (frame.requestType === 'stream_queue_messages' && frame.message) {
    current.value = frame.message
    state.value = 'holding'
  } else if (frame.requestType === 'ack_queue_messages' || frame.requestType === 'reject_queue_messages') {
    if (current.value) {
      handled.value.unshift({ message: current.value, outcome: frame.requestType === 'ack_queue_messages' ? 'Acknowledged' : 'Rejected' })
      if (handled.value.length > 50) handled.value.length = 50
    }
    current.value = null
    pollSnapshot()
    if (autoNext.value) requestNext()
    else state.value = 'ready'
  }
}

function start() {
  error.value = ''
  state.value = 'connecting'
  socket = new WebSocket(socketUrl('/api/stream_queue_messages'))
  socket.onopen = requestNext
  socket.onmessage = onFrame
  socket.onclose = () => {
    if (state.value === 'connecting') error.value = 'Could not open the stream.'
    socket = null
    current.value = null
    state.value = 'idle'
  }
}

function stop() {
  socket?.close()
}

function settle(ack) {
  if (!current.value) return
  state.value = 'settling'
  send({ requestType: ack ? 'ack_queue_messages' : 'reject_queue_messages', refSequence: current.value.sequence })
}

onBeforeUnmount(stop)
</script>

<template>
  <div class="tab">
    <div class="controls">
      <button v-if="state === 'idle'" type="button" class="btn btn-primary" @click="start">Start stream</button>
      <button v-else type="button" class="btn" @click="stop">Stop stream</button>
      <button v-if="state === 'ready'" type="button" class="btn" @click="requestNext">Get next message</button>
      <label class="checkbox"><input v-model="autoNext" type="checkbox" />Get the next message after ack/reject</label>
    </div>
    <p class="muted note">
      The stream takes one message at a time and holds it until you acknowledge or reject it. Rejected messages, and a held message when you stop the
      stream, go back to the queue.
    </p>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>

    <p v-if="state === 'connecting'" class="muted">Connecting…</p>
    <p v-else-if="state === 'waiting'" class="muted waiting"><span class="pulse" aria-hidden="true" />Waiting for a message…</p>

    <MessageView
      v-if="current"
      :message="current"
      :details="[
        { label: 'Sequence', value: current.sequence },
        { label: 'Sent by', value: current.clientId },
        { label: 'Received', value: current.receivedCount ? `${current.receivedCount}×` : '' },
      ]"
    >
      <div class="settle">
        <button type="button" class="btn btn-primary" :disabled="state !== 'holding'" @click="settle(true)">Acknowledge</button>
        <button type="button" class="btn btn-danger" :disabled="state !== 'holding'" @click="settle(false)">Reject</button>
      </div>
    </MessageView>

    <section v-if="handled.length" class="history">
      <h3 class="muted small">Handled in this session</h3>
      <MessageView v-for="(h, i) in handled" :key="i + h.message.messageId" :message="h.message">
        <template #status>
          <span class="badge">{{ h.outcome }}</span>
        </template>
      </MessageView>
    </section>
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
.note {
  margin: 0;
  font-size: 0.85rem;
  max-width: 70ch;
}
.waiting {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0;
}
.pulse {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent);
  animation: pulse 1.4s ease-in-out infinite;
}
@keyframes pulse {
  50% {
    opacity: 0.25;
  }
}
@media (prefers-reduced-motion: reduce) {
  .pulse {
    animation: none;
  }
}
.settle {
  display: flex;
  gap: 10px;
}
.history {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.small {
  margin: 6px 0 0;
  font-size: 0.82rem;
  font-weight: 500;
}
</style>
