<!-- 商品卡片沿用 HashPay 的 panel 与 Naive UI 样式，商城使用响应式网格。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { t } from "../i18n";

import { onMounted, ref } from "vue";
import { NAlert, NButton, NEmpty, NSkeleton, NSpin, NTag } from "naive-ui";
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
  <StoreLayout>
    <template #actions>
      <router-link to="/admin"
        ><n-button size="small" secondary>{{
          t("nav.admin")
        }}</n-button></router-link
      >
    </template>
    <main class="checkout-page catalog-page">
      <n-alert v-if="notice" type="info" :show-icon="false">{{
        notice
      }}</n-alert>
      <n-alert v-if="error" type="error"
        >{{ errorText(error) }}
        <n-button text @click="load">{{
          t("common.reload")
        }}</n-button></n-alert
      >
      <n-spin :show="loading && products.length > 0"
        ><section
          v-if="loading && !products.length"
          class="product-grid"
          aria-busy="true"
          :aria-label="t('shop.loading')"
        >
          <div
            v-for="row in 3"
            :key="row"
            class="panel loading-card"
          >
            <n-skeleton text width="60%" />
            <n-skeleton text :repeat="3" />
            <n-skeleton text width="40%" />
            <n-skeleton height="34px" />
          </div>
        </section>
        <section v-else-if="products.length" class="product-grid">
          <article
            v-for="product in products"
            :key="product.id"
            class="panel product-card"
          >
            <div class="product-info">
              <div class="product-heading">
                <strong :title="product.name">{{ product.name }}</strong>
                <n-tag size="small">{{ kindNames[product.kind] }}</n-tag>
              </div>
              <p class="product-description" :title="product.description">
                {{ product.description || t("shop.default_description") }}
              </p>
              <p class="product-stock">
                {{
                  ["card", "account"].includes(product.kind)
                    ? t("stock.count", { quantity: product.available })
                    : t("shop.after_payment")
                }}
              </p>
            </div>
            <div class="product-footer">
              <strong class="product-price">{{
                money(product.price, product.currency)
              }}</strong
              ><router-link :to="`/products/${product.id}`"
                ><n-button type="primary">{{
                  t("shop.view")
                }}</n-button></router-link
              >
            </div>
          </article>
        </section>
        <div v-else-if="!loading && !error" class="panel empty-panel">
          <n-empty :description="t('shop.empty')" /></div
      ></n-spin></main
  ></StoreLayout>
</template>

<style scoped lang="scss">
.catalog-page {
  grid-template-columns: minmax(0, 1fr);
}

.product-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.product-card,
.loading-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}

.product-card p {
  color: #6b7280;
  margin: 0;
}

.loading-card {
  gap: 12px;
}

.product-info {
  display: grid;
  gap: 12px;
  min-width: 0;
}

.product-description {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
  line-clamp: 3;
  line-height: 1.6;
  overflow: hidden;
  overflow-wrap: anywhere;
  white-space: pre-line;
}

.product-heading {
  align-items: flex-start;
  display: flex;
  gap: 8px;
  min-width: 0;
}

.product-heading strong {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}

.product-heading .n-tag {
  flex-shrink: 0;
}

.product-footer {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  justify-content: space-between;
  margin-top: auto;
}

.product-price {
  font-size: 18px;
  min-width: 0;
  overflow-wrap: anywhere;
}

@media (max-width: 980px) {
  .product-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .product-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
