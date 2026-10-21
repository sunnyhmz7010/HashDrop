<!-- 基于 HashPay src/app/components/OrderRow.vue 修改：去掉批量选择，展示商品与交付状态。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { t } from "../i18n";
import { deliveryNames, type Order } from "../api";
import HistoryRow from "./HistoryRow.vue";
defineProps<{ order: Order }>();
defineEmits<{ open: [] }>();
</script>

<template>
  <HistoryRow
    :id="order.id"
    :title="order.product_name"
    :payment-status="order.payment_status"
    :amount="order.total"
    :currency="order.currency"
    :created-at="order.created_at"
    @open="$emit('open')"
  >
    <strong>{{ deliveryNames[order.delivery_status] }}</strong>
    <span>{{ t("orders.quantity", { quantity: order.quantity }) }}</span>
  </HistoryRow>
</template>
