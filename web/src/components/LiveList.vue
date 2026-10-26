<script setup>
import Icon from './Icon.vue'
import MessageView from './MessageView.vue'

defineProps({
  list: { type: Object, required: true }, // from useLiveList()
  live: { type: Boolean, default: false },
  emptyText: { type: String, required: true },
  details: { type: Function, default: () => [] },
})
</script>

<template>
  <section class="live">
    <div class="bar">
      <span class="state" :class="{ on: live }"><span class="status-dot" :style="{ background: live ? 'var(--good)' : 'var(--axis)' }" />{{ live ? 'Live' : 'Stopped' }}</span>
      <span class="muted count num">
        {{ list.received.value }} received<template v-if="list.received.value > list.limit"> · showing the latest {{ list.limit }}</template>
      </span>
      <label class="filter">
        <Icon name="search" :size="16" />
        <span class="sr-only">Filter messages</span>
        <input v-model="list.filter.value" class="input" type="search" placeholder="Filter" />
      </label>
      <button type="button" class="btn btn-small" :disabled="!list.items.value.length" @click="list.clear()">Clear</button>
    </div>
    <div v-if="!list.items.value.length" class="empty-state">
      <p>{{ live ? 'Waiting for messages…' : emptyText }}</p>
    </div>
    <p v-else-if="!list.visible.value.length" class="muted">No messages match the filter.</p>
    <div v-else class="items">
      <template v-for="entry in list.visible.value" :key="entry.key">
        <slot :entry="entry">
          <MessageView :message="entry.item" :details="details(entry.item)" />
        </slot>
      </template>
    </div>
  </section>
</template>

<style scoped>
.live {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.state {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-weight: 500;
  font-size: 0.875rem;
}
.count {
  font-size: 0.85rem;
}
.filter {
  position: relative;
  display: flex;
  align-items: center;
  margin-left: auto;
  width: min(240px, 100%);
  color: var(--text-muted);
}
.filter :deep(svg) {
  position: absolute;
  left: 10px;
  pointer-events: none;
}
.filter .input {
  min-height: 32px;
  padding: 4px 10px 4px 32px;
}
.items {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
</style>
