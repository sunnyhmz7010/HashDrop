<!-- 沿用 HashPay 弹窗样式。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import {
  NAlert,
  NButton,
  NCheckbox,
  NInput,
  NInputNumber,
  NSkeleton,
  useMessage,
} from "naive-ui";
import {
  api,
  APIError,
  copyText,
  errorText,
  money,
  type Product,
  type Order,
  type OrderDetail,
  type OrderQuote,
} from "../api";
import {
  forgetPurchase,
  generateClaimKey,
  newPurchaseID,
  rememberClaimKey,
  rememberOrder,
  rememberPurchase,
  restorePurchase,
  historyCleared,
  clearedOrderIDs,
} from "../claim";
import { t } from "../i18n";
import FormDialog from "./FormDialog.vue";

const show = defineModel<boolean>("show", { required: true });
const props = defineProps<{ productID: string | null }>();
const router = useRouter(),
  message = useMessage();
const product = ref<Product>(),
  quantity = ref(1),
  key = ref(""),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  locked = ref(false),
  storageError = ref(false),
  copiedKey = ref(""),
  manualSaved = ref(false),
  order = ref<Order>(),
  suggestions = ref<string[]>([]),
  reload = ref(0);
const coupon = ref(""),
  quote = ref<OrderQuote>(),
  frozenQuote = ref<OrderQuote>(),
  quoting = ref(false),
  quoteError = ref(""),
  quoteReload = ref(0);
let requestID = "",
  controller: AbortController | undefined;
