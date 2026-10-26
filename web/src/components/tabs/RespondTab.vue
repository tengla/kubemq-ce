<script setup>
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import LiveList from '../LiveList.vue'
import MessageView from '../MessageView.vue'
import { apiRequest, formatBody, parseBodyInput, socketUrl } from '../../api'
import { useLiveList } from '../../liveList'

const props = defineProps({
  channel: { type: String, required: true },
  type: { type: String, required: true }, // commands | queries
})

const isQuery = computed(() => props.type === 'queries')
const group = ref('')
const autoReply = ref(false)
const autoBody = ref('OK')
const state = ref('idle')
const error = ref('')
const list = useLiveList(200)
// Per-request reply drafts and outcomes, keyed by requestId.
const replies = reactive({})
let socket = null

function draft(id) {
  replies[id] ??= { body: '', metadata: '', error: '', outcome: '', sending: false }
  return replies[id]
}

async function reply(request, executed) {
  const r = draft(request.requestId)
  if (!executed && !r.error.trim()) {
    r.outcome = 'Enter an error message to fail the request.'
    return
  }
  r.sending = true
  try {
    await apiRequest('send_cqrs_message_response', {
      requestId: request.requestId,
      replyChannel: request.replyChannel,
      body: executed ? parseBodyInput(r.body) : '',
      metadata: r.metadata || undefined,
      executed,
      error: executed ? undefined : r.error.trim(),
    })
    r.outcome = executed ? 'Replied: executed' : 'Replied: failed'
  } catch (err) {
    r.outcome = `Reply failed: ${err.message}`
  } finally {
    r.sending = false
  }
}

function start() {
  error.value = ''
  state.value = 'connecting'
  socket = new WebSocket(socketUrl('/api/subscribe/cqrs', { subscribe_type: props.type, channel: props.channel, group: group.value.trim() }))
  socket.onopen = () => (state.value = 'live')
  socket.onmessage = (event) => {
    let request
    try {
      request = JSON.parse(event.data)
    } catch {
      error.value = String(event.data)
      return
    }
    const r = draft(request.requestId)
    list.add(request, `${request.requestId} ${request.metadata} ${formatBody(request.body)}`)
    if (autoReply.value) {
      r.body = autoBody.value
      reply(request, true)
    }
  }
  socket.onclose = () => {
    if (state.value === 'connecting') error.value = 'Could not subscribe. Check that the broker is ready.'
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
        <span>Group <span class="hint">optional, load-balances with other responders</span></span>
        <input v-model="group" class="input" :disabled="state !== 'idle'" />
      </label>
      <button type="submit" class="btn" :class="{ 'btn-primary': state === 'idle' }">{{ state === 'idle' ? 'Start responding' : 'Stop' }}</button>
    </form>
    <div class="auto">
      <label class="checkbox"><input v-model="autoReply" type="checkbox" />Reply automatically as executed</label>
      <label v-if="autoReply && isQuery" class="field auto-body">
        <span>Response body</span>
        <input v-model="autoBody" class="input" />
      </label>
    </div>
    <p class="muted note">
      Senders wait only until their timeout, so reply promptly. While this page is responding it competes with other responders on the channel unless you
      use a different group.
    </p>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>

    <LiveList :list="list" :live="state === 'live'" :empty-text="`Start responding to receive ${isQuery ? 'queries' : 'commands'} sent to this channel.`">
      <template #default="{ entry }">
        <MessageView :message="entry.item">
          <template v-if="replies[entry.item.requestId]?.outcome" #status>
            <span class="badge">{{ replies[entry.item.requestId].outcome }}</span>
          </template>
          <form v-if="!replies[entry.item.requestId].outcome.startsWith('Replied')" class="reply" @submit.prevent="reply(entry.item, true)">
            <template v-if="isQuery">
              <label class="field">
                <span>Response body</span>
                <textarea v-model="replies[entry.item.requestId].body" class="textarea small" spellcheck="false" />
              </label>
              <label class="field">
                <span>Metadata</span>
                <input v-model="replies[entry.item.requestId].metadata" class="input" />
              </label>
            </template>
            <label class="field">
              <span>Error message <span class="hint">to fail the request</span></span>
              <input v-model="replies[entry.item.requestId].error" class="input" />
            </label>
            <div class="buttons">
              <button type="submit" class="btn btn-primary btn-small" :disabled="replies[entry.item.requestId].sending">Reply executed</button>
              <button type="button" class="btn btn-danger btn-small" :disabled="replies[entry.item.requestId].sending" @click="reply(entry.item, false)">Reply failed</button>
            </div>
          </form>
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
  display: grid;
  grid-template-columns: minmax(200px, 360px) auto;
  justify-content: start;
  align-items: end;
  gap: 12px 14px;
}
.auto {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 12px 18px;
}
.auto-body {
  width: min(280px, 100%);
}
.note {
  margin: 0;
  font-size: 0.85rem;
  max-width: 75ch;
}
.reply {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 10px 14px;
  padding-top: 10px;
  border-top: 1px solid var(--border);
}
.textarea.small {
  min-height: 70px;
}
.buttons {
  display: flex;
  align-items: flex-end;
  gap: 10px;
}
@media (max-width: 640px) {
  .controls {
    grid-template-columns: 1fr;
  }
}
</style>
