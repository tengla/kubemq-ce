<script setup>
import { inject } from 'vue'
import Icon from './Icon.vue'

defineProps({
  title: { type: String, required: true },
  icon: { type: String, default: '' },
  back: { type: Object, default: null }, // { to, label }
})

const openMenu = inject('openMenu')
</script>

<template>
  <header class="page-header">
    <button type="button" class="menu" aria-label="Open navigation" @click="openMenu"><Icon name="menu" /></button>
    <div class="heading">
      <RouterLink v-if="back" :to="back.to" class="back"><Icon name="arrow-left" :size="16" />{{ back.label }}</RouterLink>
      <h1><Icon v-if="icon" :name="icon" :size="26" /><span class="title">{{ title }}</span></h1>
      <div v-if="$slots.subtitle" class="subtitle muted"><slot name="subtitle" /></div>
    </div>
    <div class="actions"><slot name="actions" /></div>
  </header>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 22px;
}
.heading {
  min-width: 0;
  flex: 1;
}
.back {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
  font-size: 0.875rem;
  color: var(--text-secondary);
}
.back:hover {
  color: var(--text-primary);
}
h1 {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0;
  font-size: 1.9rem;
  font-weight: 700;
  min-width: 0;
}
.title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.subtitle {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 14px;
  margin-top: 6px;
  font-size: 0.875rem;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 10px;
}
.menu {
  display: none;
  flex: none;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface-1);
  width: 38px;
  height: 38px;
  place-items: center;
  cursor: pointer;
}
@media (max-width: 900px) {
  .menu {
    display: grid;
  }
  h1 {
    font-size: 1.5rem;
  }
}
@media (max-width: 640px) {
  .page-header {
    flex-wrap: wrap;
  }
  .actions {
    width: 100%;
    justify-content: flex-start;
  }
}
</style>
