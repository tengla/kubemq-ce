<script setup>
import Modal from './Modal.vue'

defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, required: true },
  confirmLabel: { type: String, default: 'Confirm' },
  busy: { type: Boolean, default: false },
})
const emit = defineEmits(['close', 'confirm'])
</script>

<template>
  <Modal :open="open" :title="title" @close="emit('close')">
    <slot />
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">Cancel</button>
      <button type="button" class="btn btn-danger-solid" :disabled="busy" @click="emit('confirm')">{{ busy ? 'Working…' : confirmLabel }}</button>
    </template>
  </Modal>
</template>
