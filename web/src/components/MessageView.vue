<script setup>
import { computed } from 'vue'
import { formatBody, formatTime } from '../api'

const props = defineProps({
  message: { type: Object, required: true },
  // Extra label/value pairs shown under the header, e.g. sequence or receive count.
  details: { type: Array, default: () => [] },
})

const id = computed(() => props.message.messageId || props.message.requestId || '')
const tags = computed(() => {
  const raw = props.message.tags
  if (!raw) return []
  if (typeof raw === 'object') return Object.entries(raw)
  try {
    return Object.entries(JSON.parse(raw))
  } catch {
    return [[raw, '']]
  }
})
const visibleDetails = computed(() => props.details.filter((d) => d.value !== '' && d.value != null && d.value !== 0))
</script>

<template>
  <article class="message">
    <header class="head">
      <span v-if="id" class="mono id" :title="id">{{ id }}</span>
      <span class="muted time">{{ formatTime(message.timestamp) }}</span>
      <slot name="status" />
    </header>
    <dl v-if="visibleDetails.length || message.metadata || tags.length" class="meta">
      <div v-for="d in visibleDetails" :key="d.label">
        <dt>{{ d.label }}</dt>
        <dd>{{ d.value }}</dd>
      </div>
      <div v-if="message.metadata">
        <dt>Metadata</dt>
        <dd>{{ message.metadata }}</dd>
      </div>
      <div v-if="tags.length">
        <dt>Tags</dt>
        <dd class="tags">
          <span v-for="[k, v] in tags" :key="k" class="badge">{{ v ? `${k}=${v}` : k }}</span>
        </dd>
      </div>
    </dl>
    <pre class="body mono">{{ formatBody(message.body) || '(empty body)' }}</pre>
    <slot />
  </article>
</template>

<style scoped>
.message {
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface-1);
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}
.head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 14px;
  font-size: 0.82rem;
}
.id {
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}
.meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 22px;
  margin: 0;
  font-size: 0.82rem;
}
.meta dt {
  color: var(--text-muted);
}
.meta dd {
  margin: 2px 0 0;
  overflow-wrap: anywhere;
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.body {
  margin: 0;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--surface-2);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 260px;
  overflow: auto;
}
</style>
