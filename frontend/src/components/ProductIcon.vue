<script setup lang="ts">
defineProps<{ name: "view" | "buy" | "back" }>();
</script>

<template>
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <template v-if="name === 'view'">
      <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z" />
      <circle cx="12" cy="12" r="3" />
    </template>
    <template v-else-if="name === 'buy'">
      <path d="M5 7h14l1 14H4L5 7Z" />
      <path d="M8 8V6a4 4 0 0 1 8 0v2" />
    </template>
    <template v-else>
      <path d="M19 12H5M12 5l-7 7 7 7" />
    </template>
  </svg>
</template>

<style scoped>
svg {
  display: block;
  width: 20px;
  height: 20px;
}
</style>
