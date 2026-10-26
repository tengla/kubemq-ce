<script setup>
import PageHeader from '../components/PageHeader.vue'
</script>

<template>
  <div>
    <PageHeader title="Page not found" icon="alert" />
    <p class="muted">There's nothing at this address. <RouterLink to="/dashboard" class="link">Go to Home</RouterLink>.</p>
  </div>
</template>
