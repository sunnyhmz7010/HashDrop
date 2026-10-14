<!-- 从原购买页拆分，沿用 HashPay 的 panel 和 Naive UI 组件。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { ref, watch } from "vue";
import { useRoute } from "vue-router";
import { NAlert, NButton, NSkeleton } from "naive-ui";
import { api, errorText, money, type Product } from "../api";
import { t } from "../i18n";
import StoreLayout from "../components/StoreLayout.vue";
import MarkdownContent from "../components/MarkdownContent.vue";
import PurchaseDialog from "../components/PurchaseDialog.vue";
import ProductIcon from "../components/ProductIcon.vue";

const route = useRoute();
const product = ref<Product>(),
  loading = ref(true),
  error = ref(""),
  purchaseOpen = ref(false),
  reload = ref(0);
watch(
  () => [route.params.id, reload.value],
  async ([id], _previous, onCleanup) => {
    const controller = new AbortController();
    let active = true;
    onCleanup(() => {
      active = false;
      controller.abort();
    });
    product.value = undefined;
    loading.value = true;
    error.value = "";
    purchaseOpen.value = false;
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
    <main class="page product-page">
      <template v-if="product">
        <section class="panel product-summary">
          <router-link to="/" custom v-slot="{ href, navigate }">
            <n-button
              class="product-back"
              tag="a"
              :href="href"
              secondary
              @click="navigate"
            >
              <template #icon><ProductIcon name="back" /></template>
              {{ t("product.back") }}
            </n-button>
          </router-link>
          <h1>{{ product.name }}</h1>
          <div class="product-summary-bottom">
            <div>
              <strong class="detail-price">{{
                money(product.price, product.currency)
              }}</strong>
              <p class="helper">
                {{
                  ["card", "account"].includes(product.kind)
                    ? t("stock.count", { quantity: product.available })
                    : t("shop.after_payment")
                }}
              </p>
            </div>
            <n-button
              type="primary"
              size="large"
              :disabled="
                ['card', 'account'].includes(product.kind) &&
                product.available === 0
              "
              @click="purchaseOpen = true"
              >{{
                ["card", "account"].includes(product.kind) &&
                product.available === 0
                  ? t("product.sold_out")
                  : t("shop.buy")
              }}</n-button
            >
          </div>
        </section>
        <section class="panel product-details">
          <MarkdownContent
            v-if="product.details?.trim()"
            :content="product.details"
          />
          <p v-else class="pre-wrap">
            {{ product.description || t("product.default_description") }}
          </p>
        </section>
      </template>
      <template v-else-if="loading"
        ><section
          class="panel"
          aria-busy="true"
          :aria-label="t('product.loading')"
        >
          <n-skeleton text width="60%" /><n-skeleton text :repeat="3" />
        </section>
        <section class="panel" aria-busy="true">
          <n-skeleton text :repeat="6" /></section
      ></template>
      <n-alert v-else-if="error" type="error"
        >{{ errorText(error)
        }}<n-button text @click="reload++">{{
          t("common.reload")
        }}</n-button></n-alert
      >
    </main>
    <PurchaseDialog
      v-model:show="purchaseOpen"
      :productID="product?.id ?? null"
    />
  </StoreLayout>
</template>

<style scoped lang="scss">
.product-page {
  display: grid;
  gap: 18px;
}
.product-summary {
  display: grid;
  gap: 18px;
}
.product-back {
  justify-self: start;
}
.product-summary h1 {
  font-size: 26px;
  line-height: 1.4;
  margin: 0;
  overflow-wrap: anywhere;
}
.product-summary-bottom {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  flex-wrap: wrap;
}
.detail-price {
  font-size: 30px;
  line-height: 1.4;
  overflow-wrap: anywhere;
}
.product-summary-bottom .helper {
  margin: 6px 0 0;
}
.product-details {
  min-width: 0;
}
@media (max-width: 640px) {
  .product-summary h1 {
    font-size: 22px;
  }
  .detail-price {
    font-size: 26px;
  }
  .product-summary-bottom > div {
    flex: 1 1 100%;
    min-width: 0;
  }
  .product-summary-bottom .n-button {
    width: 100%;
  }
}
</style>
