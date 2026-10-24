<!-- 首页和详情顶部共用商品信息、排列与字号层级。 -->
<script setup lang="ts">
import { money, type Product } from "../api";
import { t } from "../i18n";

withDefaults(
  defineProps<{
    product: Product;
    heading?: "h1" | "h2";
    truncateIntroduction?: boolean;
  }>(),
  { heading: "h2", truncateIntroduction: false },
);
</script>

<template>
  <div class="product-summary-content">
    <div class="product-info">
      <component :is="heading" class="product-heading" :title="product.name">{{
        product.name
      }}</component>
      <p
        v-if="product.description.trim()"
        class="product-description"
        :class="{ 'product-description-truncated': truncateIntroduction }"
        :title="truncateIntroduction ? product.description : undefined"
      >
        {{ product.description }}
      </p>
      <p class="product-stock">
        {{
          product.delivery_mode === "automatic" && !product.stock_unlimited
            ? t("stock.count", { quantity: product.available })
            : product.kind === "manual"
              ? t("product.manual_help")
              : t("shop.after_payment")
        }}
      </p>
      <p
        v-if="product.min_quantity > 1 || product.max_quantity < 100"
        class="product-stock"
      >
        {{
          t("product.quantity_range", {
            min: product.min_quantity,
            max: product.max_quantity,
          })
        }}
      </p>
    </div>
    <div class="product-footer">
      <div class="product-pricing">
        <strong class="product-price">{{
          money(product.sale_price ?? product.price, product.currency)
        }}</strong>
        <del v-if="product.sale_price !== undefined">{{
          money(product.price, product.currency)
        }}</del>
        <small v-if="product.offer_name">{{ product.offer_name }}</small>
      </div>
      <div class="product-actions"><slot name="actions" /></div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.product-summary-content {
  display: flex;
  flex-direction: column;
  gap: 16px;
  flex: 1;
  min-width: 0;
}
.product-info {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.product-heading {
  font-size: 20px;
  line-height: 1.4;
  font-weight: 700;
  margin: 0;
  overflow-wrap: anywhere;
}
.product-description,
.product-stock {
  font-size: 14px;
  line-height: 1.6;
  color: #6b7280;
  margin: 0;
  overflow-wrap: anywhere;
}
.product-description {
  white-space: pre-line;
}
.product-description-truncated {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
  line-clamp: 3;
  overflow: hidden;
}
.product-footer {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 12px;
  margin-top: auto;
}
.product-price {
  font-size: 18px;
  line-height: 1.4;
  min-width: 0;
  overflow-wrap: anywhere;
}
.product-pricing {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 6px 10px;
  min-width: 0;
  del,
  small {
    color: #6b7280;
    font-size: 13px;
    overflow-wrap: anywhere;
  }
}
.product-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}
</style>
