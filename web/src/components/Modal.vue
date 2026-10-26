<script setup>
import { nextTick, onMounted, ref, watch } from 'vue'
import Icon from './Icon.vue'

const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, required: true },
  wide: { type: Boolean, default: false },
})
const emit = defineEmits(['close'])
const dialog = ref(null)

// A native <dialog> gives focus trapping, Escape handling and inert
// background content for free.
async function sync() {
  await nextTick()
  const el = dialog.value
  if (!el) return
  if (props.open && !el.open) el.showModal()
  else if (!props.open && el.open) el.close()
}
watch(() => props.open, sync)
onMounted(sync)

function onCancel(event) {
  event.preventDefault()
  emit('close')
}
function onBackdrop(event) {
  if (event.target === dialog.value) emit('close')
}
</script>

<template>
  <dialog ref="dialog" class="modal" :class="{ wide }" :aria-label="title" @cancel="onCancel" @click="onBackdrop">
    <div class="box">
      <header class="head">
        <h2 class="card-title">{{ title }}</h2>
        <button type="button" class="close" aria-label="Close" @click="emit('close')"><Icon name="x" /></button>
      </header>
      <div class="body"><slot /></div>
      <footer v-if="$slots.footer" class="foot"><slot name="footer" /></footer>
    </div>
  </dialog>
</template>

<style scoped>
.modal {
  width: min(560px, calc(100vw - 32px));
  max-height: calc(100vh - 48px);
  padding: 0;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface-1);
  color: var(--text-primary);
  box-shadow: 0 24px 64px rgba(0, 0, 0, 0.35);
}
.modal.wide {
  width: min(760px, calc(100vw - 32px));
}
.modal::backdrop {
  background: rgba(5, 10, 20, 0.55);
}
.box {
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 50px);
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 18px 20px 12px;
}
.close {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}
.close:hover {
  background: var(--surface-2);
  color: var(--text-primary);
}
.body {
  padding: 4px 20px 18px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.foot {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 10px;
  padding: 14px 20px 18px;
  border-top: 1px solid var(--border);
}
</style>
