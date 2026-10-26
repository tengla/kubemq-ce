import { computed, ref } from 'vue'

// Newest-first list of streamed items, capped so a busy channel can't grow
// the page without bound. `text` is what the filter box matches against.
export function useLiveList(limit = 500) {
  const items = ref([])
  const received = ref(0)
  const filter = ref('')
  let seq = 0

  function add(item, text) {
    received.value += 1
    items.value.unshift({ key: ++seq, item, text: text.toLowerCase() })
    if (items.value.length > limit) items.value.length = limit
  }

  function clear() {
    items.value = []
    received.value = 0
  }

  const visible = computed(() => {
    const query = filter.value.trim().toLowerCase()
    return query ? items.value.filter((entry) => entry.text.includes(query)) : items.value
  })

  return { items, visible, received, filter, add, clear, limit }
}
