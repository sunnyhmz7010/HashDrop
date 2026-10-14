<!-- 购买页基于 HashPay Pay.vue 与 PaymentSelector.vue 修改，保留凭单和操作区结构。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
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
            <span>商品</span
            ><strong :title="product.name">{{ product.name }}</strong>
          </div>
          <n-tag :bordered="false">{{
            product.active ? "在售" : "已下架"
          }}</n-tag>
        </div>
        <div class="checkout-amount-block">
          <span>单价</span
          ><strong>{{ money(product.price, product.currency) }}</strong>
        </div>
        <dl class="checkout-receipt-meta">
          <div>
            <dt>商品说明</dt>
            <dd class="pre-wrap">
              {{
                product.description || "支付后可通过专属领取链接查看商品内容。"
              }}
            </dd>
          </div>
          <div>
            <dt>商品类型</dt>
            <dd>{{ kindNames[product.kind] }}</dd>
          </div>
          <div v-if="['card', 'account'].includes(product.kind)">
            <dt>可用库存</dt>
            <dd>{{ product.available }}</dd>
          </div>
        </dl>
      </section>
      <section class="checkout-flow">
        <div class="checkout-flow-head">
          <span>购买商品</span>
          <h1>确认购买</h1>
        </div>
        <div class="checkout-step-back">
          <span>{{ product.name }}</span
          ><router-link class="text-link" to="/">返回商城</router-link>
        </div>
        <div class="field-stack">
          <label class="field-label" for="quantity">购买数量</label
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
            <span>合计</span
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
          >{{ max === 0 ? "暂时售罄" : "创建订单" }}</n-button
        >
        <small class="field-help"
          >创建订单后先保存领取链接，再前往 HashPay 付款。</small
        >
      </section>
    </main>
    <main v-else class="checkout-page">
      <template v-if="loading">
        <section
          class="checkout-receipt"
          aria-busy="true"
          aria-label="正在加载商品"
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
          aria-label="正在加载购买信息"
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
        >{{ error }}
        <n-button text @click="router.go(0)">重新加载</n-button></n-alert
      >
    </main>
  </StoreLayout>
</template>
