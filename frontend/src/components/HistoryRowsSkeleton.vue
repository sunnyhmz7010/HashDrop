<!-- 沿用 HashPay OrderRow 的列结构，供订单与个人记录共用首屏骨架。 -->
<script setup lang="ts">
import { NSkeleton } from "naive-ui";
withDefaults(defineProps<{ columns?: number }>(), { columns: 5 });
</script>

<template>
  <div
    v-for="row in 3"
    :key="row"
    class="order-row order-row--plain"
    aria-hidden="true"
  >
    <div v-for="column in columns" :key="column" class="order-cell">
      <n-skeleton text width="78%" />
      <n-skeleton v-if="column === 1" text width="52%" />
    </div>
  </div>
</template>
