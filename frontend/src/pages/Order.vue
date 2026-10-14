<!-- 订单与领取页基于 HashPay Pay.vue 的凭单、付款结果和复制字段结构修改。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。
     支付状态直接展示网关核验结果，付款渠道和链上信息由 HashPay 页面提供。 -->
<script setup lang="ts">
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
  try {
    const url = new URL(claimLink.value.trim());
    const value = new URLSearchParams(url.hash.slice(1)).get("token");
    if (
      url.origin !== location.origin ||
      !/^\/orders\/[A-Za-z0-9_-]+$/.test(url.pathname) ||
      !value ||
      value.length !== 43
    )
      throw new Error("请填写本站完整领取链接");
    await router.push(url.pathname + url.hash);
  } catch (e) {
    message.error(errorText(e));
  }
}
async function copy(value: string) {
  try {
    await copyText(value);
    if (value === receipt.value) receiptSaved.value = true;
    message.success("已复制");
  } catch {
    message.warning("复制失败，请长按或选中文字复制");
  }
}
function pay() {
  if (storageError.value && !receiptSaved.value) return;
  if (detail.value?.order.checkout_url)
    location.assign(detail.value.order.checkout_url);
}
const expires = computed(() =>
  detail.value?.order.expires_at
    ? new Date(detail.value.order.expires_at * 1000).toLocaleString("zh-CN", {
        hour12: false,
      })
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
          <h1>{{ orderID ? "订单与领取" : "领取你的订单" }}</h1>
        </div>
        <p class="muted">领取链接包含私密凭证，请勿分享给他人。</p>
        <n-alert v-if="orderID" type="warning"
          >当前浏览器没有该订单的领取凭证，请粘贴已保存的完整链接。</n-alert
        >
        <div class="field-stack">
          <label class="field-label" for="claim-link">完整领取链接</label
          ><n-input
            id="claim-link"
            v-model:value="claimLink"
            placeholder="https://本站/orders/订单号#token=凭证"
            @keydown.enter="claim"
          />
        </div>
        <div class="form-actions">
          <n-button type="primary" @click="claim">查看订单</n-button>
        </div>
        <small class="field-help">凭证丢失后无法自动找回。</small>
      </section>
    </main>
    <main v-else class="checkout-page">
      <section class="checkout-receipt">
        <template v-if="detail">
          <div class="checkout-receipt-head">
            <div>
              <span>订单凭证</span
              ><strong>{{ detail.order.product_name }}</strong>
            </div>
            <n-tag :type="statusType">{{
              paymentNames[detail.order.payment_status]
            }}</n-tag>
          </div>
          <div class="checkout-amount-block">
            <span>订单金额</span
            ><strong>{{
              money(detail.order.total, detail.order.currency)
            }}</strong>
          </div>
          <dl class="checkout-receipt-meta">
            <div>
              <dt>订单号</dt>
              <dd>
                <code :title="detail.order.id">{{ detail.order.id }}</code>
                <n-button
                  size="tiny"
                  text
                  type="primary"
                  @click="copy(detail.order.id)"
                  >复制</n-button
                >
              </dd>
            </div>
            <div>
              <dt>订单信息</dt>
              <dd>
                {{ detail.order.product_name }} × {{ detail.order.quantity }}
              </dd>
            </div>
            <div>
              <dt>创建时间</dt>
              <dd>
                {{
                  new Date(detail.order.created_at * 1000).toLocaleString(
                    "zh-CN",
                    { hour12: false },
                  )
                }}
              </dd>
            </div>
            <div v-if="expires">
              <dt>支付有效期</dt>
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
        <p v-else class="muted">订单信息暂未加载</p>
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
              <strong>付款已确认</strong><span>HashPay 已确认付款。</span>
            </div>
          </div>
          <dl class="checkout-paid-meta">
            <div>
              <dt>交付状态</dt>
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
              ? "支付单已过期。若已经付款，请刷新状态等待 HashPay 核验。"
              : "该支付单已终止，请返回商城重新下单。"
          }}</span>
        </div>
        <div v-else class="checkout-flow-head">
          <span>订单与领取</span>
          <h1>先保存领取链接</h1>
          <small class="field-help"
            >付款后通过此链接领取；关闭浏览器后，请使用你保存的链接。</small
          >
        </div>
        <div class="checkout-copy-field">
          <span>领取链接</span>
          <code :title="receipt">{{ receipt }}</code>
          <n-button size="small" secondary @click="copy(receipt)"
            >复制链接</n-button
          >
        </div>
        <n-alert v-if="storageError" type="warning"
          >浏览器无法保存凭证，请复制领取链接，并在付款后通过该链接返回。</n-alert
        >
        <n-alert v-if="error" type="error"
          >{{ error }}
          <n-button text :loading="busy" @click="load"
            >重新查询</n-button
          ></n-alert
        >
        <template v-if="detail">
          <n-alert
            v-if="detail.order.payment_status === 'review'"
            type="warning"
            >网关订单暂未通过核验，库存仍为你保留。请勿重复付款。</n-alert
          >
          <n-alert
            v-else-if="detail.order.payment_status === 'creating'"
            type="info"
            >正在创建支付单，系统会自动重试。请保存领取链接并稍候。</n-alert
          >
          <n-alert
            v-else-if="
              detail.order.payment_status === 'paid' &&
              detail.order.delivery_status === 'manual'
            "
            type="warning"
            >付款已确认，交付需要人工处理，请联系店主并提供订单号。</n-alert
          >
          <n-checkbox v-if="storageError" v-model:checked="receiptSaved"
            >我已手动保存领取链接</n-checkbox
          >
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
            >前往 HashPay 付款</n-button
          >
          <div class="form-actions">
            <n-button :loading="busy" secondary size="small" @click="load"
              >刷新状态</n-button
            >
          </div>
          <div
            v-if="detail.deliveries.some((item) => item.status === 'done')"
            class="deliveries grid"
          >
            <div class="section-title"><h2>交付内容</h2></div>
            <article
              v-for="item in detail.deliveries.filter(
                (item) => item.status === 'done',
              )"
              :key="item.id"
              class="delivery-item checkout-result"
            >
              <div class="section-title">
                <span>第 {{ item.ordinal }} 份</span
                ><n-button size="small" @click="copy(item.content ?? '')"
                  >复制内容</n-button
                >
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
