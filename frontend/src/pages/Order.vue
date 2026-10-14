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
  NInput,
  NSkeleton,
  NTag,
  useMessage,
} from "naive-ui";
import StoreLayout from "../components/StoreLayout.vue";
import {
  api,
  copyText,
  deliveryNames,
  errorText,
  money,
  paymentNames,
  type OrderDetail,
} from "../api";
const route = useRoute(),
  router = useRouter(),
  message = useMessage(),
  detail = ref<OrderDetail>(),
  error = ref(""),
  busy = ref(false),
  token = ref(""),
  orderID = ref(""),
  claimLink = ref(""),
  storageError = ref(false),
  receiptSaved = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined,
  controller: AbortController | undefined,
  generation = 0;
const receipt = computed(() =>
  orderID.value && token.value
    ? `${location.origin}/orders/${orderID.value}#token=${token.value}`
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
    error.value = "";
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
    token.value = "";
    storageError.value = false;
    receiptSaved.value = false;
    orderID.value = String(route.params.id ?? "");
    if (!orderID.value) return;
    const fromURL = new URLSearchParams(route.hash.slice(1)).get("token");
    try {
      token.value =
        fromURL ?? sessionStorage.getItem(`tinyfaka:${orderID.value}`) ?? "";
      if (fromURL) sessionStorage.setItem(`tinyfaka:${orderID.value}`, fromURL);
    } catch {
      token.value = fromURL ?? "";
      storageError.value = true;
    }
    void load();
  },
  { immediate: true },
);
onUnmounted(stop);
async function claim() {
  let url: URL;
  try {
    url = new URL(claimLink.value.trim());
  } catch {
    message.error(t("errors.claim_link"));
    return;
  }
  try {
    const value = new URLSearchParams(url.hash.slice(1)).get("token");
    if (
      url.origin !== location.origin ||
      !/^\/orders\/[A-Za-z0-9_-]+$/.test(url.pathname) ||
      !value ||
      value.length !== 43
    )
      throw new Error(t("errors.claim_link"));
    await router.push(url.pathname + url.hash);
  } catch (e) {
    message.error(errorText(e));
  }
}
async function copy(value: string) {
  try {
    await copyText(value);
    if (value === receipt.value) receiptSaved.value = true;
    message.success(t("common.copied"));
  } catch {
    message.warning(t("common.copy_fallback"));
  }
}
function pay() {
  if (storageError.value && !receiptSaved.value) return;
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
  return status === "invalid" ? "error" : "default";
});
</script>
<template>
  <StoreLayout>
    <main v-if="!orderID || !token" class="page">
      <section class="panel grid claim-panel">
        <div class="section-title">
          <h1>{{ orderID ? t("claim.order_title") : t("claim.title") }}</h1>
        </div>
        <p class="muted">{{ t("claim.private_help") }}</p>
        <n-alert v-if="orderID" type="warning">{{
          t("claim.missing")
        }}</n-alert>
        <div class="field-stack">
          <label class="field-label" for="claim-link">{{
            t("claim.full_link")
          }}</label
          ><n-input
            id="claim-link"
            v-model:value="claimLink"
            :placeholder="t('claim.placeholder')"
            @keydown.enter="claim"
          />
        </div>
        <div class="form-actions">
          <n-button type="primary" @click="claim">{{
            t("claim.view")
          }}</n-button>
        </div>
        <small class="field-help">{{ t("claim.lost_help") }}</small>
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
        </template>
        <template v-else-if="busy">
          <div class="checkout-receipt-head">
            <n-skeleton text width="34%" />
          </div>
          <div class="checkout-amount-block">
            <n-skeleton text width="26%" /><n-skeleton
              text
              :repeat="2"
              width="64%"
            />
          </div>
          <div class="form-grid"><n-skeleton text :repeat="3" /></div>
        </template>
        <p v-else class="muted">{{ t("claim.not_loaded") }}</p>
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
          <h1>{{ t("claim.save_title") }}</h1>
          <small class="field-help">{{ t("claim.save_help") }}</small>
        </div>
        <div class="checkout-copy-field">
          <span>{{ t("claim.link") }}</span>
          <code :title="receipt">{{ receipt }}</code>
          <n-button size="small" secondary @click="copy(receipt)">{{
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
            >{{ t("claim.manual_help") }}</n-alert
          >
          <n-checkbox v-if="storageError" v-model:checked="receiptSaved">{{
            t("claim.saved_manually")
          }}</n-checkbox>
          <n-button
            v-if="
              detail.order.checkout_url &&
              detail.order.payment_status === 'pending'
            "
            type="primary"
            size="large"
            block
            :disabled="storageError && !receiptSaved"
            @click="pay"
            >{{ t("claim.pay") }}</n-button
          >
          <div class="form-actions">
            <n-button :loading="busy" secondary size="small" @click="load">{{
              t("orders.refresh_status")
            }}</n-button>
          </div>
          <div
            v-if="detail.deliveries.some((item) => item.status === 'done')"
            class="deliveries grid"
          >
            <div class="section-title">
              <h2>{{ t("delivery.content") }}</h2>
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
  </StoreLayout>
</template>

<style scoped lang="scss">
.claim-panel {
  max-width: 460px;
  margin: 0 auto;
}

.claim-panel p {
  margin: 0;
}

.checkout-result .section-title {
  margin-bottom: 0;
}
</style>
