<script setup>
import Icon from './Icon.vue'
import Sparkline from './Sparkline.vue'

defineProps({
  label: { type: String, required: true },
  icon: { type: String, required: true },
  value: { type: String, required: true },
  unit: { type: String, default: '' },
  caption: { type: String, default: '' },
  trend: { type: Array, default: () => [] },
})
</script>

<template>
  <section class="card tile">
    <h3 class="label"><Icon :name="icon" :size="16" />{{ label }}</h3>
    <p class="value">
      {{ value }}<span v-if="unit" class="unit">{{ unit }}</span>
    </p>
    <p class="caption muted" :title="caption">{{ caption || ' ' }}</p>
    <Sparkline :values="trend" />
  </section>
</template>

<style scoped>
.tile {
  padding: 16px 16px 10px;
  min-width: 0;
}
.label {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--text-secondary);
}
.value {
  margin: 10px 0 0;
  font-size: 1.9rem;
  font-weight: 600;
  line-height: 1.1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.unit {
  margin-left: 6px;
  font-size: 0.95rem;
  font-weight: 500;
  color: var(--text-secondary);
}
.caption {
  margin: 4px 0 6px;
  font-size: 0.8rem;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
