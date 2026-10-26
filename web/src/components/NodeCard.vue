<script setup>
import { computed } from 'vue'
import Icon from './Icon.vue'
import RingGauge from './RingGauge.vue'
import Sparkline from './Sparkline.vue'
import { formatRate } from '../snapshot'

const props = defineProps({
  host: { type: Object, default: null },
  samples: { type: Array, default: () => [] },
})

const running = computed(() => props.host?.status === 'Running')
const throughput = computed(() => props.samples.map((s) => s.totalRate))
const facts = computed(() => [
  { label: 'Uptime', value: props.host?.uptime ?? '-' },
  { label: 'CPU cores', value: props.host?.cpuCores ?? '-' },
  { label: 'OS threads', value: props.host?.osThreads ?? '-' },
  { label: 'Version', value: props.host?.version || '-' },
])
</script>

<template>
  <section class="card node">
    <header class="head">
      <Icon name="server" :size="18" />
      <h2 class="card-title host" :title="host?.host">{{ host?.host ?? 'Node' }}</h2>
    </header>
    <div class="chips">
      <span class="chip">{{ host?.role ?? 'Standalone' }}</span>
      <span class="chip">
        <span class="status-dot" :style="{ background: running ? 'var(--good)' : 'var(--critical)' }" />
        {{ host?.status ?? 'Unknown' }}
      </span>
    </div>
    <div class="gauges">
      <RingGauge label="CPU" :value="host?.cpuUtilization ?? 0" :detail="`of ${host?.cpuCores ?? '-'} cores`" />
      <RingGauge label="Heap" :value="host?.memoryUtilization ?? 0" :detail="`${host?.memoryAllocated ?? '-'} of ${host?.memoryUsed ?? '-'} process`" />
    </div>
    <dl class="facts">
      <div v-for="f in facts" :key="f.label">
        <dt class="muted">{{ f.label }}</dt>
        <dd>{{ f.value }}</dd>
      </div>
    </dl>
    <footer class="foot">
      <span class="muted">Throughput</span>
      <div class="spark"><Sparkline :values="throughput" :height="28" /></div>
      <strong>{{ formatRate(samples.at(-1)?.totalRate ?? 0) }}</strong><span class="muted">msg/s</span>
    </footer>
  </section>
</template>

<style scoped>
.node {
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}
.head {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--text-secondary);
}
.host {
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: -6px;
}
.gauges {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.facts {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px 16px;
  margin: 0;
}
.facts dt {
  font-size: 0.78rem;
}
.facts dd {
  margin: 0;
  font-weight: 500;
}
.foot {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
  font-size: 0.875rem;
}
.foot .spark {
  flex: 1;
  min-width: 40px;
}
.foot strong {
  font-size: 1rem;
}
</style>
