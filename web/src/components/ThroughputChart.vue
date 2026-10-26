<script setup>
import { computed, ref } from 'vue'
import { useWidth } from '../useWidth'
import { formatRate } from '../snapshot'

const props = defineProps({
  samples: { type: Array, required: true },
  series: { type: Array, required: true },
})

const root = ref(null)
const width = useWidth(root, 800)
const height = 260
const margin = { top: 12, right: 16, bottom: 28, left: 44 }
const plotW = computed(() => Math.max(1, width.value - margin.left - margin.right))
const plotH = height - margin.top - margin.bottom

const ready = computed(() => props.samples.length >= 2)

const Y_TICKS = 4

// Rounds a tick interval up to 1, 2 or 5 times a power of ten, so every
// gridline lands on a round number.
function niceStep(value) {
  if (value <= 0) return 0.25
  const magnitude = 10 ** Math.floor(Math.log10(value))
  for (const step of [1, 2, 5, 10]) {
    if (step * magnitude >= value) return step * magnitude
  }
  return 10 * magnitude
}

const yStep = computed(() => {
  let max = 0
  for (const sample of props.samples) {
    for (const s of props.series) max = Math.max(max, sample.rates[s.key] ?? 0)
  }
  return niceStep(max / Y_TICKS)
})
const yMax = computed(() => yStep.value * Y_TICKS)

const t0 = computed(() => props.samples[0]?.t ?? 0)
const t1 = computed(() => props.samples.at(-1)?.t ?? 1)
const x = (t) => margin.left + ((t - t0.value) / Math.max(1, t1.value - t0.value)) * plotW.value
const y = (v) => margin.top + plotH - (v / yMax.value) * plotH

const yTicks = computed(() => Array.from({ length: Y_TICKS + 1 }, (_, i) => yStep.value * i))

const timeFormat = new Intl.DateTimeFormat([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
const xTicks = computed(() => {
  if (!ready.value) return []
  const count = Math.max(2, Math.min(6, Math.floor(plotW.value / 110)))
  return Array.from({ length: count }, (_, i) => {
    const t = t0.value + ((t1.value - t0.value) * i) / (count - 1)
    return { x: x(t), label: timeFormat.format(t), anchor: i === 0 ? 'start' : i === count - 1 ? 'end' : 'middle' }
  })
})

const paths = computed(() =>
  props.series.map((s) => ({
    ...s,
    d: props.samples.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)},${y(p.rates[s.key] ?? 0).toFixed(1)}`).join(''),
  })),
)

// Hover / keyboard readout: one crosshair, every series listed.
const hoverIndex = ref(null)
const hovered = computed(() => (hoverIndex.value == null ? null : props.samples[hoverIndex.value]))

function nearestIndex(px) {
  let best = 0
  let bestDist = Infinity
  props.samples.forEach((p, i) => {
    const dist = Math.abs(x(p.t) - px)
    if (dist < bestDist) {
      best = i
      bestDist = dist
    }
  })
  return best
}

function onPointerMove(event) {
  const rect = event.currentTarget.getBoundingClientRect()
  hoverIndex.value = nearestIndex(event.clientX - rect.left)
}

function onKeydown(event) {
  const last = props.samples.length - 1
  if (event.key === 'ArrowLeft') hoverIndex.value = Math.max(0, (hoverIndex.value ?? last) - 1)
  else if (event.key === 'ArrowRight') hoverIndex.value = Math.min(last, (hoverIndex.value ?? last) + 1)
  else if (event.key === 'Escape') hoverIndex.value = null
  else return
  event.preventDefault()
}

const tooltipStyle = computed(() => {
  if (!hovered.value) return {}
  const px = x(hovered.value.t)
  const flip = px > width.value - 200
  return flip ? { right: `${width.value - px + 12}px`, top: `${margin.top}px` } : { left: `${px + 12}px`, top: `${margin.top}px` }
})

const rows = computed(() =>
  hovered.value ? [...props.series].map((s) => ({ ...s, value: hovered.value.rates[s.key] ?? 0 })).sort((a, b) => b.value - a.value) : [],
)
</script>

<template>
  <div class="chart">
    <ul class="legend" aria-label="Series">
      <li v-for="s in series" :key="s.key"><span class="key" :style="{ background: s.color }" />{{ s.label }}</li>
    </ul>
    <div ref="root" class="plot">
      <p v-if="!ready" class="empty muted">Collecting data. Throughput appears after two server snapshots (about 10 seconds).</p>
      <svg
        v-else
        :width="width"
        :height="height"
        tabindex="0"
        role="img"
        aria-label="Incoming messages per second by channel type. Use the left and right arrow keys to read values."
        @pointermove="onPointerMove"
        @pointerleave="hoverIndex = null"
        @keydown="onKeydown"
        @focus="hoverIndex = samples.length - 1"
        @blur="hoverIndex = null"
      >
        <g class="grid">
          <line v-for="tick in yTicks" :key="tick" :x1="margin.left" :x2="width - margin.right" :y1="y(tick)" :y2="y(tick)" />
        </g>
        <g class="axis">
          <text v-for="tick in yTicks" :key="`y${tick}`" :x="margin.left - 8" :y="y(tick)" text-anchor="end" dominant-baseline="central">
            {{ formatRate(tick) }}
          </text>
          <text v-for="tick in xTicks" :key="`x${tick.x}`" :x="tick.x" :y="height - 6" :text-anchor="tick.anchor">{{ tick.label }}</text>
        </g>
        <path v-for="p in paths" :key="p.key" :d="p.d" fill="none" :stroke="p.color" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
        <g v-if="hovered">
          <line class="crosshair" :x1="x(hovered.t)" :x2="x(hovered.t)" :y1="margin.top" :y2="margin.top + plotH" />
          <circle
            v-for="s in series"
            :key="`dot${s.key}`"
            :cx="x(hovered.t)"
            :cy="y(hovered.rates[s.key] ?? 0)"
            r="4"
            :fill="s.color"
            stroke="var(--surface-1)"
            stroke-width="2"
          />
        </g>
      </svg>
      <div v-if="hovered" class="tooltip" :style="tooltipStyle" role="status">
        <p class="time">{{ timeFormat.format(hovered.t) }}</p>
        <p v-for="r in rows" :key="r.key" class="row">
          <span class="key" :style="{ background: r.color }" />
          <strong class="num">{{ formatRate(r.value) }}</strong>
          <span class="muted">{{ r.label }}</span>
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 18px;
  margin: 0 0 12px;
  padding: 0;
  list-style: none;
  font-size: 0.85rem;
  color: var(--text-secondary);
}
.legend li,
.row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.key {
  width: 14px;
  height: 2px;
  border-radius: 1px;
  flex: none;
}
.plot {
  position: relative;
  width: 100%;
  min-height: 260px;
}
svg {
  display: block;
  touch-action: none;
}
.grid line {
  stroke: var(--grid);
  stroke-width: 1;
}
.axis text {
  fill: var(--text-muted);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.crosshair {
  stroke: var(--axis);
  stroke-width: 1;
}
.empty {
  margin: 0;
  padding: 100px 0;
  text-align: center;
}
.tooltip {
  position: absolute;
  pointer-events: none;
  min-width: 150px;
  padding: 10px 12px;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.18);
  font-size: 0.82rem;
}
.tooltip p {
  margin: 0;
}
.time {
  color: var(--text-muted);
  margin-bottom: 6px !important;
}
.row {
  padding: 2px 0;
}
.row strong {
  min-width: 36px;
  color: var(--text-primary);
}
</style>
