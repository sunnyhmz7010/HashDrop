<script setup lang="ts">
import { NEmpty, NSpin } from "naive-ui";
import { t } from "../i18n";
import type { Order } from "../api";
import OrderRow from "./OrderRow.vue";

defineProps<{
  items: Order[];
  loading: boolean;
  emptyText: string;
  showEmpty?: boolean;
}>();
defineEmits<{ open: [order: Order] }>();
</script>

<template>
  <n-spin :show="loading">
    <div class="orders-table">
      <div class="orders-table-head order-row--plain">
        <span>{{ t("orders.product_column") }}</span
        ><span>{{ t("orders.payment_status") }}</span
        ><span>{{ t("common.amount") }}</span
        ><span>{{ t("orders.delivery_column") }}</span
        ><span>{{ t("common.created_at") }}</span>
      </div>
      <OrderRow
        v-for="order in items"
        :key="order.id"
        :order="order"
        @open="$emit('open', order)"
      />
      <div
        v-if="!items.length && !loading && showEmpty !== false"
        class="orders-table-empty"
      >
        <n-empty :description="emptyText" />
      </div>
    </div>
  </n-spin>
</template>
