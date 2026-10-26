<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from "vue";
import {
  NAlert,
  NButton,
  NInput,
  NSelect,
  NSkeleton,
  NSwitch,
  useMessage,
} from "naive-ui";
import {
  api,
  errorText,
  priceText,
  toMinor,
  type DonationSettings,
} from "../api";
import { t } from "../i18n";
import MarkdownEditor from "./MarkdownEditor.vue";

const message = useMessage(),
  loaded = ref(false),
  loadingSettings = ref(false),
  saving = ref(false);
const error = ref("");
const model = reactive({
  enabled: false,
  title: "",
  description: "",
  amounts: [] as { amount: string; currency: string }[],
  currencies: [] as {
    currency: string;
    min: string;
    max: string;
  }[],
});
const currencies = ["CNY", "USD", "EUR", "GBP", "HKD", "JPY", "USDT", "USDC"];
const currencyOptions = currencies.map((value) => ({ label: value, value }));
const controller = new AbortController();
function setModel(s: DonationSettings) {
  Object.assign(model, {
    ...s,
    amounts: s.currencies.flatMap((group) =>
      group.amounts.map((n) => ({
        amount: priceText(n, group.currency),
        currency: group.currency,
      })),
    ),
    currencies: s.currencies.map((group) => ({
      currency: group.currency,
      min: priceText(group.min_amount, group.currency),
      max: priceText(group.max_amount, group.currency),
    })),
  });
}
function addAmount() {
  model.amounts.push({
    amount: "",
    currency:
      model.amounts[model.amounts.length - 1]?.currency ??
      model.currencies[0]!.currency,
  });
}
function syncCurrencies() {
  for (const { currency } of model.amounts) {
    if (!model.currencies.some((group) => group.currency === currency))
      model.currencies.push({
        currency,
        min: "1",
        max: "1000",
      });
  }
}
function removeCurrency(index: number) {
  const currency = model.currencies[index]!.currency;
  model.amounts = model.amounts.filter(
    (amount) => amount.currency !== currency,
  );
  model.currencies.splice(index, 1);
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
onMounted(loadSettings);
onUnmounted(() => controller.abort());
async function save() {
  if (saving.value || !loaded.value) return;
  saving.value = true;
  error.value = "";
  try {
    syncCurrencies();
    // 按币种提交金额，独立保留上下限以支持仅自定义金额的币种。
    const amounts = model.amounts.map(({ amount, currency }) => ({
      currency,
      value: toMinor(amount, currency),
    }));
    const body: DonationSettings = {
      enabled: model.enabled,
      title: model.title,
      description: model.description,
      currencies: model.currencies.map((group) => ({
        currency: group.currency,
        amounts: amounts
          .filter((amount) => amount.currency === group.currency)
          .map((amount) => amount.value),
        min_amount: toMinor(group.min, group.currency),
        max_amount: toMinor(group.max, group.currency),
      })),
    };
    const result = await api<DonationSettings>("/admin/donation", {
      method: "PUT",
      body,
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
</script>

<template>
  <div class="grid">
    <div class="section-title">
      <h2>{{ t("nav.donation") }}</h2>
      <n-button tag="a" href="/donate" target="_blank" rel="noopener">{{
        t("donation.open_page")
      }}</n-button>
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
        <label class="field-label" for="donation-description-input">{{
          t("donation.description")
        }}</label
        ><MarkdownEditor
          id="donation-description"
          input-id="donation-description-input"
          v-model="model.description"
          :disabled="saving"
          :maxlength="65536"
        />
      </div>
      <div class="field-stack">
        <span class="field-label">{{ t("donation.presets") }}</span>
        <div
          v-for="(preset, index) in model.amounts"
          :key="index"
          class="donation-preset"
        >
          <n-input
            v-model:value="preset.amount"
            :disabled="saving"
            placeholder="5"
            :input-props="{
              inputmode: 'decimal',
              'aria-label': `${t('donation.presets')} ${index + 1}`,
            }"
          >
            <template #suffix>
              <div class="donation-preset-currency" @mousedown.stop>
                <n-select
                  v-model:value="preset.currency"
                  size="small"
                  :bordered="false"
                  :consistent-menu-width="false"
                  :disabled="saving"
                  :options="currencyOptions"
                  :aria-label="`${t('donation.currency')} ${index + 1}`"
                  @update:value="syncCurrencies"
                />
              </div>
            </template>
          </n-input>
          <n-button
            size="small"
            quaternary
            circle
            :aria-label="t('donation.remove_amount')"
            :title="t('donation.remove_amount')"
            :disabled="saving"
            @click="model.amounts.splice(index, 1)"
          >
            ×
          </n-button>
        </div>
        <n-button
          class="add-amount"
          :disabled="saving || model.amounts.length >= currencies.length * 8"
          @click="addAmount"
          >{{ t("donation.add_amount") }}</n-button
        >
        <small class="field-help">{{ t("donation.presets_help") }}</small>
      </div>
      <h3 class="amount-limits-title">{{ t("donation.amount_limits") }}</h3>
      <section
        v-for="(group, index) in model.currencies"
        :key="group.currency"
        class="grid donation-currency-range"
      >
        <div class="section-title">
          <span class="field-label">{{ group.currency }}</span>
          <n-button
            :disabled="saving || model.currencies.length <= 1"
            @click="removeCurrency(index)"
            >{{ t("donation.remove_currency") }}</n-button
          >
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
    </form>
  </div>
</template>

<style scoped lang="scss">
.donation-switch {
  justify-self: start;
}
.amount-limits-title {
  margin: 0;
}
.donation-currency-range {
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
.donation-preset-currency {
  width: 96px;
}
.add-amount {
  justify-self: start;
}
</style>
