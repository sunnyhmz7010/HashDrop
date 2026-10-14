<!-- 从原商品购买页抽出，沿用 HashPay 弹窗样式。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NInputNumber, NSkeleton } from "naive-ui";
import { api, errorText, money, type Product, type Order } from "../api";
import { t } from "../i18n";
import FormDialog from "./FormDialog.vue";

const show = defineModel<boolean>("show", { required: true });
const props = defineProps<{ productID: string | null }>();
const router = useRouter();
const product = ref<Product>(),
  quantity = ref(1),
  loading = ref(false),
  busy = ref(false),
  error = ref("");
const reload = ref(0);
const max = computed(() =>
  product.value && ["card", "account"].includes(product.value.kind)
    ? Math.min(100, product.value.available)
    : 100,
);
const validQuantity = computed(
  () =>
    Number.isInteger(quantity.value) &&
    quantity.value >= 1 &&
    quantity.value <= max.value,
);
const total = computed(() =>
  product.value
    ? money(product.value.price * quantity.value, product.value.currency)
    : "",
);
watch(
  () => [show.value, props.productID, reload.value] as const,
  async ([open, id], _previous, onCleanup) => {
    if (!open || !id) return;
    const controller = new AbortController();
    let active = true;
    onCleanup(() => {
      active = false;
      controller.abort();
    });
    product.value = undefined;
    error.value = "";
    loading.value = true;
    quantity.value = 1;
    try {
      const result = await api<Product>(`/products/${id}`, {
        signal: controller.signal,
      });
      if (active) product.value = result;
    } catch (e) {
      if (active) error.value = errorText(e);
    } finally {
      if (active) loading.value = false;
    }
  },
);
async function buy() {
  if (busy.value || loading.value || !product.value || !validQuantity.value)
    return;
  busy.value = true;
  error.value = "";
  try {
    const result = await api<{ order: Order; token: string }>("/orders", {
      method: "POST",
      body: { product_id: product.value.id, quantity: quantity.value },
    });
    // 凭证只进入订单 Fragment，不能放到查询参数或商品详情中。
    await router.push(`/orders/${result.order.id}#token=${result.token}`);
    show.value = false;
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <FormDialog
    v-model:show="show"
    :title="t('product.confirm')"
    :busy="busy"
    width="min(480px, calc(100vw - 28px))"
  >
    <form id="purchase-form" class="purchase-form" @submit.prevent="buy">
      <template v-if="loading"
        ><n-skeleton text width="60%" /><n-skeleton text :repeat="3"
      /></template>
      <template v-else-if="product">
        <div class="purchase-summary">
          <strong>{{ product.name }}</strong
          ><span>{{ money(product.price, product.currency) }}</span>
        </div>
        <p v-if="['card', 'account'].includes(product.kind)" class="helper">
          {{ t("stock.count", { quantity: product.available }) }}
        </p>
        <div class="field-stack">
          <label class="field-label" for="purchase-quantity">{{
            t("product.quantity")
          }}</label>
          <n-input-number
            :value="quantity"
            :min="1"
            :max="Math.max(1, max)"
            :precision="0"
            :disabled="busy || max === 0"
            :input-props="{ id: 'purchase-quantity' }"
            @update:value="(value) => (quantity = value ?? 1)"
          />
        </div>
        <div class="purchase-total">
          <span>{{ t("product.total") }}</span
          ><strong>{{ total }}</strong>
        </div>
      </template>
      <n-alert v-if="error" type="error"
        >{{ errorText(error)
        }}<n-button v-if="!product" text @click="reload++">{{
          t("common.reload")
        }}</n-button></n-alert
      >
    </form>
    <template #footer
      ><n-button :disabled="busy" @click="show = false">{{
        t("common.cancel")
      }}</n-button
      ><n-button
        attr-type="submit"
        form="purchase-form"
        type="primary"
        :loading="busy"
        :disabled="loading || !product || !validQuantity || busy"
        >{{
          product && max === 0
            ? t("product.sold_out")
            : t("product.create_order")
        }}</n-button
      ></template
    >
  </FormDialog>
</template>

<style scoped lang="scss">
.purchase-form {
  display: grid;
  gap: 18px;
  min-width: 0;
}
.purchase-summary {
  display: grid;
  gap: 8px;
}
.purchase-summary strong {
  font-size: 18px;
  overflow-wrap: anywhere;
}
.purchase-summary span,
.purchase-total > span {
  color: #6b7280;
}
.purchase-form p {
  margin: 0;
}
.purchase-total {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  border-top: 1px solid #e5e7eb;
  padding-top: 16px;
}
.purchase-total strong {
  font-size: 22px;
  overflow-wrap: anywhere;
}
</style>
