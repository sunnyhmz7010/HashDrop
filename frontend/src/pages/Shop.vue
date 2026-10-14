<!-- 商品列表基于 HashPay Merchants.vue 的 panel/list-card 结构修改。
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
  <StoreLayout
    ><main class="checkout-page catalog-page">
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
          class="panel grid"
          aria-busy="true"
          :aria-label="t('shop.loading')"
        >
          <div v-for="row in 3" :key="row" class="list-card loading-row">
            <n-skeleton text width="34%" /><n-skeleton
              text
              width="82%"
            /><n-skeleton text width="64%" />
          </div>
        </section>
        <section v-else-if="products.length" class="panel grid">
          <article
            v-for="product in products"
            :key="product.id"
            class="list-card merchant-card product-card"
          >
            <div class="merchant-card-main">
              <div class="merchant-card-title">
                <strong :title="product.name">{{ product.name }}</strong>
                <n-tag size="small">{{ kindNames[product.kind] }}</n-tag>
              </div>
              <p class="product-description" :title="product.description">
                {{ product.description || t("shop.default_description") }}
              </p>
              <p>
                {{
                  ["card", "account"].includes(product.kind)
                    ? t("stock.count", { quantity: product.available })
                    : t("shop.after_payment")
                }}
              </p>
            </div>
            <div class="merchant-card-actions">
              <strong>{{ money(product.price, product.currency) }}</strong
              ><router-link :to="`/products/${product.id}`"
                ><n-button size="small" quaternary>{{
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

.list-card {
  align-items: flex-start;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  display: flex;
  gap: 12px;
  justify-content: space-between;
  padding: 14px;
}

.list-card p {
  color: #6b7280;
  margin: 4px 0 0;
}

.loading-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
}

.merchant-card-main {
  display: grid;
  gap: 5px;
  min-width: 0;
}

.merchant-card-main p {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.merchant-card-title {
  align-items: center;
  display: flex;
  gap: 8px;
  min-width: 0;
}

.merchant-card-title strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.merchant-card-actions {
  align-items: flex-end;
  align-self: stretch;
  color: #6b7280;
  display: flex;
  flex-direction: column;
  flex: 0 0 auto;
  gap: 8px;
  justify-content: space-between;
}
</style>
