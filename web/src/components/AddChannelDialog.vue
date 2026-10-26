<script setup>
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import Modal from './Modal.vue'
import { TYPES_BY_KEY, channelPath } from '../channelTypes'
import { apiRequest } from '../api'
import { pollSnapshot } from '../snapshot'
import { notify } from '../toast'

const props = defineProps({
  open: { type: Boolean, default: false },
  type: { type: String, required: true },
})
const emit = defineEmits(['close'])
const router = useRouter()

const type = computed(() => TYPES_BY_KEY[props.type])
const name = ref('')
const error = ref('')
const saving = ref(false)

watch(
  () => props.open,
  (open) => {
    if (open) {
      name.value = ''
      error.value = ''
    }
  },
)

async function create() {
  const channel = name.value.trim()
  if (!channel) {
    error.value = 'Enter a channel name.'
    return
  }
  saving.value = true
  error.value = ''
  try {
    await apiRequest('create_channel', { type: props.type, name: channel })
    notify(`Created ${type.value.noun} ${channel}`)
    pollSnapshot()
    emit('close')
    router.push(channelPath(props.type, channel))
  } catch (err) {
    error.value = err.message
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Modal :open="open" :title="`New ${type.noun}`" @close="emit('close')">
    <form id="add-channel-form" @submit.prevent="create">
      <label class="field">
        <span>Name</span>
        <input v-model="name" class="input" required autofocus placeholder="e.g. orders.created" />
      </label>
    </form>
    <p class="muted note">Channels are also created automatically the first time a message is sent to them.</p>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">Cancel</button>
      <button type="submit" form="add-channel-form" class="btn btn-primary" :disabled="saving">{{ saving ? 'Creating…' : 'Create' }}</button>
    </template>
  </Modal>
</template>

<style scoped>
.note {
  margin: 0;
  font-size: 0.85rem;
}
</style>