const max = computed(() =>
  product.value && ["card", "account"].includes(product.value.kind)
    ? Math.min(100, product.value.available)
    : 100,
);
const validQuantity = computed(
  () =>
    Number.isInteger(quantity.value) &&
    quantity.value >= 1 &&
    quantity.value <= max.value,
);
const validKey = computed(
  () => key.value.trim().length > 0 && Array.from(key.value).length <= 128,
);
const keySaved = computed(
  () => manualSaved.value || (!!key.value && copiedKey.value === key.value),
);
const pricing = computed(
  () => order.value ?? (locked.value ? frozenQuote.value : quote.value),
);
const pricingReady = computed(() =>
  locked.value
    ? !!order.value || !!frozenQuote.value || !!quote.value
    : !quoting.value && !!quote.value,
);
watch(
  () =>
    [
      show.value,
      product.value?.id,
      quantity.value,
      coupon.value,
      quoteReload.value,
      locked.value,
    ] as const,
  ([open, id, count, code], previous, onCleanup) => {
    quote.value = undefined;
    quoteError.value = "";
    quoting.value = false;
    if (
      open !== previous?.[0] ||
      id !== previous?.[1] ||
      count !== previous?.[2] ||
      code !== previous?.[3]
    )
      error.value = "";
    if (
      !open ||
      !id ||
      (locked.value && (order.value || frozenQuote.value)) ||
      !validQuantity.value
    )
      return;
    const quoteController = new AbortController();
    let active = true;
    quoting.value = true;
    const timer = setTimeout(async () => {
      try {
        const result = await api<OrderQuote>("/orders/quote", {
          method: "POST",
          body: { product_id: id, quantity: count, coupon_code: code },
          signal: quoteController.signal,
        });
        if (active) {
          quote.value = result;
          if (locked.value && !order.value && !frozenQuote.value)
            frozenQuote.value = result;
        }
      } catch (e) {
        if (active) quoteError.value = errorText(e);
      } finally {
        if (active) quoting.value = false;
      }
    }, 250);
    onCleanup(() => {
      active = false;
      clearTimeout(timer);
      quoteController.abort();
    });
  },
);
watch(key, () => {
  manualSaved.value = false;
});
const total = computed(() =>
  pricing.value ? money(pricing.value.total, pricing.value.currency) : "—",
);
watch(
  () => [show.value, props.productID, reload.value] as const,
  async ([open, id], previous, onCleanup) => {
    if (!open || !id) return;
    const fetchController = new AbortController();
    let active = true;
    onCleanup(() => {
      active = false;
      fetchController.abort();
    });
    if (!previous?.[0] || previous[1] !== id) {
      const pending = restorePurchase(id);
      key.value = pending?.key ?? "";
      quantity.value = pending?.quantity ?? 1;
      requestID = pending?.requestID ?? newPurchaseID();
      locked.value = !!pending;
      coupon.value = pending?.couponCode ?? "";
      frozenQuote.value = pending?.quote;
      order.value = undefined;
      suggestions.value = [];
      storageError.value = false;
      copiedKey.value = "";
      manualSaved.value = false;
    }
    product.value = undefined;
    error.value = "";
    loading.value = true;
    try {
      const [result, restored] = await Promise.allSettled([
        api<Product>(`/products/${id}`, { signal: fetchController.signal }),
        locked.value
          ? api<OrderDetail>("/orders/lookup", {
              method: "POST",
              body: { key: key.value },
              signal: fetchController.signal,
            })
          : Promise.resolve(undefined),
      ] as const);
      if (!active) return;
      if (result.status === "fulfilled") product.value = result.value;
      if (
        restored.status === "fulfilled" &&
        restored.value?.order.id === requestID &&
        restored.value.order.product_id === id
      )
        order.value = restored.value.order;
      if (!product.value && !order.value && result.status === "rejected")
        error.value = errorText(result.reason);
    } catch (e) {
      if (active) error.value = errorText(e);
    } finally {
      if (active) loading.value = false;
    }
  },
);
onUnmounted(() => controller?.abort());
watch(historyCleared, () => {
  // 清除另一标签页的已创建订单时，取消本页重试，避免旧响应恢复凭证。
  if (!show.value || !clearedOrderIDs.value.includes(requestID)) return;
  controller?.abort();
  busy.value = false;
  forgetPurchase(requestID);
  show.value = false;
});
function chooseKey(value: string) {
  if (busy.value || locked.value) return;
  key.value = value;
  suggestions.value = [];
  error.value = "";
}
async function copyKey(event: MouseEvent) {
  const value = key.value;
  try {
    await copyText(value, event.currentTarget as HTMLElement);
    copiedKey.value = value;
    message.success(t("common.copied"));
  } catch {
    message.warning(t("common.copy_fallback"));
  }
}
async function openClaim() {
  rememberClaimKey(key.value);
  if (order.value) rememberOrder(order.value, key.value);
  await router.push("/center/query");
  show.value = false;
}
async function buy() {
  if (
    busy.value ||
    loading.value ||
    (!product.value && !order.value) ||
    !pricingReady.value ||
    !validKey.value ||
    (!locked.value && !validQuantity.value)
  )
    return;
  busy.value = true;
  error.value = "";
  suggestions.value = [];
  controller = new AbortController();
  const signal = controller.signal;
  const wasLocked = locked.value;
  if (!locked.value || !frozenQuote.value)
    frozenQuote.value = order.value ?? quote.value ?? frozenQuote.value;
  const saved = rememberPurchase({
    requestID,
    productID: order.value?.product_id ?? product.value!.id,
    quantity: quantity.value,
    key: key.value,
    couponCode: coupon.value,
    quote: frozenQuote.value,
  });
  storageError.value = !rememberClaimKey(key.value) || !saved;
  if (storageError.value && !keySaved.value) {
    busy.value = false;
    return;
  }
  try {
    const result = await api<{ order: Order }>("/orders", {
      method: "POST",
      body: {
        product_id: order.value?.product_id ?? product.value!.id,
        quantity: quantity.value,
        key: key.value,
        request_id: requestID,
        coupon_code: coupon.value,
        quoted_total: frozenQuote.value?.total,
        quoted_currency: frozenQuote.value?.currency,
      },
      signal,
    });
    if (signal.aborted) return;
    order.value = result.order;
    rememberOrder(result.order, key.value);
    locked.value = true;
    const deadline = Date.now() + 20_000;
    while (!signal.aborted) {
      if (
        order.value.payment_status === "pending" &&
        order.value.checkout_url
      ) {
        if (!rememberClaimKey(key.value) && !keySaved.value) {
          storageError.value = true;
          return;
        }
        forgetPurchase(requestID);
        location.assign(order.value.checkout_url);
        return;
      }
      if (
        order.value.payment_status !== "creating" &&
        order.value.payment_status !== "pending"
      ) {
        await openClaim();
        return;
      }
      if (Date.now() >= deadline)
        throw new Error(t("errors.payment_link_wait"));
      await new Promise((resolve) => setTimeout(resolve, 1000));
      if (signal.aborted) return;
      const detail = await api<OrderDetail>("/orders/lookup", {
        method: "POST",
        body: { key: key.value },
        signal,
      });
      order.value = detail.order;
    }
  } catch (e) {
    if (signal.aborted) return;
    if (e instanceof APIError && e.code === "key_conflict") {
      suggestions.value = e.suggestions;
      locked.value = false;
      forgetPurchase(requestID);
    } else if (
      !order.value &&
      e instanceof APIError &&
      ((!wasLocked && e.status >= 400 && e.status < 500) || e.status === 409)
    ) {
      locked.value = false;
      forgetPurchase(requestID);
      frozenQuote.value = undefined;
      if (e.status === 409) quoteReload.value++;
    } else {
      // 请求可能已提交；保留相同密钥、数量及请求标识，重试不能创建第二笔订单。
      locked.value = true;
    }
    error.value = errorText(e);
  } finally {
    if (!signal.aborted) busy.value = false;
  }
}
</script>

