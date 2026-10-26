<script setup>
import { computed, reactive, ref, watch } from 'vue'
import Modal from './Modal.vue'
import MessageView from './MessageView.vue'
import { TYPES_BY_KEY } from '../channelTypes'
import { apiRequest, clientId, normalizeTags, parseBodyInput } from '../api'
import { pollSnapshot } from '../snapshot'
import { notify } from '../toast'

const props = defineProps({
  open: { type: Boolean, default: false },
  type: { type: String, required: true },
  // When set, the channel is fixed (sending from a channel's own page).
  channel: { type: String, default: '' },
})
const emit = defineEmits(['close'])

const type = computed(() => TYPES_BY_KEY[props.type])
const isQueue = computed(() => props.type === 'queues')
const isRequest = computed(() => props.type === 'commands' || props.type === 'queries')

const form = reactive({
  channel: '',
  body: '',
  metadata: '',
  tags: '',
  expiration: '',
  delay: '',
  maxReceiveCount: '',
  deadLetterQueue: '',
  timeout: 10,
})
const sending = ref(false)
const error = ref('')
const result = ref(null)

watch(
  () => props.open,
  (open) => {
    if (!open) return
    form.channel = props.channel
    error.value = ''
    result.value = null
  },
  { immediate: true },
)

const toInt = (v) => (v === '' || v == null ? undefined : Math.max(0, Math.round(Number(v))))

async function send() {
  error.value = ''
  result.value = null
  if (!form.channel.trim()) {
    error.value = 'Enter a channel name.'
    return
  }
  let tags
  try {
    tags = normalizeTags(form.tags)
  } catch (err) {
    error.value = err.message
    return
  }
  const base = {
    channel: form.channel.trim(),
    body: parseBodyInput(form.body),
    metadata: form.metadata || undefined,
    tags: tags || undefined,
  }
  sending.value = true
  try {
    if (isQueue.value) {
      const res = await apiRequest('send_queue_message', {
        ...base,
        expirationAt: toInt(form.expiration),
        delayedTo: toInt(form.delay),
        maxReceiveCount: toInt(form.maxReceiveCount),
        maxReceiveQueue: form.deadLetterQueue.trim() || undefined,
      })
      result.value = { kind: 'queue', ...res }
      notify(`Message sent to ${base.channel}`)
    } else if (isRequest.value) {
      const res = await apiRequest('send_cqrs_message_request', {
        ...base,
        requestId: clientId('request'),
        isCommands: props.type === 'commands',
        timeout: Math.max(1, toInt(form.timeout) ?? 10),
      })
      result.value = { kind: 'response', ...res }
      const noun = props.type === 'commands' ? 'Command' : 'Query'
      notify(res.executed ? `${noun} executed` : `${noun} was not executed`, res.executed ? 'success' : 'error')
    } else {
      await apiRequest('send_pubsub_message', { ...base, isEvents: props.type === 'events' })
      result.value = { kind: 'sent' }
      notify(`Event sent to ${base.channel}`)
    }
    pollSnapshot()
  } catch (err) {
    error.value = err.message
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <Modal :open="open" :title="`Send to ${type.label.toLowerCase()}`" wide @close="emit('close')">
    <form id="send-form" class="form" @submit.prevent="send">
      <label class="field">
        <span>Channel</span>
        <input v-model="form.channel" class="input" :readonly="!!channel" required placeholder="e.g. orders.created" />
      </label>
      <label class="field">
        <span>Body <span class="hint">JSON objects and arrays are sent as JSON, anything else as text</span></span>
        <textarea v-model="form.body" class="textarea" spellcheck="false" placeholder='{"hello": "world"}' />
      </label>
      <div class="form-grid">
        <label class="field">
          <span>Metadata</span>
          <input v-model="form.metadata" class="input" />
        </label>
        <label class="field">
          <span>Tags <span class="hint">key=value, comma separated</span></span>
          <input v-model="form.tags" class="input" placeholder="env=dev, team=ops" />
        </label>
      </div>
      <div v-if="isQueue" class="form-grid">
        <label class="field">
          <span>Expires after (s)</span>
          <input v-model="form.expiration" class="input" type="number" min="0" placeholder="never" />
        </label>
        <label class="field">
          <span>Delay (s)</span>
          <input v-model="form.delay" class="input" type="number" min="0" placeholder="none" />
        </label>
        <label class="field">
          <span>Max receive count</span>
          <input v-model="form.maxReceiveCount" class="input" type="number" min="0" placeholder="unlimited" />
        </label>
        <label class="field">
          <span>Dead-letter queue</span>
          <input v-model="form.deadLetterQueue" class="input" placeholder="none" />
        </label>
      </div>
      <div v-if="isRequest" class="form-grid">
        <label class="field">
          <span>Timeout (s) <span class="hint">how long to wait for a responder</span></span>
          <input v-model="form.timeout" class="input" type="number" min="1" required />
        </label>
      </div>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>

      <div v-if="result?.kind === 'queue'" class="result">
        <p class="result-title">Sent</p>
        <dl class="kv">
          <div><dt>Message ID</dt><dd class="mono">{{ result.messageId }}</dd></div>
          <div v-if="result.sentAt"><dt>Sent at</dt><dd>{{ result.sentAt }}</dd></div>
          <div v-if="result.delayedTo"><dt>Delayed to</dt><dd>{{ result.delayedTo }}</dd></div>
          <div v-if="result.expiresAt"><dt>Expires at</dt><dd>{{ result.expiresAt }}</dd></div>
        </dl>
      </div>
      <div v-else-if="result?.kind === 'response'" class="result">
        <p class="result-title">Response · {{ result.executed ? 'executed' : 'not executed' }}</p>
        <p v-if="result.error" class="form-error">{{ result.error }}</p>
        <MessageView v-if="result.body != null || result.metadata" :message="result" />
      </div>
      <div v-else-if="result?.kind === 'sent'" class="result">
        <p class="result-title">Sent</p>
      </div>
    </form>
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">Close</button>
      <button type="submit" form="send-form" class="btn btn-primary" :disabled="sending">
        {{ sending ? (isRequest ? 'Waiting for response…' : 'Sending…') : result ? 'Send again' : 'Send' }}
      </button>
    </template>
  </Modal>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.result {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px 14px;
  border-radius: 12px;
  background: var(--accent-wash);
}
.result-title {
  margin: 0;
  font-weight: 600;
}
.kv {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 22px;
  margin: 0;
  font-size: 0.85rem;
}
.kv dt {
  color: var(--text-muted);
}
.kv dd {
  margin: 2px 0 0;
  overflow-wrap: anywhere;
}
</style>
