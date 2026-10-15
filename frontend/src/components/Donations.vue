<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NSelect,
  NSkeleton,
  NSwitch,
  NTag,
  useMessage,
} from "naive-ui";
import {
  api,
  copyText,
  errorText,
  money,
  paymentNames,
  priceText,
  toMinor,
  type DonationList,
  type DonationSettings,
} from "../api";
import { formatTime, t } from "../i18n";

const message = useMessage(),
  loaded = ref(false),
  loadingSettings = ref(false),
  saving = ref(false);
const error = ref(""),
  recordsError = ref(""),
  loadingRecords = ref(false),
  checking = ref("");
const records = ref<DonationList>({
  items: [],
  page: 1,
  has_more: false,
  totals: [],
});
const model = reactive({
  enabled: false,
  title: "",
  description: "",
  currency: "CNY",
  amounts: "",
  min: "",
  max: "",
});
const currencies = [
  "CNY",
  "USD",
  "EUR",
  "GBP",
  "HKD",
  "JPY",
  "USDT",
  "USDC",
].map((value) => ({ label: value, value }));
const publicURL = computed(() => `${location.origin}/donate`);
const controller = new AbortController();
function setModel(s: DonationSettings) {
  Object.assign(model, {
    ...s,
    amounts: s.amounts.map((n) => priceText(n, s.currency)).join(", "),
    min: priceText(s.min_amount, s.currency),
    max: priceText(s.max_amount, s.currency),
  });
}
async function loadSettings() {
  loadingSettings.value = true;
  error.value = "";
  try {
    const result = await api<DonationSettings>("/admin/donation", {
      signal: controller.signal,
    });
    setModel(result);
    loaded.value = true;
  } catch (e) {
    if (!controller.signal.aborted) error.value = errorText(e);
  } finally {
    loadingSettings.value = false;
  }
}
async function loadRecords(page = records.value.page) {
  if (loadingRecords.value) return;
  loadingRecords.value = true;
  recordsError.value = "";
  try {
    records.value = await api<DonationList>(`/admin/donations?page=${page}`, {
      signal: controller.signal,
    });
  } catch (e) {
    if (!controller.signal.aborted) recordsError.value = errorText(e);
  } finally {
    loadingRecords.value = false;
  }
}
onMounted(() => {
  void loadSettings();
  void loadRecords();
});
onUnmounted(() => controller.abort());
async function save() {
  if (saving.value || !loaded.value) return;
  saving.value = true;
  error.value = "";
  try {
    const body: DonationSettings = {
      enabled: model.enabled,
      title: model.title,
      description: model.description,
      currency: model.currency,
      amounts: model.amounts.trim()
        ? model.amounts
            .trim()
            .split(/[,，\s]+/)
            .map((value) => toMinor(value, model.currency))
        : [],
      min_amount: toMinor(model.min, model.currency),
      max_amount: toMinor(model.max, model.currency),
    };
    await api("/admin/donation", {
      method: "PUT",
      body,
      signal: controller.signal,
    });
    const result = await api<DonationSettings>("/admin/donation", {
      signal: controller.signal,
    });
    setModel(result);
    message.success(t("donation.settings_saved"));
  } catch (e) {
    if (!controller.signal.aborted) error.value = errorText(e);
  } finally {
    saving.value = false;
  }
}
async function copyURL() {
  try {
    await copyText(publicURL.value);
    message.success(t("common.copied"));
  } catch {
    message.warning(t("common.copy_manual"));
  }
}
async function recheck(id: string) {
  if (checking.value || loadingRecords.value) return;
  checking.value = id;
  recordsError.value = "";
  try {
    await api(`/admin/donations/${id}/recheck`, {
      method: "POST",
      body: {},
      signal: controller.signal,
    });
    await loadRecords();
  } catch (e) {
    if (!controller.signal.aborted) recordsError.value = errorText(e);
  } finally {
    checking.value = "";
  }
}
</script>