<template>
  <FormDialog
    v-model:show="show"
    :title="t('product.confirm')"
    :busy="busy"
    width="min(480px, calc(100vw - 28px))"
  >
    <form id="purchase-form" class="purchase-form" @submit.prevent="buy">
      <template v-if="loading"
        ><n-skeleton text width="60%" /><n-skeleton text :repeat="3"
      /></template>
      <template v-else-if="product || order">
        <div class="purchase-summary">
          <strong>{{ order?.product_name ?? product?.name }}</strong
          ><span v-if="!locked || order || pricing">{{
            money(
              order?.unit_price ??
                (pricing ? pricing.original_total / quantity : product!.price),
              pricing?.currency ?? product!.currency,
            )
          }}</span>
        </div>
        <div class="field-stack">
          <label class="field-label" for="purchase-quantity">{{
            t("product.quantity")
          }}</label>
          <n-input-number
            :value="quantity"
            :min="1"
            :max="Math.max(1, max, locked ? quantity : 1)"
            :precision="0"
            :disabled="busy || locked || max === 0"
            :input-props="{ id: 'purchase-quantity' }"
            @update:value="(value) => (quantity = value ?? 1)"
          />
        </div>
        <div class="field-stack">
          <label class="field-label" for="purchase-coupon">{{
            t("promotion.checkout_code")
          }}</label>
          <n-input
            id="purchase-coupon"
            v-model:value="coupon"
            :maxlength="64"
            :disabled="busy || locked"
            :placeholder="t('promotion.checkout_placeholder')"
            :input-props="{ autocomplete: 'off' }"
          />
        </div>
        <div class="field-stack">
          <label class="field-label" for="purchase-key">{{
            t("claim.key")
          }}</label>
          <n-input
            v-model:value="key"
            :disabled="busy || locked"
            :placeholder="t('claim.key_placeholder')"
            :input-props="{
              id: 'purchase-key',
              autocomplete: 'off',
              spellcheck: false,
            }"
          >
            <!-- 阻止后缀按钮触发输入框聚焦，避免移动端弹出键盘。 -->
            <template #suffix
              ><div class="key-actions" @mousedown.stop>
                <n-button
                  text
                  type="primary"
                  :disabled="busy || locked"
                  @click="chooseKey(generateClaimKey())"
                  >{{ t("claim.generate") }}</n-button
                >
                <n-button
                  text
                  type="primary"
                  :disabled="!key"
                  @click="copyKey"
                  >{{ t("common.copy") }}</n-button
                >
              </div></template
            >
          </n-input>
          <small class="field-help">{{ t("claim.key_help") }}</small>
        </div>
        <div v-if="suggestions.length" class="field-stack">
          <span class="field-label">{{ t("claim.suggestions") }}</span>
          <div class="key-suggestions">
            <n-button
              v-for="candidate in suggestions"
              :key="candidate"
              size="small"
              secondary
              @click="chooseKey(candidate)"
              >{{ candidate }}</n-button
            >
          </div>
        </div>
        <div v-if="quoting && !locked" class="purchase-quote-loading">
          <n-skeleton text :repeat="2" />
        </div>
        <div
          v-else-if="pricing && pricing.discount_total > 0"
          class="purchase-discounts"
        >
          <div>
            <span>{{ t("promotion.original_total") }}</span
            ><span>{{ money(pricing.original_total, pricing.currency) }}</span>
          </div>
          <div v-for="discount in pricing.discounts" :key="discount.id">
            <span
              >{{ discount.name
              }}<small v-if="discount.code"> · {{ discount.code }}</small></span
            ><span>−{{ money(discount.amount, pricing.currency) }}</span>
          </div>
          <small
            v-if="
              coupon.trim() &&
              !pricing.discounts.some((discount) => discount.code)
            "
            >{{ t("promotion.best_single") }}</small
          >
        </div>
        <div class="purchase-total">
          <span>{{ t("product.total") }}</span
          ><strong>{{
            locked && !pricing ? t("purchase.unknown_total") : total
          }}</strong>
        </div>
        <n-alert v-if="busy" type="info">{{
          t("purchase.connecting")
        }}</n-alert>
        <n-alert v-else-if="locked" type="info">{{
          t("purchase.resume_help")
        }}</n-alert>
        <n-alert v-if="storageError" type="warning">{{
          t("claim.storage_help")
        }}</n-alert>
        <n-checkbox
          v-if="storageError"
          v-model:checked="manualSaved"
          :disabled="busy"
          >{{ t("claim.saved_manually") }}</n-checkbox
        >
      </template>
      <n-alert v-if="error || quoteError" type="error"
        >{{ errorText(quoteError || error)
        }}<n-button
          v-if="(!product && !order) || quoteError"
          text
          @click="!product && !order ? reload++ : quoteReload++"
          >{{ t("common.reload") }}</n-button
        ></n-alert
      >
    </form>
    <template #footer>
      <n-button :disabled="busy" @click="show = false">{{
        t("common.cancel")
      }}</n-button>
      <n-button v-if="locked" :disabled="busy" @click="openClaim">{{
        t("claim.view")
      }}</n-button>
      <n-button
        attr-type="submit"
        form="purchase-form"
        type="primary"
        :loading="busy"
        :disabled="
          loading ||
          (!product && !order) ||
          !validKey ||
          !pricingReady ||
          (!locked && !validQuantity) ||
          busy ||
          (storageError && !keySaved)
        "
        >{{
          product && max === 0 && !locked
            ? t("product.sold_out")
            : locked
              ? t("purchase.resume")
              : t("product.create_order")
        }}</n-button
      >
    </template>
  </FormDialog>
