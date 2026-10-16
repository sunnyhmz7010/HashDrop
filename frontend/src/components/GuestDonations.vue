<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NPagination,
  NSpin,
  useDialog,
  useMessage,
} from "naive-ui";
import { api, errorText, money, paymentNames, type Donation } from "../api";
import {
  clearDonationHistory,
  donationHistory,
  donationsCleared,
  donationStorageError,
  rememberDonation,
} from "../donationHistory";
import { formatTime, t } from "../i18n";
const router = useRouter(),
  dialog = useDialog(),
  message = useMessage();
const page = ref(1),
  loading = ref(false),
  importing = ref(false),
  link = ref(""),
  error = ref(""),
  importError = ref("");
const items = computed(() =>
  donationHistory.value.slice((page.value - 1) * 20, page.value * 20),
);
let generation = 0,
  controller: AbortController | undefined,
  importController: AbortController | undefined;
function stop() {
  generation++;
  controller?.abort();
  importController?.abort();
  loading.value = importing.value = false;
}
async function load(next = page.value) {
  controller?.abort();
  const current = ++generation;
  controller = new AbortController();
  const signal = controller.signal;
  page.value = Math.min(
    Math.max(next, 1),
    Math.max(1, Math.ceil(donationHistory.value.length / 20)),
  );
  const records = [...items.value];
  loading.value = true;
  error.value = "";
  try {
    for (const record of records) {
      if (signal.aborted) return;
      const result = await api<Donation>(`/donations/${record.donation.id}`, {
        token: record.token,
        signal,
      });
      if (current !== generation) return;
      rememberDonation(result, record.token);
    }
  } catch (cause) {
    if (current === generation) error.value = errorText(cause);
  } finally {
    if (current === generation) loading.value = false;
  }
}
async function importLink() {
  if (importing.value) return;
  importing.value = true;
  importError.value = "";
  importController = new AbortController();
  const signal = importController.signal;
  try {
    const url = new URL(link.value.trim());
    const match =
      /^\/(?:donate\/orders|center\/donations)\/(don_[A-Za-z0-9_-]{22,64})$/.exec(
        url.pathname,
      );
    const token = new URLSearchParams(url.hash.slice(1)).get("token") ?? "";
    if (
      url.origin !== location.origin ||
      url.username ||
      url.password ||
      !match ||
      !/^[A-Za-z0-9_-]{43}$/.test(token)
    )
      throw new Error(t("center.donation_link_invalid"));
    const result = await api<Donation>(`/donations/${match[1]}`, {
      token,
      signal,
    });
    if (signal.aborted) return;
    rememberDonation(result, token);
    link.value = "";
    page.value = 1;
    await router.push(`/center/donations/${result.id}`);
  } catch (cause) {
    if (!signal.aborted) importError.value = errorText(cause);
  } finally {
    if (!signal.aborted) importing.value = false;
  }
}
function clear() {
  dialog.warning({
    title: t("center.clear_donations"),
    content: t("center.clear_donations_warning"),
    positiveText: t("center.clear_confirm"),
    negativeText: t("common.cancel"),
    onPositiveClick: () => {
      stop();
      if (!clearDonationHistory()) {
        message.error(t("center.clear_failed"));
        return false;
      }
      message.success(t("center.history_cleared"));
    },
  });
}
watch(donationsCleared, () => {
  stop();
  page.value = 1;
  error.value = importError.value = link.value = "";
});
onMounted(() => load());
onUnmounted(stop);
</script>
<template>
  <div class="grid personal-donations">
    <div class="section-title">
      <h2>{{ t("center.donations") }}</h2>
      <div class="row-actions">
        <n-button :loading="loading" @click="load()">{{
          t("common.refresh")
        }}</n-button>
        <n-button
          :disabled="loading || !donationHistory.length"
          @click="clear"
          >{{ t("center.clear_donations") }}</n-button
        >
      </div>
    </div>
    <p class="muted">{{ t("center.donations_help") }}</p>
    <n-alert v-if="donationStorageError" type="warning">{{
      t("center.donation_storage_error")
    }}</n-alert>
    <form class="panel grid" @submit.prevent="importLink">
      <label class="field-label" for="import-donation-link">{{
        t("center.import_donation")
      }}</label>
      <n-input
        v-model:value="link"
        :disabled="importing"
        :input-props="{ id: 'import-donation-link', autocomplete: 'off' }"
      />
      <n-alert v-if="importError" type="error">{{ importError }}</n-alert>
      <div class="form-actions">
        <n-button
          type="primary"
          attr-type="submit"
          :loading="importing"
          :disabled="!link.trim() || importing"
          >{{ t("common.import") }}</n-button
        >
      </div>
    </form>
    <n-alert v-if="error" type="warning"
      >{{ error
      }}<n-button text @click="load()">{{
        t("common.retry")
      }}</n-button></n-alert
    >
    <n-spin :show="loading"
      ><div class="grid">
        <article
          v-for="record in items"
          :key="record.donation.id"
          class="panel grid donation-history-row"
        >
          <n-button
            text
            type="primary"
            @click="router.push('/center/donations/' + record.donation.id)"
            >{{ record.donation.id }}</n-button
          >
          <strong>{{ record.donation.title }}</strong>
          <div class="row-actions">
            <span>{{
              money(record.donation.amount, record.donation.currency)
            }}</span
            ><span>{{ paymentNames[record.donation.payment_status] }}</span
            ><span>{{ formatTime(record.donation.created_at) }}</span>
          </div>
        </article>
        <n-empty
          v-if="!items.length && !loading && !error"
          :description="t('center.donations_empty')"
        /></div
    ></n-spin>
    <n-pagination
      v-if="donationHistory.length"
      :page="page"
      :page-size="20"
      :item-count="donationHistory.length"
      :page-slot="5"
      size="small"
      :disabled="loading"
      @update:page="(next) => load(next)"
    />
  </div>
</template>
<style scoped>
.section-title {
  flex-wrap: wrap;
}
.donation-history-row {
  overflow-wrap: anywhere;
}
.donation-history-row > .n-button {
  justify-self: start;
  max-width: 100%;
}
.donation-history-row :deep(.n-button__content) {
  white-space: normal;
  overflow-wrap: anywhere;
  text-align: left;
}
p {
  margin: 0;
}
</style>
