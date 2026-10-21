<!-- 基于 HashPay OrderRow.vue 修改，共用订单和捐赠的记录展示。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { computed } from "vue";
import { money, paymentNames } from "../api";
import { formatTime } from "../i18n";

const props = defineProps<{
  id: string;
  title: string;
  paymentStatus: string;
  amount: number;
  currency: string;
  createdAt: number;
}>();
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
    })[props.paymentStatus],
);
</script>

<template>
  <div class="order-row order-row--plain">
    <div class="order-cell order-id-cell">
      <button
        type="button"
        class="order-id-button"
        :title="id"
        @click="$emit('open')"
      >
        {{ id }}
      </button>
      <span :title="title">{{ title }}</span>
    </div>
    <div>
      <span class="order-status" :class="statusClass">{{
        paymentNames[paymentStatus]
      }}</span>
    </div>
    <div class="order-cell order-amount-cell">
      <strong>{{ money(amount, currency) }}</strong>
    </div>
    <div class="order-cell order-payment-cell"><slot /></div>
    <div class="order-cell order-time-cell">
      <span>{{ formatTime(createdAt) }}</span>
    </div>
  </div>
</template>