</template>

<style scoped lang="scss">
.purchase-form,
.purchase-summary,
.key-suggestions {
  display: grid;
  gap: 18px;
  min-width: 0;
}
.purchase-summary {
  gap: 8px;
}
.purchase-summary strong {
  font-size: 18px;
  overflow-wrap: anywhere;
}
.purchase-summary span,
.purchase-total > span {
  color: #6b7280;
}
.purchase-total {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  border-top: 1px solid #e5e7eb;
  padding-top: 16px;
}
.purchase-total strong {
  font-size: 22px;
  overflow-wrap: anywhere;
}
.purchase-discounts {
  display: grid;
  gap: 10px;
  color: #6b7280;
  font-size: 14px;
}
.purchase-discounts > div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
.purchase-discounts > div > span {
  overflow-wrap: anywhere;
}
.purchase-discounts > div > span:last-child {
  flex-shrink: 0;
}
.key-actions {
  display: flex;
  gap: 10px;
}
.key-suggestions {
  gap: 8px;
}
.key-suggestions :deep(.n-button) {
  height: auto;
  min-height: 28px;
  max-width: 100%;
  padding: 6px 10px;
}
.key-suggestions :deep(.n-button__content) {
  white-space: normal;
  overflow-wrap: anywhere;
  min-width: 0;
}
</style>
