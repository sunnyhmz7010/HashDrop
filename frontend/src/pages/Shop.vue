<script setup lang="ts">
import { onMounted, ref } from "vue";
import { NAlert, NButton, NEmpty, NSpin, NTag } from "naive-ui";
import StoreLayout from "../components/StoreLayout.vue";
import { api, errorText, kindNames, money, type Product } from "../api";
const products = ref<Product[]>([]),
  loading = ref(true),
  error = ref(""),
  notice = ref("");
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [items, site] = await Promise.all([
      api<Product[]>("/products"),
      api<{ notice: string }>("/site"),
    ]);
    products.value = items;
    notice.value = site.notice;
  } catch (e) {
    error.value = errorText(e);
  } finally {
    loading.value = false;
  }
}
onMounted(load);
</script>
<template>
  <StoreLayout
    ><main class="page grid catalog-page">
      <section class="catalog-heading">
        <div class="section-title"><h1>选好商品，付款后领取</h1></div>
        <p>无需注册。请保存订单领取链接，它是查看交付内容的凭证。</p>
      </section>
      <n-alert
        v-if="notice"
        type="info"
        :show-icon="false"
        class="space-bottom"
        >{{ notice }}</n-alert
      >
      <n-alert v-if="error" type="error" class="space-bottom"
        >{{ error }} <n-button text @click="load">重新加载</n-button></n-alert
      >
      <n-spin :show="loading"
        ><div v-if="products.length" class="product-grid">
          <article
            v-for="product in products"
            :key="product.id"
            class="panel product-card"
          >
            <div class="card-top">
              <n-tag size="small" :bordered="false">{{
                kindNames[product.kind]
              }}</n-tag
              ><span class="muted">{{
                ["card", "account"].includes(product.kind)
                  ? `库存 ${product.available}`
                  : "支付后交付"
              }}</span>
            </div>
            <h2>{{ product.name }}</h2>
            <p class="product-description">
              {{ product.description || "购买后凭专属链接领取商品。" }}
            </p>
            <div class="card-bottom">
              <strong>{{ money(product.price, product.currency) }}</strong
              ><router-link :to="`/products/${product.id}`"
                ><n-button
                  :disabled="
                    ['card', 'account'].includes(product.kind) &&
                    product.available === 0
                  "
                  >{{
                    ["card", "account"].includes(product.kind) &&
                    product.available === 0
                      ? "暂时售罄"
                      : "查看商品"
                  }}</n-button
                ></router-link
              >
            </div>
          </article>
        </div>
        <div v-else-if="!loading && !error" class="panel empty-panel">
          <n-empty description="暂无上架商品" /></div
      ></n-spin></main
  ></StoreLayout>
</template>

<style scoped lang="scss">
.catalog-heading p {
  margin: 0;
  color: #6b7280;
}

.product-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}

.product-card {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.product-card h2 {
  margin: 0;
  font-size: 20px;
  overflow-wrap: anywhere;
}

.card-top,
.card-bottom {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.card-top {
  font-size: 12px;
}

.card-bottom {
  margin-top: auto;
}

.card-bottom strong {
  font-size: 17px;
}

.product-description {
  color: #6b7280;
  font-size: 14px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 0;
}

@media (max-width: 820px) {
  .product-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 520px) {
  .product-grid {
    grid-template-columns: 1fr;
  }
}
</style>
