<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NInput, useDialog, useMessage } from "naive-ui";
import { api, errorText, type Donation } from "../api";
import {
  clearDonationHistory,
  donationHistory,
  donationsCleared,
  donationStorageError,
  rememberDonation,
} from "../donationHistory";
import { t } from "../i18n";
import FormDialog from "./FormDialog.vue";
import HistoryList from "./HistoryList.vue";
import HistoryRow from "./HistoryRow.vue";
const router = useRouter(),
  dialog = useDialog(),
  message = useMessage();
const page = ref(1),
  queryOpen = ref(false),
  loading = ref(false),
  importing = ref(false),
  nickname = ref(""),
  error = ref(""),
  importError = ref("");
const items = computed(() =>
  donationHistory.value
    .slice((page.value - 1) * 20, page.value * 20)
    .map((record) => record.donation),
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
  const records = donationHistory.value.slice(
    (page.value - 1) * 20,
    page.value * 20,
  );
  loading.value = true;
  error.value = "";
  let failures = 0;
  let lastError: unknown;
  try {
    for (const record of records) {
      if (signal.aborted) return;
      try {
        const result = await api<Donation>(`/donations/${record.donation.id}`, {
          token: record.token,
          signal,
        });
        if (current !== generation) return;
        rememberDonation(result, record.token);
      } catch (cause) {
        if (signal.aborted || current !== generation) return;
        failures++;
        lastError = cause;
      }
    }
    if (failures && current === generation)
      error.value = t("center.donation_refresh_error", {
        count: failures,
        error: errorText(lastError),
      });
  } finally {
    if (current === generation) loading.value = false;
  }
}
async function lookup() {
  if (importing.value) return;
  importing.value = true;
  importError.value = "";
  importController = new AbortController();
  const signal = importController.signal;
  try {
    const name = nickname.value.trim();
    if (!name || Array.from(name).length > 128)
      throw new Error(t("errors.donation_nickname"));
    const result = await api<Donation>("/donations/lookup", {
      method: "POST",
      body: { nickname: name },
      signal,
    });
    if (signal.aborted) return;
    rememberDonation(result, result.nickname);
    nickname.value = "";
    page.value = 1;
    queryOpen.value = false;
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
  queryOpen.value = false;
  page.value = 1;
  error.value = importError.value = nickname.value = "";
});
watch(queryOpen, () => {
  importController?.abort();
  importing.value = false;
  nickname.value = importError.value = "";
});
onMounted(() => load());
onUnmounted(stop);
</script>
<template>
  <HistoryList
    class="personal-donations"
    :title="t('center.donations')"
    :items="items"
    :columns="[
      t('center.donation_column'),
      t('orders.payment_status'),
      t('common.amount'),
      t('donation.nickname'),
      t('common.created_at'),
    ]"
    :loading="loading"
    :error="error"
    :empty-text="t('center.donations_empty')"
    :clear-text="t('center.clear_donations')"
    :total="donationHistory.length"
    :total-text="t('center.donations_total', { total: donationHistory.length })"
    :page="page"
    @refresh="load()"
    @query="queryOpen = true"
    @clear="clear"
    @update:page="load"
  >
    <template #notice>
      <n-alert v-if="donationStorageError" type="warning">{{
        t("center.donation_storage_error")
      }}</n-alert>
    </template>
    <template #row="{ item }">
      <HistoryRow
        class="donation-history-row"
        :id="item.id"
        :title="item.title"
        :payment-status="item.payment_status"
        :amount="item.amount"
        :currency="item.currency"
        :created-at="item.created_at"
        @open="router.push('/center/donations/' + item.id)"
      >
        <strong :title="item.nickname">{{ item.nickname || "—" }}</strong>
      </HistoryRow>
    </template>
  </HistoryList>
  <FormDialog
    v-model:show="queryOpen"
    :title="t('center.donation_query')"
    :busy="importing"
    width="min(440px, calc(100vw - 28px))"
  >
    <form id="donation-query-form" class="form-grid" @submit.prevent="lookup">
      <div class="field-stack">
        <label class="field-label" for="donation-query-nickname">{{
          t("donation.nickname")
        }}</label>
        <n-input
          v-model:value="nickname"
          :disabled="importing"
          :input-props="{
            id: 'donation-query-nickname',
            autocomplete: 'nickname',
          }"
        />
      </div>
      <n-alert v-if="importError" type="error">{{ importError }}</n-alert>
    </form>
    <template #footer>
      <n-button :disabled="importing" @click="queryOpen = false">{{
        t("common.cancel")
      }}</n-button>
      <n-button
        type="primary"
        attr-type="submit"
        form="donation-query-form"
        :loading="importing"
        :disabled="!nickname.trim() || importing"
        >{{ t("common.query") }}</n-button
      >
    </template>
  </FormDialog>
</template>
