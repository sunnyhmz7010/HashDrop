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
  <router-link class="back-link" to="/">← 返回商城</router-link
  ><n-spin :show="loading"
    ><n-alert v-if="error" type="error">{{ error }}</n-alert>
    <div v-if="product" class="purchase-grid">
      <section class="panel product-detail">
        <n-tag :bordered="false">{{ kindNames[product.kind] }}</n-tag>
        <h1>{{ product.name }}</h1>
        <p class="pre-wrap muted">
          {{ product.description || "支付后可通过专属领取链接查看商品内容。" }}
        </p>
      </section>
      <section class="panel purchase-panel">
        <span class="muted">单价</span>
        <h2>{{ money(product.price, product.currency) }}</h2>
        <label class="field-label" for="quantity">购买数量</label
        ><n-input-number
          id="quantity"
          :value="quantity"
          :min="1"
          :max="Math.max(1, max)"
          :precision="0"
          @update:value="(value) => (quantity = value ?? 1)"
        />
        <p class="muted" v-if="['card', 'account'].includes(product.kind)">
          可用库存：{{ product.available }}
        </p>
        <div class="total-row">
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
        <p class="helper">创建订单后先保存领取链接，再前往 HashPay 付款。</p>
      </section>
    </div></n-spin
  >
</template>
