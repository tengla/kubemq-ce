<script setup>
defineProps({
  // [{ label, value, caption }]
  items: { type: Array, required: true },
})
</script>

<template>
  <dl class="card strip">
    <div v-for="item in items" :key="item.label" class="stat">
      <dt>{{ item.label }}</dt>
      <dd>{{ item.value }}</dd>
      <dd v-if="item.caption" class="caption muted">{{ item.caption }}</dd>
    </div>
  </dl>
</template>

<style scoped>
.strip {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  margin: 0 0 18px;
  padding: 4px 0;
}
.stat {
  padding: 12px 20px;
  border-left: 1px solid var(--border);
  min-width: 0;
}
.stat:first-child {
  border-left: none;
}
dt {
  font-size: 0.82rem;
  color: var(--text-secondary);
}
dd {
  margin: 4px 0 0;
  font-size: 1.35rem;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
dd.caption {
  margin-top: 0;
  font-size: 0.78rem;
  font-weight: 400;
}
@media (max-width: 640px) {
  .strip {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .stat {
    border-left: none;
  }
}
</style>
