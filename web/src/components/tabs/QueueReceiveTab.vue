<script setup>
import { computed, ref } from 'vue'
import MessageView from '../MessageView.vue'
import { apiRequest, formatTime } from '../../api'
import { pollSnapshot } from '../../snapshot'
import { notify } from '../../toast'

const props = defineProps({
  channel: { type: String, required: true },
  // peek leaves messages in the queue; pull removes them.
  mode: { type: String, required: true },
})

const isPeek = computed(() => props.mode === 'peek')
const count = ref(10)
const loading = ref(false)
const error = ref('')
const messages = ref(null)

async function run() {
  loading.value = true
  error.value = ''
  try {
    const result = await apiRequest('receive_queue_messages', { channel: props.channel, isPeek: isPeek.value, count: Math.max(1, Math.round(Number(count.value) || 1)) })
    messages.value = result ?? []
    if (!isPeek.value && messages.value.length) {
      notify(`Pulled ${messages.value.length} ${messages.value.length === 1 ? 'message' : 'messages'} from ${props.channel}`)
      pollSnapshot()
    }
  } catch (err) {
    error.value = err.message
  } finally {
    loading.value = false
  }
}

function details(m) {
  return [
    { label: 'Sequence', value: m.sequence },
    { label: 'Sent by', value: m.clientId },
    { label: 'Received', value: m.receivedCount ? `${m.receivedCount}×` : '' },
    { label: 'Re-routed from', value: m.reRoutedFrom },
    { label: 'Expires', value: formatTime(m.expirationAt) },
    { label: 'Delayed to', value: formatTime(m.delayedTo) },
  ]
}
</script>

<template>
  <div class="tab">
    <form class="controls" @submit.prevent="run">
      <label class="field count">
        <span>Messages</span>
        <input v-model="count" class="input" type="number" min="1" max="1024" />
      </label>
      <button type="submit" class="btn" :class="isPeek ? 'btn-primary' : 'btn-danger-solid'" :disabled="loading">
        {{ loading ? 'Working…' : isPeek ? 'Peek' : 'Pull' }}
      </button>
      <p class="muted note">
        {{ isPeek ? 'Peeking shows waiting messages without removing them.' : 'Pulling removes the messages from the queue.' }}
      </p>
    </form>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>
    <div v-if="messages && !messages.length" class="empty-state"><p>No messages waiting.</p></div>
    <div v-else-if="messages" class="list">
      <p class="muted small">{{ messages.length }} {{ messages.length === 1 ? 'message' : 'messages' }}</p>
      <MessageView v-for="m in messages" :key="m.messageId + m.sequence" :message="m" :details="details(m)" />
    </div>
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
.count {
  width: 120px;
}
.note {
  flex: 1 1 220px;
  min-width: 0;
  margin: 0 0 9px;
  font-size: 0.85rem;
}
.list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.small {
  margin: 0;
  font-size: 0.82rem;
}
</style>
