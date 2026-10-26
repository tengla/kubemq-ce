import { reactive } from 'vue'

export const toasts = reactive([])
let nextId = 0

export function notify(message, kind = 'success') {
  const id = ++nextId
  toasts.push({ id, message, kind })
  setTimeout(() => dismiss(id), kind === 'error' ? 8000 : 4000)
}

export function dismiss(id) {
  const index = toasts.findIndex((t) => t.id === id)
  if (index !== -1) toasts.splice(index, 1)
}
