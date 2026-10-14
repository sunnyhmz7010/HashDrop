<!-- 基于 HashPay src/app/components/OrderRow.vue 修改：去掉批量选择，展示商品与交付状态。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { computed } from "vue";
import { deliveryNames, money, paymentNames, type Order } from "../api";
const props = defineProps<{ order: Order }>();
defineEmits<{ open: [] }>();
const statusClass = computed(
  () =>
    ({
      paid: "is-success",
      expired: "is-muted",
      invalid: "is-error",
      pending: "is-warning",
      creating: "is-warning",
      review: "is-warning",
      closed: "is-muted",
    })[props.order.payment_status],
);
</script>

<template>
  <div class="order-row order-row--plain">
    <div class="order-cell order-id-cell">
      <button
        type="button"
        class="order-id-button"
        :title="order.id"
        @click="$emit('open')"
      >
        {{ order.id }}
      </button>
      <span :title="order.product_name">{{ order.product_name }}</span>
    </div>
    <div>
      <span class="order-status" :class="statusClass">{{
        paymentNames[order.payment_status]
      }}</span>
    </div>
    <div class="order-cell order-amount-cell">
      <strong>{{ money(order.total, order.currency) }}</strong>
    </div>
    <div class="order-cell order-payment-cell">
      <strong>{{ deliveryNames[order.delivery_status] }}</strong
      ><span>数量 {{ order.quantity }}</span>
    </div>
    <div class="order-cell order-time-cell">
      <span>{{
        new Date(order.created_at * 1000).toLocaleString("zh-CN", {
          hour12: false,
        })
      }}</span>
    </div>
  </div>
</template>
