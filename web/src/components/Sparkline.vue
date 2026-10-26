<script setup>
import { computed, ref } from 'vue'
import { useWidth } from '../useWidth'

const props = defineProps({
  values: { type: Array, default: () => [] },
  height: { type: Number, default: 36 },
})

const root = ref(null)
const width = useWidth(root, 120)
const pad = 5

const points = computed(() => {
  const values = props.values
  if (values.length < 2) return []
  const max = Math.max(...values)
  const min = Math.min(...values)
  const span = max - min || 1
  const w = width.value - pad * 2
  const h = props.height - pad * 2
  return values.map((v, i) => ({
    x: pad + (i / (values.length - 1)) * w,
    // A flat series sits on the baseline rather than floating mid-box.
    y: max === min ? props.height - pad : pad + h - ((v - min) / span) * h,
  }))
})

const line = computed(() => points.value.map((p) => `${p.x},${p.y}`).join(' '))
const last = computed(() => points.value.at(-1))
</script>

<template>
  <div ref="root" class="spark">
    <svg v-if="points.length" :width="width" :height="height" aria-hidden="true">
      <polyline :points="line" fill="none" stroke="var(--spark)" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
      <circle :cx="last.x" :cy="last.y" r="4" fill="var(--accent)" stroke="var(--surface-1)" stroke-width="2" />
    </svg>
  </div>
</template>

<style scoped>
.spark {
  width: 100%;
  height: v-bind('`${height}px`');
}
</style>
