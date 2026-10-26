<script setup>
import { nextTick, ref, watch } from 'vue'
import Icon from './Icon.vue'
import { dismiss, toasts } from '../toast'

// An open <dialog> sits in the browser's top layer, above any z-index. Showing
// the toast stack as a popover (re-shown on each new toast) puts it above
// whatever dialog is open at that moment.
const stack = ref(null)
watch(
  () => toasts.length,
  async (count, before) => {
    await nextTick()
    const el = stack.value
    if (!el?.showPopover) return
    if (el.matches(':popover-open') && (count === 0 || count > before)) el.hidePopover()
    if (count > 0 && !el.matches(':popover-open')) el.showPopover()
  },
)
</script>

<template>
  <div ref="stack" class="toasts" popover="manual" role="status" aria-live="polite">
    <div v-for="t in toasts" :key="t.id" class="toast" :class="t.kind">
      <span class="icon"><Icon :name="t.kind === 'error' ? 'alert' : 'check'" :size="18" /></span>
      <span class="text">{{ t.message }}</span>
      <button type="button" class="dismiss" aria-label="Dismiss" @click="dismiss(t.id)"><Icon name="x" :size="16" /></button>
    </div>
  </div>
</template>

<style scoped>
.toasts {
  position: fixed;
  inset: auto 20px 20px auto;
  z-index: 50;
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: min(380px, calc(100vw - 32px));
  margin: 0;
  padding: 0;
  border: none;
  background: transparent;
  color: inherit;
  overflow: visible;
}
/* Without popover support the stack is shown as a normal fixed element. */
.toasts:not(:popover-open):empty {
  display: none;
}
.toast {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 12px 12px 12px 14px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface-2);
  color: var(--text-primary);
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.25);
  font-size: 0.875rem;
}
.icon {
  display: grid;
  margin-top: 1px;
}
.toast.success .icon {
  color: var(--good-text);
}
.toast.error .icon {
  color: var(--critical);
}
.text {
  flex: 1;
  overflow-wrap: anywhere;
}
.dismiss {
  display: grid;
  place-items: center;
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 2px;
}
</style>
