<!-- 购买页基于 HashPay Pay.vue 与 PaymentSelector.vue 修改，保留凭单和操作区结构。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { t } from "../i18n";

import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NAlert,
  NButton,
  NInputNumber,
  NSkeleton,
  NTag,
  useMessage,
} from "naive-ui";
import StoreLayout from "../components/StoreLayout.vue";
import {
  api,
  errorText,
  kindNames,
  money,
  type Product,
  type Order,
} from "../api";
const route = useRoute(),
  router = useRouter(),
  message = useMessage(),
  product = ref<Product>(),
  quantity = ref(1),
  loading = ref(true),
  busy = ref(false),
  error = ref("");
const max = computed(() =>
  product.value && ["card", "account"].includes(product.value.kind)
    ? Math.min(100, product.value.available)
    : 100,
);
const total = computed(() =>
  product.value
    ? money(product.value.price * quantity.value, product.value.currency)
    : "",
);
watch(
  () => route.params.id,
  async (id, _previous, onCleanup) => {
    const controller = new AbortController();
    let active = true;
    onCleanup(() => {
      active = false;
      controller.abort();
    });
    loading.value = true;
    error.value = "";
    product.value = undefined;
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
  { immediate: true },
);
async function buy() {
  if (busy.value || !product.value) return;
  busy.value = true;
  try {
    const result = await api<{ order: Order; token: string }>("/orders", {
      method: "POST",
      body: { product_id: product.value.id, quantity: quantity.value },
    });
    await router.push(`/orders/${result.order.id}#token=${result.token}`);
  } catch (e) {
    message.error(errorText(e));
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <StoreLayout>
    <main v-if="product" class="checkout-page">
      <section class="checkout-receipt">
        <div class="checkout-receipt-head">
          <div>
            <span>{{ t("common.product") }}</span
            ><strong :title="product.name">{{ product.name }}</strong>
          </div>
          <n-tag :bordered="false">{{
            product.active ? t("product.on_sale") : t("product.inactive")
          }}</n-tag>
        </div>
        <div class="checkout-amount-block">
          <span>{{ t("product.price_label") }}</span
          ><strong>{{ money(product.price, product.currency) }}</strong>
        </div>
        <dl class="checkout-receipt-meta">
          <div>
            <dt>{{ t("product.description") }}</dt>
            <dd class="pre-wrap">
              {{ product.description || t("product.default_description") }}
            </dd>
          </div>
          <div>
            <dt>{{ t("product.kind") }}</dt>
            <dd>{{ kindNames[product.kind] }}</dd>
          </div>
          <div v-if="['card', 'account'].includes(product.kind)">
            <dt>{{ t("overview.stock") }}</dt>
            <dd>{{ product.available }}</dd>
          </div>
        </dl>
      </section>
      <section class="checkout-flow">
        <div class="checkout-flow-head">
          <span>{{ t("product.purchase") }}</span>
          <h1>{{ t("product.confirm") }}</h1>
        </div>
        <div class="checkout-step-back">
          <span>{{ product.name }}</span
          ><router-link class="text-link" to="/">{{
            t("product.back")
          }}</router-link>
        </div>
        <div class="field-stack">
          <label class="field-label" for="quantity">{{
            t("product.quantity")
          }}</label
          ><n-input-number
            id="quantity"
            :value="quantity"
            :min="1"
            :max="Math.max(1, max)"
            :precision="0"
            @update:value="(value) => (quantity = value ?? 1)"
          />
        </div>
        <div class="checkout-due-amount">
          <div>
            <span>{{ t("product.total") }}</span
            ><strong class="checkout-currency-title">{{ total }}</strong>
          </div>
        </div>
        <n-button
          type="primary"
          size="large"
          block
          :loading="busy"
          :disabled="max === 0 || busy"
          @click="buy"
          >{{
            max === 0 ? t("product.sold_out") : t("product.create_order")
          }}</n-button
        >
        <small class="field-help">{{ t("product.payment_help") }}</small>
      </section>
    </main>
    <main v-else class="checkout-page">
      <template v-if="loading">
        <section
          class="checkout-receipt"
          aria-busy="true"
          :aria-label="t('product.loading')"
        >
          <div class="checkout-receipt-head">
            <n-skeleton text width="34%" />
          </div>
          <div class="checkout-amount-block">
            <n-skeleton text width="26%" /><n-skeleton
              text
              :repeat="2"
              width="64%"
            />
          </div>
          <div class="form-grid"><n-skeleton text :repeat="3" /></div>
        </section>
        <section
          class="checkout-flow"
          aria-busy="true"
          :aria-label="t('product.loading_purchase')"
        >
          <div class="checkout-flow-head">
            <n-skeleton text width="34%" /><n-skeleton
              text
              :repeat="2"
              width="64%"
            />
          </div>
          <div class="form-grid">
            <n-skeleton text :repeat="3" /><n-skeleton text :repeat="2" />
          </div>
        </section>
      </template>
      <n-alert v-else-if="error" type="error"
        >{{ errorText(error) }}
        <n-button text @click="router.go(0)">{{
          t("common.reload")
        }}</n-button></n-alert
      >
    </main>
  </StoreLayout>
</template>
