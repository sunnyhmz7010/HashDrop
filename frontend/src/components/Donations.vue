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
  currencies: [] as {
    currency: string;
    amounts: string[];
    min: string;
    max: string;
  }[],
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
    currencies: s.currencies.map((group) => ({
      currency: group.currency,
      amounts: group.amounts.map((n) => priceText(n, group.currency)),
      min: priceText(group.min_amount, group.currency),
      max: priceText(group.max_amount, group.currency),
    })),
  });
}
function addCurrency() {
  const next = currencies.find(
    (option) =>
      !model.currencies.some((group) => group.currency === option.value),
  );
  if (next)
    model.currencies.push({
      currency: next.value,
      amounts: ["5", "10", "20"],
      min: "1",
      max: "1000",
    });
}
function currencyOptions(index: number) {
  return currencies.map((option) => ({
    ...option,
    disabled: model.currencies.some(
      (group, other) => other !== index && group.currency === option.value,
    ),
  }));
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
      currencies: model.currencies.map((group) => ({
        currency: group.currency,
        amounts: group.amounts.map((value) => toMinor(value, group.currency)),
        min_amount: toMinor(group.min, group.currency),
        max_amount: toMinor(group.max, group.currency),
      })),
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
      <section
        v-for="(group, index) in model.currencies"
        :key="index"
        class="grid donation-currency-group"
      >
        <div class="section-title">
          <label class="field-label" :id="`donation-currency-label-${index}`">{{
            t("donation.currency")
          }}</label>
          <n-button
            :disabled="saving || model.currencies.length <= 1"
            @click="model.currencies.splice(index, 1)"
            >{{ t("donation.remove_currency") }}</n-button
          >
        </div>
        <n-select
          v-model:value="group.currency"
          :disabled="saving"
          :options="currencyOptions(index)"
          :aria-labelledby="`donation-currency-label-${index}`"
        />
        <div class="field-stack">
          <span class="field-label"
            >{{ t("donation.presets") }} · {{ group.currency }}</span
          >
          <div
            v-for="(_, amountIndex) in group.amounts"
            :key="amountIndex"
            class="donation-preset"
          >
            <n-input
              v-model:value="group.amounts[amountIndex]"
              :disabled="saving"
              :input-props="{
                inputmode: 'decimal',
                'aria-label': `${t('donation.presets')} ${amountIndex + 1} · ${group.currency}`,
              }"
            />
            <n-button
              :disabled="saving"
              @click="group.amounts.splice(amountIndex, 1)"
              >{{ t("donation.remove_amount") }}</n-button
            >
          </div>
          <n-button
            class="add-amount"
            :disabled="saving || group.amounts.length >= 8"
            @click="group.amounts.push('')"
            >{{ t("donation.add_amount") }}</n-button
          >
          <small class="field-help">{{ t("donation.presets_help") }}</small>
        </div>
        <div class="form-grid two">
          <div class="field-stack">
            <label class="field-label" :for="`donation-min-${index}`"
              >{{ t("donation.min") }} · {{ group.currency }}</label
            ><n-input
              v-model:value="group.min"
              :disabled="saving"
              :input-props="{
                id: `donation-min-${index}`,
                inputmode: 'decimal',
              }"
            />
          </div>
          <div class="field-stack">
            <label class="field-label" :for="`donation-max-${index}`"
              >{{ t("donation.max") }} · {{ group.currency }}</label
            ><n-input
              v-model:value="group.max"
              :disabled="saving"
              :input-props="{
                id: `donation-max-${index}`,
                inputmode: 'decimal',
              }"
            />
          </div>
        </div>
      </section>
      <n-button
        class="add-currency"
        :disabled="saving || model.currencies.length >= currencies.length"
        @click="addCurrency"
        >{{ t("donation.add_currency") }}</n-button
      >
      <p class="helper">
        {{ t("donation.gateway_help") }}
        <router-link to="/center/settings" class="text-link">{{
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
          ><span v-if="d.nickname"
            >{{ d.nickname }} ·
            {{
              d.show_public ? t("donation.public_yes") : t("donation.public_no")
            }}</span
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
.donation-currency-group {
  padding: 16px 0;
  border-top: 1px solid #e5e7eb;
}
.donation-preset {
  display: flex;
  align-items: center;
  gap: 8px;
}
.donation-preset > .n-input {
  flex: 1;
  min-width: 0;
}
.add-amount,
.add-currency {
  justify-self: start;
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
.donation-identity strong,
.donation-identity span {
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
