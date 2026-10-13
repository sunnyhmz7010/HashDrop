<script setup lang="ts">
import { onMounted, ref } from "vue";
import { NAlert, NButton, NEmpty, NSpin, NTag } from "naive-ui";
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
  <section class="page-heading">
    <p class="eyebrow">数字商品</p>
    <h1>选好商品，付款后领取</h1>
    <p>无需注册。请保存订单领取链接，它是查看交付内容的凭证。</p>
  </section>
  <n-alert v-if="notice" type="info" :show-icon="false" class="space-bottom">{{
    notice
  }}</n-alert>
  <n-alert v-if="error" type="error" class="space-bottom"
    >{{ error }} <n-button text @click="load">重新加载</n-button></n-alert
  >
  <n-spin :show="loading"
    ><div v-if="products.length" class="product-grid">
      <article
        v-for="product in products"
        :key="product.id"
        class="product-card"
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
  ></n-spin>
</template>
