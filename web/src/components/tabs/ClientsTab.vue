<script setup>
import { computed, ref } from 'vue'
import Icon from '../Icon.vue'
import { formatBytes, formatCompact } from '../../snapshot'

const props = defineProps({
  clients: { type: Array, default: () => [] },
})

const PAGE = 100
const search = ref('')
const shown = ref(PAGE)

const sorted = computed(() => {
  const query = search.value.trim().toLowerCase()
  return props.clients.filter((c) => !query || c.name.toLowerCase().includes(query)).sort((a, b) => (b.lastActivity ?? 0) - (a.lastActivity ?? 0))
})
</script>

<template>
  <div class="tab">
    <div class="toolbar">
      <label class="search">
        <Icon name="search" :size="16" />
        <span class="sr-only">Search clients</span>
        <input v-model="search" class="input" type="search" placeholder="Search client IDs" />
      </label>
      <span class="muted count">{{ sorted.length }} {{ sorted.length === 1 ? 'client' : 'clients' }}</span>
    </div>
    <div v-if="!clients.length" class="empty-state"><p>No clients have used this channel yet.</p></div>
    <div v-else class="table-scroll">
      <table class="data">
        <thead>
          <tr>
            <th>Client ID</th>
            <th class="right">Sent</th>
            <th class="right">Received</th>
            <th class="right">Errors</th>
            <th class="right">Last activity</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in sorted.slice(0, shown)" :key="c.name">
            <td class="strong mono">{{ c.name || '(no client ID)' }}</td>
            <td class="right num">{{ formatCompact(c.incoming?.messages) }} · {{ formatBytes(c.incoming?.volume) }}</td>
            <td class="right num">{{ formatCompact(c.outgoing?.messages) }} · {{ formatBytes(c.outgoing?.volume) }}</td>
            <td class="right num">{{ formatCompact(c.total?.errors) }}</td>
            <td class="right">{{ c.lastActivityHuman }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <button v-if="sorted.length > shown" type="button" class="btn more" @click="shown += PAGE">Show more ({{ sorted.length - shown }} left)</button>
  </div>
</template>

<style scoped>
.tab {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px 18px;
}
.search {
  position: relative;
  display: flex;
  align-items: center;
  flex: 0 1 320px;
  color: var(--text-muted);
}
.search :deep(svg) {
  position: absolute;
  left: 12px;
  pointer-events: none;
}
.search .input {
  padding-left: 36px;
}
.count {
  font-size: 0.85rem;
}
.more {
  align-self: flex-start;
}
</style>