<template>
  <div class="grid">
    <div class="section-title">
      <h2>{{ t("nav.donation") }}</h2>
      <div class="row-actions">
        <router-link to="/donate" target="_blank" class="text-link">{{
          t("donation.open_page")
        }}</router-link
        ><n-button @click="copyURL">{{ t("donation.copy_page") }}</n-button>
      </div>
    </div>
    <n-alert v-if="error" type="error"
      >{{ errorText(error) }}
      <n-button
        v-if="!loaded"
        text
        :loading="loadingSettings"
        @click="loadSettings"
        >{{ t("common.retry") }}</n-button
      ></n-alert
    >
    <section
      v-if="loadingSettings && !loaded"
      class="panel grid"
      aria-busy="true"
    >
      <n-skeleton text width="30%" /><n-skeleton height="40px" :repeat="3" />
    </section>
    <form v-if="loaded" class="panel grid" @submit.prevent="save">
      <div class="section-title">
        <h2>{{ t("donation.configuration") }}</h2>
        <n-button type="primary" attr-type="submit" :loading="saving">{{
          t("settings.save")
        }}</n-button>
      </div>
      <div class="field-stack">
        <label class="field-label" id="donation-enabled-label">{{
          t("donation.enabled")
        }}</label
        ><n-switch
          v-model:value="model.enabled"
          class="donation-switch"
          :disabled="saving"
          aria-labelledby="donation-enabled-label"
        />
      </div>
      <div class="form-grid two">
        <div class="field-stack">
          <label class="field-label" for="donation-title">{{
            t("donation.title")
          }}</label
          ><n-input
            v-model:value="model.title"
            :disabled="saving"
            :maxlength="120"
            :input-props="{ id: 'donation-title' }"
          />
        </div>
        <div class="field-stack">
          <label class="field-label" id="donation-currency-label">{{
            t("donation.currency")
          }}</label
          ><n-select
            v-model:value="model.currency"
            :disabled="saving"
            :options="currencies"
            aria-labelledby="donation-currency-label"
          />
        </div>
      </div>
      <div class="field-stack">
        <label class="field-label" for="donation-description">{{
          t("donation.description")
        }}</label
        ><n-input
          v-model:value="model.description"
          :disabled="saving"
          type="textarea"
          :maxlength="65536"
          :autosize="{ minRows: 3, maxRows: 8 }"
          :input-props="{ id: 'donation-description' }"
        />
      </div>
      <div class="field-stack">
        <label class="field-label" for="donation-presets"
          >{{ t("donation.presets") }} · {{ model.currency }}</label
        ><n-input
          v-model:value="model.amounts"
          :disabled="saving"
          :input-props="{ id: 'donation-presets' }"
        /><small class="field-help">{{ t("donation.presets_help") }}</small>
      </div>
      <div class="form-grid two">
        <div class="field-stack">
          <label class="field-label" for="donation-min"
            >{{ t("donation.min") }} · {{ model.currency }}</label
          ><n-input
            v-model:value="model.min"
            :disabled="saving"
            :input-props="{ id: 'donation-min', inputmode: 'decimal' }"
          />
        </div>
        <div class="field-stack">
          <label class="field-label" for="donation-max"
            >{{ t("donation.max") }} · {{ model.currency }}</label
          ><n-input
            v-model:value="model.max"
            :disabled="saving"
            :input-props="{ id: 'donation-max', inputmode: 'decimal' }"
          />
        </div>
      </div>
      <p class="helper">
        {{ t("donation.gateway_help") }}
        <router-link to="/admin/settings" class="text-link">{{
          t("nav.settings")
        }}</router-link>
      </p>
    </form>
    <section class="panel grid">
      <div class="section-title">
        <h2>{{ t("donation.records") }}</h2>
        <n-button
          :loading="loadingRecords"
          :disabled="!!checking"
          @click="loadRecords()"
          >{{ t("orders.refresh_list") }}</n-button
        >
      </div>
      <n-alert v-if="recordsError" type="error"
        >{{ errorText(recordsError) }}
        <n-button text :disabled="!!checking" @click="loadRecords()">{{
          t("common.retry")
        }}</n-button></n-alert
      >
      <div v-if="records.totals.length" class="donation-totals">
        <span>{{ t("donation.verified_total") }}</span
        ><span v-for="total in records.totals" :key="total.currency"
          ><strong>{{ money(total.amount, total.currency) }}</strong> ·
          {{ t("donation.count", { count: total.count }) }}</span
        >
      </div>
      <template v-if="loadingRecords && !records.items.length"
        ><n-skeleton text :repeat="3"
      /></template>
      <n-empty
        v-else-if="!records.items.length && !recordsError"
        :description="t('donation.no_records')"
      />
      <div
        v-for="d in records.items"
        :key="d.id"
        class="donation-record"
        :aria-label="d.id"
      >
        <div class="grid donation-identity">
          <strong>{{ d.title }}</strong
          ><span class="code-cell">{{ d.id }}</span
          ><small class="muted">{{ formatTime(d.created_at) }}</small
          ><small v-if="d.last_error" class="helper">{{
            errorText(d.last_error)
          }}</small>
        </div>
        <div class="grid donation-status">
          <strong>{{ money(d.amount, d.currency) }}</strong
          ><n-tag
            :type="d.payment_status === 'paid' ? 'success' : 'default'"
            :bordered="false"
            >{{ paymentNames[d.payment_status] }}</n-tag
          >
        </div>
        <n-button
          :loading="checking === d.id"
          :disabled="!!checking || loadingRecords"
          @click="recheck(d.id)"
          >{{ t("orders.refresh_status") }}</n-button
        >
      </div>
      <div v-if="records.page > 1 || records.has_more" class="form-actions">
        <n-button
          :disabled="records.page <= 1 || loadingRecords || !!checking"
          @click="loadRecords(records.page - 1)"
          >{{ t("pagination.previous") }}</n-button
        ><span>{{ t("pagination.page", { page: records.page }) }}</span
        ><n-button
          :disabled="!records.has_more || loadingRecords || !!checking"
          @click="loadRecords(records.page + 1)"
          >{{ t("pagination.next") }}</n-button
        >
      </div>
    </section>
  </div>
</template>

<style scoped lang="scss">
.donation-switch {
  justify-self: start;
}
.helper {
  margin: 0;
}
.donation-totals {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  font-size: 13px;
}
.donation-record {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 14px;
  padding: 14px 0;
  border-top: 1px solid #e5e7eb;
}
.donation-identity,
.donation-status {
  min-width: 0;
  gap: 6px;
}
.donation-identity strong {
  overflow-wrap: anywhere;
}
.donation-identity span {
  font-size: 12px;
}
@media (max-width: 820px) {
  .donation-record {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .donation-record > .n-button {
    grid-column: 1 / -1;
    justify-self: start;
  }
}
</style>
