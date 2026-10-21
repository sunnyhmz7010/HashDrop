<!-- 订单与领取页基于 HashPay Pay.vue 的凭单、付款结果和复制字段结构修改。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。
     支付状态直接展示网关核验结果，付款渠道和链上信息由 HashPay 页面提供。 -->
<script setup lang="ts">
import { formatTime, t } from "../i18n";
import { computed, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NAlert,
  NButton,
  NCheckbox,
  NSkeleton,
  NTag,
  useMessage,
} from "naive-ui";
import {
  api,
  copyText,
  deliveryNames,
  errorText,
  money,
  paymentNames,
  type OrderDetail,
} from "../api";
import {
  forgetPurchase,
  rememberClaimKey,
  rememberOrder,
  restoreOrderKey,
  historyCleared,
} from "../claim";
import OrderQueryDialog from "../components/OrderQueryDialog.vue";

const route = useRoute(),
  router = useRouter(),
  message = useMessage();
const detail = ref<OrderDetail>(),
  error = ref(""),
  busy = ref(false),
  token = ref(""),
  orderID = ref(""),
  queryOpen = ref(false),
  storageError = ref(false),
  credentialSaved = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined,
  controller: AbortController | undefined,
  generation = 0;
const credential = computed(() =>
  orderID.value && token.value
    ? `${location.origin}/center/orders/${orderID.value}#token=${encodeURIComponent(token.value)}`
    : "",
);
function stop() {
  clearTimeout(timer);
  controller?.abort();
  generation++;
  busy.value = false;
}
async function load() {
  const current = generation;
  if (busy.value || !token.value || !orderID.value) return;
  clearTimeout(timer);
  controller = new AbortController();
  busy.value = true;
  try {
    const result = await api<OrderDetail>(`/orders/${orderID.value}`, {
      token: token.value,
      signal: controller.signal,
    });
    if (current !== generation) return;
    detail.value = result;
    const orderSaved = rememberOrder(result.order, token.value);
    error.value = "";
    storageError.value = !rememberClaimKey(token.value) && !orderSaved;
    if (result.order.delivery_status === "done")
      forgetPurchase(result.order.id);
    if (
      !["closed", "invalid"].includes(result.order.payment_status) &&
      result.order.delivery_status !== "done"
    )
      timer = setTimeout(load, 8000);
  } catch (e) {
    if (current !== generation) return;
    error.value = errorText(e);
  } finally {
    if (current === generation) busy.value = false;
  }
}
watch(
  () => [route.params.id, route.hash],
  () => {
    stop();
    detail.value = undefined;
    error.value = "";
    storageError.value = false;
    credentialSaved.value = false;
    orderID.value = String(route.params.id ?? "");
    const fromURL = new URLSearchParams(route.hash.slice(1)).get("token");
    try {
      token.value =
        fromURL ??
        (restoreOrderKey(orderID.value) ||
          sessionStorage.getItem(`hashdrop:${orderID.value}`) ||
          "");
      if (fromURL) sessionStorage.setItem(`hashdrop:${orderID.value}`, fromURL);
    } catch {
      token.value = fromURL ?? "";
      storageError.value = true;
    }
    void load();
  },
  { immediate: true },
);
onUnmounted(stop);
watch(historyCleared, () => {
  stop();
  detail.value = undefined;
  token.value = orderID.value = "";
  error.value = "";
  credentialSaved.value = false;
  queryOpen.value = false;
  void router.replace("/center/history");
});
async function queried(result: OrderDetail, key: string) {
  const path = `/center/orders/${encodeURIComponent(result.order.id)}`;
  if (route.path !== path || route.hash) await router.replace(path);
  else {
    stop();
    detail.value = undefined;
    error.value = "";
    token.value = key;
    credentialSaved.value = false;
    await load();
  }
}
async function copy(value: string) {
  try {
    await copyText(value);
    if (value === credential.value) credentialSaved.value = true;
    message.success(t("common.copied"));
  } catch {
    message.warning(t("common.copy_fallback"));
  }
}
function pay() {
  const orderSaved =
    detail.value && rememberOrder(detail.value.order, token.value);
  storageError.value = !rememberClaimKey(token.value) && !orderSaved;
  if (storageError.value && !credentialSaved.value) return;
  if (detail.value?.order.checkout_url)
    location.assign(detail.value.order.checkout_url);
}
const expires = computed(() =>
  detail.value?.order.expires_at
    ? formatTime(detail.value.order.expires_at)
    : null,
);
const statusType = computed(() => {
  const status = detail.value?.order.payment_status;
  if (status === "paid") return "success";
  if (status === "expired" || status === "review") return "warning";
  return status === "invalid" || status === "closed" ? "error" : "default";
});
</script>
<template>
  <div class="guest-order-view grid">
    <div class="section-title">
      <h2>{{ t("center.order_detail") }}</h2>
      <n-button secondary @click="router.push('/center/history')">{{
        t("center.back_history")
      }}</n-button>
    </div>
    <main v-if="!detail" class="grid">
      <section class="panel grid claim-panel">
        <n-skeleton v-if="busy" text :repeat="3" />
        <n-alert v-if="orderID && !token" type="warning">{{
          t("claim.missing")
        }}</n-alert>
        <n-alert v-if="error" type="error"
          >{{ errorText(error) }}
          <n-button text :loading="busy" @click="load">{{
            t("common.retry")
          }}</n-button>
        </n-alert>
        <n-button type="primary" @click="queryOpen = true">{{
          t("common.query")
        }}</n-button>
      </section>
    </main>
    <main v-else class="checkout-page">
      <section class="checkout-receipt">
        <template v-if="detail">
          <div class="checkout-receipt-head">
            <div>
              <span>{{ t("claim.receipt") }}</span
              ><strong>{{ detail.order.product_name }}</strong>
            </div>
            <n-tag :type="statusType">{{
              paymentNames[detail.order.payment_status]
            }}</n-tag>
          </div>
          <div class="checkout-amount-block">
            <span>{{ t("orders.amount") }}</span
            ><strong>{{
              money(detail.order.total, detail.order.currency)
            }}</strong>
          </div>
          <dl class="checkout-receipt-meta">
            <div v-if="detail.order.discount_total > 0">
              <dt>{{ t("promotion.original_total") }}</dt>
              <dd>
                {{ money(detail.order.original_total, detail.order.currency) }}
              </dd>
            </div>
            <div v-for="discount in detail.order.discounts" :key="discount.id">
              <dt>
                {{ discount.name
                }}<small v-if="discount.code"> · {{ discount.code }}</small>
              </dt>
              <dd>−{{ money(discount.amount, detail.order.currency) }}</dd>
            </div>
            <div>
              <dt>{{ t("orders.id") }}</dt>
              <dd>
                <code :title="detail.order.id">{{ detail.order.id }}</code>
                <n-button
                  size="tiny"
                  text
                  type="primary"
                  @click="copy(detail.order.id)"
                  >{{ t("common.copy") }}</n-button
                >
              </dd>
            </div>
            <div>
              <dt>{{ t("orders.info") }}</dt>
              <dd>
                {{ detail.order.product_name }} × {{ detail.order.quantity }}
              </dd>
            </div>
            <div>
              <dt>{{ t("common.created_at") }}</dt>
              <dd>
                {{ formatTime(detail.order.created_at) }}
              </dd>
            </div>
            <div v-if="expires">
              <dt>{{ t("orders.deadline") }}</dt>
              <dd>{{ expires }}</dd>
            </div>
          </dl>
          <div v-if="detail.order.buyer_note" class="order-note-block">
            <strong>{{ t("orders.buyer_note") }}</strong>
            <p>{{ detail.order.buyer_note }}</p>
          </div>
          <div v-if="detail.order.delivery_note" class="order-note-block">
            <strong>{{ t("product.delivery_note") }}</strong>
            <p>{{ detail.order.delivery_note }}</p>
          </div>
        </template>
      </section>
      <section
        class="checkout-flow"
        :class="{
          'checkout-flow--paid': detail?.order.payment_status === 'paid',
        }"
      >
        <template v-if="detail?.order.payment_status === 'paid'">
          <div class="checkout-paid-state">
            <div class="checkout-paid-mark" aria-hidden="true">✓</div>
            <div>
              <strong>{{ t("claim.paid") }}</strong
              ><span>{{ t("claim.gateway_confirmed") }}</span>
            </div>
          </div>
          <dl class="checkout-paid-meta">
            <div>
              <dt>{{ t("delivery.status") }}</dt>
              <dd>{{ deliveryNames[detail.order.delivery_status] }}</dd>
            </div>
          </dl>
        </template>
        <div
          v-else-if="
            detail &&
            ['expired', 'invalid', 'closed'].includes(
              detail.order.payment_status,
            )
          "
          class="checkout-expired-state"
        >
          <strong>{{ paymentNames[detail.order.payment_status] }}</strong>
          <span>{{
            detail.order.payment_status === "expired"
              ? t("claim.expired_help")
              : t("claim.closed_help")
          }}</span>
        </div>
        <div v-else class="checkout-flow-head">
          <span>{{ t("claim.order_title") }}</span>
          <h1>{{ t("claim.status_title") }}</h1>
          <small class="field-help">{{ t("claim.status_help") }}</small>
        </div>
        <div class="checkout-copy-field">
          <span>{{ t("claim.link") }}</span>
          <code :title="credential">{{ credential }}</code>
          <n-button size="small" secondary @click="copy(credential)">{{
            t("claim.copy_link")
          }}</n-button>
        </div>
        <n-alert v-if="storageError" type="warning">{{
          t("claim.storage_help")
        }}</n-alert>
        <n-alert v-if="error" type="error"
          >{{ errorText(error) }}
          <n-button text :loading="busy" @click="load">{{
            t("common.recheck")
          }}</n-button></n-alert
        >
        <template v-if="detail">
          <n-alert
            v-if="detail.order.payment_status === 'review'"
            type="warning"
            >{{ t("claim.review_help") }}</n-alert
          >
          <n-alert
            v-else-if="detail.order.payment_status === 'creating'"
            type="info"
            >{{ t("claim.creating_help") }}</n-alert
          >
          <n-alert
            v-else-if="
              detail.order.payment_status === 'paid' &&
              detail.order.delivery_status === 'manual'
            "
            type="warning"
            >{{
              t(
                detail.order.kind === "manual"
                  ? "claim.manual_waiting"
                  : "claim.manual_help",
              )
            }}</n-alert
          >
          <n-checkbox
            v-if="storageError && detail.order.payment_status === 'pending'"
            v-model:checked="credentialSaved"
            >{{ t("claim.saved_manually") }}</n-checkbox
          >
          <n-button
            v-if="
              detail.order.checkout_url &&
              detail.order.payment_status === 'pending'
            "
            type="primary"
            size="large"
            block
            :disabled="storageError && !credentialSaved"
            @click="pay"
            >{{ t("claim.pay") }}</n-button
          >
          <div class="form-actions">
            <n-button :loading="busy" secondary size="small" @click="load">{{
              t("orders.refresh_status")
            }}</n-button>
            <n-button secondary size="small" @click="queryOpen = true">{{
              t("claim.other")
            }}</n-button>
          </div>
          <div
            v-if="detail.deliveries.some((item) => item.status === 'done')"
            class="deliveries grid"
          >
            <div class="section-title">
              <h2>{{ t("delivery.content") }}</h2>
              <span class="muted">{{
                t("delivery.progress", {
                  done: detail.deliveries.filter(
                    (item) => item.status === "done",
                  ).length,
                  total: detail.order.quantity,
                })
              }}</span>
            </div>
            <article
              v-for="item in detail.deliveries.filter(
                (item) => item.status === 'done',
              )"
              :key="item.id"
              class="delivery-item checkout-result"
            >
              <div class="section-title">
                <span>{{ t("delivery.item", { ordinal: item.ordinal }) }}</span
                ><n-button size="small" @click="copy(item.content ?? '')">{{
                  t("delivery.copy_content")
                }}</n-button>
              </div>
              <pre>{{ item.content }}</pre>
            </article>
          </div>
        </template>
      </section>
    </main>
    <OrderQueryDialog v-model:show="queryOpen" @found="queried" />
  </div>
</template>

<style scoped lang="scss">
.guest-order-view .checkout-page {
  padding: 0;
  width: 100%;
}
.claim-panel {
  max-width: 460px;
  margin: 0 auto;
}

.checkout-result .section-title {
  margin-bottom: 0;
}
.order-note-block {
  margin-top: 18px;
  overflow-wrap: anywhere;
  font-size: 14px;
}
.order-note-block p {
  margin: 8px 0 0;
  white-space: pre-wrap;
}
</style>
