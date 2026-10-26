<script setup>
import { computed } from 'vue'

const props = defineProps({
  value: { type: Number, default: 0 }, // percent, 0-100
  label: { type: String, required: true },
  detail: { type: String, default: '' },
})

const size = 112
const stroke = 10
const radius = (size - stroke) / 2
const circumference = 2 * Math.PI * radius

const clamped = computed(() => Math.min(100, Math.max(0, props.value || 0)))
const dash = computed(() => `${(clamped.value / 100) * circumference} ${circumference}`)
// Severity steps: accent while healthy, then warning, then critical.
const color = computed(() => {
  if (clamped.value >= 90) return 'var(--critical)'
  if (clamped.value >= 75) return 'var(--warning)'
  return 'var(--accent)'
})
const display = computed(() => (clamped.value < 10 ? clamped.value.toFixed(1) : Math.round(clamped.value)))
</script>

<template>
  <figure class="gauge" role="meter" :aria-valuenow="clamped" aria-valuemin="0" aria-valuemax="100" :aria-label="`${label} ${display}%`">
    <svg :width="size" :height="size" :viewBox="`0 0 ${size} ${size}`" aria-hidden="true">
      <circle :cx="size / 2" :cy="size / 2" :r="radius" fill="none" :stroke="color" stroke-opacity="0.18" :stroke-width="stroke" />
      <circle
        :cx="size / 2"
        :cy="size / 2"
        :r="radius"
        fill="none"
        :stroke="color"
        :stroke-width="stroke"
        stroke-linecap="round"
        :stroke-dasharray="dash"
        :transform="`rotate(-90 ${size / 2} ${size / 2})`"
      />
      <text :x="size / 2" :y="size / 2" text-anchor="middle" dominant-baseline="central" class="pct">{{ display }}%</text>
    </svg>
    <figcaption>
      <span class="name">{{ label }}</span>
      <span class="detail muted">{{ detail }}</span>
    </figcaption>
  </figure>
</template>

<style scoped>
.gauge {
  margin: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.pct {
  fill: var(--text-primary);
  font-size: 20px;
  font-weight: 600;
}
figcaption {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
}
.name {
  font-weight: 500;
}
.detail {
  font-size: 0.8rem;
}
</style>
