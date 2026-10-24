<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NAlert, NButton, NSkeleton, NTag } from "naive-ui";
import { api, errorText, money, paymentNames, type Donation } from "../api";
import { formatTime, t } from "../i18n";
import {
  donationNickname,
  donationsCleared,
  rememberDonation,
} from "../donationHistory";

const route = useRoute(),
  router = useRouter(),
  detail = ref<Donation>();
const id = ref(""),
  token = ref(""),
  error = ref(""),
  busy = ref(false);
const storageError = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined,
  controller: AbortController | undefined,
  generation = 0;
const statusType = computed(() =>
  detail.value?.payment_status === "paid"
    ? "success"
    : ["expired", "review"].includes(detail.value?.payment_status ?? "")
      ? "warning"
      : "default",
);
const statusHelp = computed(() => {
  switch (detail.value?.payment_status) {
    case "creating":
      return t("donation.creating_help");
    case "closed":
    case "invalid":
      return t("donation.closed_help");
    case "expired":
      return t("donation.expired_help");
    case "review":
      return t("donation.review_help");
    default:
      return t("donation.waiting_help");
  }
});
function stop() {
  generation++;
  clearTimeout(timer);
  controller?.abort();
  busy.value = false;
}
async function load() {
  if (busy.value || !id.value || !token.value) return;
  const current = generation;
  clearTimeout(timer);
  controller = new AbortController();
  busy.value = true;
  try {
    const result = await api<Donation>(`/donations/${id.value}`, {
      token: token.value,
      signal: controller.signal,
    });
    if (current !== generation) return;
    detail.value = result;
    storageError.value = !rememberDonation(result);
    error.value = "";
    if (!["paid", "closed", "invalid"].includes(result.payment_status))
      timer = setTimeout(load, 6000);
  } catch (e) {
    if (current === generation) error.value = errorText(e);
  } finally {
    if (current === generation) busy.value = false;
  }
}
watch(
  () => route.params.id,
  () => {
    stop();
    detail.value = undefined;
    error.value = "";
    storageError.value = false;
    id.value = String(route.params.id ?? "");
    try {
      token.value =
        donationNickname(id.value) ||
        sessionStorage.getItem(`hashdrop:donation:${id.value}`) ||
        "";
    } catch {
      token.value = donationNickname(id.value);
      storageError.value = true;
    }
    void load();
  },
  { immediate: true },
);
onUnmounted(stop);
watch(donationsCleared, (ids) => {
  if (!ids.includes(id.value)) return;
  stop();
  detail.value = undefined;
  token.value = "";
  error.value = "";
  void router.replace("/center/donations");
});
function pay() {
  if (detail.value?.payment_status === "pending" && detail.value.checkout_url)
    location.assign(detail.value.checkout_url);
}
</script>

<template>
  <div>
    <main class="donation-receipt grid">
      <n-button secondary @click="router.push('/center/donations')">{{
        t("center.back_donations")
      }}</n-button>
      <section class="panel grid">
        <div class="section-title">
          <h1>
            {{
              detail?.payment_status === "paid"
                ? t("donation.thanks")
                : t("donation.receipt_title")
            }}
          </h1>
          <n-tag v-if="detail" :type="statusType" :bordered="false">{{
            paymentNames[detail.payment_status]
          }}</n-tag>
        </div>
        <n-alert v-if="!token" type="warning"
          >{{ t("donation.missing_nickname") }}
          <router-link to="/center/donations" class="text-link">{{
            t("donation.lookup")
          }}</router-link></n-alert
        >
        <n-alert v-if="error" type="error"
          >{{ errorText(error) }}
          <n-button text :loading="busy" @click="load">{{
            t("common.retry")
          }}</n-button></n-alert
        >
        <template v-if="busy && !detail"
          ><n-skeleton text :repeat="3"
        /></template>
        <template v-if="detail">
          <p>{{ detail.title }}</p>
          <strong class="donation-total">{{
            money(detail.amount, detail.currency)
          }}</strong>
          <dl class="donation-meta">
            <template v-if="detail.nickname">
              <dt>{{ t("donation.nickname") }}</dt>
              <dd>{{ detail.nickname }}</dd>
              <dt>{{ t("donation.public_visibility") }}</dt>
              <dd>
                {{
                  detail.show_public
                    ? t("donation.public_yes")
                    : t("donation.public_no")
                }}
              </dd>
            </template>
            <dt>{{ t("orders.id") }}</dt>
            <dd>{{ detail.id }}</dd>
            <dt>{{ t("common.created_at") }}</dt>
            <dd>{{ formatTime(detail.created_at) }}</dd>
            <template v-if="detail.paid_at"
              ><dt>{{ t("donation.paid_at") }}</dt>
              <dd>{{ formatTime(detail.paid_at) }}</dd></template
            >
            <template v-else-if="detail.expires_at"
              ><dt>{{ t("orders.deadline") }}</dt>
              <dd>{{ formatTime(detail.expires_at) }}</dd></template
            >
          </dl>
          <n-alert v-if="detail.payment_status === 'paid'" type="success">{{
            t("donation.paid_help")
          }}</n-alert>
          <p v-else class="helper">{{ statusHelp }}</p>
          <n-alert v-if="detail.last_error" type="warning">{{
            errorText(detail.last_error)
          }}</n-alert>
        </template>
        <n-alert v-if="storageError && detail" type="warning">{{
          t("donation.storage_help")
        }}</n-alert>
        <div class="form-actions">
          <n-button
            v-if="detail?.payment_status === 'pending' && detail.checkout_url"
            type="primary"
            @click="pay"
            >{{ t("donation.pay") }}</n-button
          >
          <n-button v-if="token" :loading="busy" @click="load">{{
            t("common.refresh")
          }}</n-button>
          <router-link to="/donate" class="text-link">{{
            t("donation.back")
          }}</router-link>
        </div>
      </section>
    </main>
  </div>
</template>

<style scoped lang="scss">
.donation-receipt {
  max-width: 100%;
}
.donation-total {
  font-size: 24px;
}
.donation-meta {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 12px;
  margin: 0;
  font-size: 13px;
}
dt {
  color: #6b7280;
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
p {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
