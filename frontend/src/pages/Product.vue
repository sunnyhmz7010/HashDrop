<!-- 从原购买页拆分，沿用 HashPay 的 panel 和 Naive UI 组件。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { ref, watch } from "vue";
import { useRoute } from "vue-router";
import { NAlert, NButton, NSkeleton } from "naive-ui";
import { api, errorText, type Product } from "../api";
import { t } from "../i18n";
import { restorePurchase } from "../claim";
import StoreLayout from "../components/StoreLayout.vue";
import MarkdownContent from "../components/MarkdownContent.vue";
import PurchaseDialog from "../components/PurchaseDialog.vue";
import ProductIcon from "../components/ProductIcon.vue";
import ProductSummary from "../components/ProductSummary.vue";

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
    <main class="page product-page">
      <template v-if="product">
        <section class="panel product-summary">
          <router-link
            v-if="!product.hidden"
            to="/"
            custom
            v-slot="{ href, navigate }"
          >
            <n-button
              class="product-back"
              tag="a"
              :href="href"
              circle
              secondary
              :aria-label="t('product.back')"
              :title="t('product.back')"
              @click="navigate"
            >
              <template #icon><ProductIcon name="back" /></template>
            </n-button>
          </router-link>
          <ProductSummary :product="product" heading="h1">
            <template #actions>
              <n-button
                type="primary"
                size="large"
                :disabled="
                  ['card', 'account'].includes(product.kind) &&
                  product.available === 0 &&
                  !restorePurchase(product.id)
                "
                @click="purchaseOpen = true"
                >{{
                  ["card", "account"].includes(product.kind) &&
                  product.available === 0 &&
                  !restorePurchase(product.id)
                    ? t("product.sold_out")
                    : t("shop.buy")
                }}</n-button
              >
            </template>
          </ProductSummary>
        </section>
        <section v-if="product.details?.trim()" class="panel product-details">
          <MarkdownContent :content="product.details" />
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
.product-details {
  min-width: 0;
}
</style>
