import { onBeforeUnmount, onMounted, ref } from 'vue'

// Tracks an element's rendered width so SVG charts can draw in real pixels.
export function useWidth(elementRef, fallback = 300) {
  const width = ref(fallback)
  let observer = null
  onMounted(() => {
    observer = new ResizeObserver(([entry]) => {
      width.value = Math.max(1, Math.round(entry.contentRect.width))
    })
    observer.observe(elementRef.value)
  })
  onBeforeUnmount(() => observer?.disconnect())
  return width
}
