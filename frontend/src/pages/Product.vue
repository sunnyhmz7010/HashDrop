<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NAlert,
  NButton,
  NInputNumber,
  NSpin,
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
  <StoreLayout
    ><n-spin :show="loading">
      <main v-if="product" class="checkout-page">
        <section class="checkout-receipt">
          <div class="checkout-receipt-head">
            <div>
              <span>{{ kindNames[product.kind] }}</span
              ><strong>{{ product.name }}</strong>
            </div>
            <n-tag :bordered="false">{{
              product.active ? "在售" : "已下架"
            }}</n-tag>
          </div>
          <div class="checkout-amount-block">
            <span>单价</span
            ><strong>{{ money(product.price, product.currency) }}</strong>
          </div>
          <p class="pre-wrap muted">
            {{
              product.description || "支付后可通过专属领取链接查看商品内容。"
            }}
          </p>
          <router-link class="text-link" to="/">返回商城</router-link>
        </section>
        <section class="checkout-flow">
          <div class="checkout-flow-head">
            <span>匿名购买</span>
            <h1>{{ product.name }}</h1>
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
          <span class="muted" v-if="['card', 'account'].includes(product.kind)"
            >可用库存：{{ product.available }}</span
          >
          <div class="checkout-receipt-head">
            <span>合计</span><strong>{{ total }}</strong>
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
      <div v-else class="page">
        <n-alert v-if="error" type="error">{{ error }}</n-alert>
        <p v-else class="muted">正在加载商品…</p>
      </div>
    </n-spin></StoreLayout
  >
</template>
