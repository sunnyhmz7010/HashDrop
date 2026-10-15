<!-- 从原商品购买页抽出，沿用 HashPay 弹窗样式。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NInput, NInputNumber, NSkeleton } from "naive-ui";
import {
  api,
  APIError,
  errorText,
  money,
  type Product,
  type Order,
  type OrderQuote,
} from "../api";
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
const coupon = ref(""),
  quote = ref<OrderQuote>(),
  quoting = ref(false),
  quoteError = ref(""),
  quoteReload = ref(0);
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
watch(
  () =>
    [
      show.value,
      product.value?.id,
      quantity.value,
      coupon.value,
      quoteReload.value,
    ] as const,
  ([open, id, count, code], previous, onCleanup) => {
    quote.value = undefined;
    quoteError.value = "";
    if (
      open !== previous?.[0] ||
      id !== previous?.[1] ||
      count !== previous?.[2] ||
      code !== previous?.[3]
    )
      error.value = "";
    quoting.value = false;
    if (!open || !id || !validQuantity.value) return;
    const controller = new AbortController();
    let active = true;
    quoting.value = true;
    const timer = setTimeout(async () => {
      try {
        const result = await api<OrderQuote>("/orders/quote", {
          method: "POST",
          body: {
            product_id: id,
            quantity: quantity.value,
            coupon_code: coupon.value,
          },
          signal: controller.signal,
        });
        if (active) quote.value = result;
      } catch (e) {
        if (active) quoteError.value = errorText(e);
      } finally {
        if (active) quoting.value = false;
      }
    }, 250);
    onCleanup(() => {
      active = false;
      clearTimeout(timer);
      controller.abort();
    });
  },
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
    coupon.value = "";
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
  if (
    busy.value ||
    loading.value ||
    quoting.value ||
    !quote.value ||
    !product.value ||
    !validQuantity.value
  )
    return;
  busy.value = true;
  error.value = "";
  try {
    const result = await api<{ order: Order; token: string }>("/orders", {
      method: "POST",
      body: {
        product_id: product.value.id,
        quantity: quantity.value,
        coupon_code: coupon.value,
        quoted_total: quote.value.total,
        quoted_currency: quote.value.currency,
      },
    });
    // 凭证只进入订单 Fragment，不能放到查询参数或商品详情中。
    await router.push(`/orders/${result.order.id}#token=${result.token}`);
    show.value = false;
  } catch (e) {
    error.value = errorText(e);
    if (e instanceof APIError && e.status === 409) {
      // 先刷新金额，保留弹窗与优惠码，由用户再次确认后提交。
      quoteReload.value++;
    }
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
          ><span>{{
            money(
              quote ? quote.original_total / quantity : product.price,
              quote?.currency ?? product.currency,
            )
          }}</span>
        </div>
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
        <div class="field-stack">
          <label class="field-label" for="purchase-coupon">{{
            t("promotion.checkout_code")
          }}</label>
          <n-input
            id="purchase-coupon"
            v-model:value="coupon"
            :maxlength="64"
            :disabled="busy"
            :placeholder="t('promotion.checkout_placeholder')"
            :input-props="{ autocomplete: 'off' }"
          />
        </div>
        <div v-if="quoting" class="purchase-quote-loading">
          <n-skeleton text :repeat="2" />
        </div>
        <div
          v-else-if="quote && quote.discount_total > 0"
          class="purchase-discounts"
        >
          <div>
            <span>{{ t("promotion.original_total") }}</span
            ><span>{{ money(quote.original_total, quote.currency) }}</span>
          </div>
          <div v-for="discount in quote.discounts" :key="discount.id">
            <span
              >{{ discount.name
              }}<small v-if="discount.code"> · {{ discount.code }}</small></span
            ><span>−{{ money(discount.amount, quote.currency) }}</span>
          </div>
          <small
            v-if="
              coupon.trim() &&
              !quote.discounts.some((discount) => discount.code)
            "
            >{{ t("promotion.best_single") }}</small
          >
        </div>
        <div class="purchase-total">
          <span>{{ t("product.total") }}</span
          ><strong>{{
            quote ? money(quote.total, quote.currency) : "—"
          }}</strong>
        </div>
      </template>
      <n-alert v-if="error || quoteError" type="error"
        >{{ errorText(error || quoteError)
        }}<n-button text @click="!product ? reload++ : quoteReload++">{{
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
        :disabled="
          loading || quoting || !quote || !product || !validQuantity || busy
        "
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
.purchase-discounts {
  display: grid;
  gap: 10px;
  color: #6b7280;
  font-size: 14px;
}
.purchase-discounts > div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
.purchase-discounts > div > span {
  overflow-wrap: anywhere;
}
.purchase-discounts > div > span:last-child {
  flex-shrink: 0;
}
</style>
