<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import {
  NAlert,
  NButton,
  NCheckbox,
  NEmpty,
  NInput,
  NSelect,
  NSkeleton,
} from "naive-ui";
import StoreLayout from "../components/StoreLayout.vue";
import MarkdownContent from "../components/MarkdownContent.vue";
import FormDialog from "../components/FormDialog.vue";
import {
  api,
  APIError,
  errorText,
  money,
  priceText,
  toMinor,
  type Donation,
  type DonationSettings,
  type PublicDonationList,
} from "../api";
import { newPurchaseID } from "../claim";
import { formatTime, t } from "../i18n";
import {
  forgetDonationIntent,
  rememberDonation,
  rememberDonationIntent,
  restoreDonationIntent,
  type DonationIntent,
} from "../donationHistory";

const router = useRouter();
const settings = ref<DonationSettings>();
const intent = ref<DonationIntent | undefined>(restoreDonationIntent());
const nickname = ref(intent.value?.nickname ?? "");
const showPublic = ref(intent.value?.show_public ?? false);
const currency = ref(intent.value?.currency ?? "");
const amount = ref(
  intent.value ? priceText(intent.value.amount, intent.value.currency) : "",
);
const loading = ref(true),
  busy = ref(false),
  error = ref(""),
  receiptPath = ref("");
const suggestions = ref<string[]>([]),
  storageError = ref(false),
  repeatOpen = ref(false);
const publicRecords = ref<PublicDonationList>({
  items: [],
  page: 1,
  has_more: false,
});
const publicLoading = ref(false),
  publicError = ref("");
const controller = new AbortController();
const locked = computed(
  () => busy.value || !!intent.value || !!receiptPath.value,
);
const group = computed(() =>
  settings.value?.currencies?.find((item) => item.currency === currency.value),
);
const currencyOptions = computed(() => {
  const values = settings.value?.currencies?.map((item) => item.currency) ?? [];
  if (intent.value && !values.includes(intent.value.currency))
    values.push(intent.value.currency);
  return values.map((value) => ({ label: value, value }));
});
function chooseCurrency(value: string) {
  currency.value = value;
  const selected = settings.value?.currencies.find(
    (item) => item.currency === value,
  );
  if (selected)
    amount.value = priceText(selected.amounts[0] ?? selected.min_amount, value);
}
async function load() {
  if (busy.value) return;
  loading.value = true;
  error.value = "";
  try {
    settings.value = await api<DonationSettings>("/donation", {
      signal: controller.signal,
    });
    if (settings.value.enabled && !intent.value && !receiptPath.value)
      chooseCurrency(
        currency.value &&
          settings.value.currencies.some(
            (item) => item.currency === currency.value,
          )
          ? currency.value
          : settings.value.currencies[0]!.currency,
      );
  } catch (cause) {
    if (!controller.signal.aborted) error.value = errorText(cause);
  } finally {
    loading.value = false;
  }
}
async function loadPublic(page = publicRecords.value.page) {
  if (publicLoading.value) return;
  publicLoading.value = true;
  publicError.value = "";
  try {
    const result = await api<PublicDonationList>(
      `/donations/public?page=${page}`,
      { signal: controller.signal },
    );
    if (!controller.signal.aborted) publicRecords.value = result;
  } catch (cause) {
    if (!controller.signal.aborted) publicError.value = errorText(cause);
  } finally {
    publicLoading.value = false;
  }
}
onMounted(() => {
  void load();
  void loadPublic();
});
onUnmounted(() => controller.abort());
function chooseNickname(value: string) {
  nickname.value = value;
  suggestions.value = [];
  error.value = "";
}
watch(repeatOpen, (open) => {
  if (!open && !busy.value) {
    forgetDonationIntent();
    intent.value = undefined;
  }
});
function confirmRepeat() {
  if (!intent.value || busy.value) return;
  intent.value = { ...intent.value, confirm_repeat: true };
  storageError.value = !rememberDonationIntent(intent.value);
  void submit();
  repeatOpen.value = false;
}
async function submit() {
  if (busy.value) return;
  if (receiptPath.value) {
    await router.push(receiptPath.value);
    return;
  }
  busy.value = true;
  error.value = "";
  suggestions.value = [];
  try {
    if (!intent.value) {
      const name = nickname.value.trim();
      if (!name || Array.from(name).length > 128)
        throw new Error(t("errors.donation_nickname"));
      if (!settings.value?.enabled || !group.value)
        throw new Error(t("errors.donation_changed"));
      const value = toMinor(amount.value, currency.value);
      if (value < group.value.min_amount || value > group.value.max_amount)
        throw new Error(t("errors.donation_amount"));
      nickname.value = name;
      // 请求发出前固定快照；未知结果和页面刷新都继续同一笔赞赏。
      intent.value = {
        request_id: newPurchaseID(),
        nickname: name,
        show_public: showPublic.value,
        amount: value,
        currency: currency.value,
      };
      storageError.value = !rememberDonationIntent(intent.value);
    }
    const result = await api<{ donation: Donation }>("/donations", {
      method: "POST",
      body: intent.value,
      signal: controller.signal,
    });
    if (controller.signal.aborted) return;
    rememberDonation(result.donation);
    receiptPath.value = `/center/donations/${result.donation.id}`;
    forgetDonationIntent();
    intent.value = undefined;
    await router.push(receiptPath.value);
  } catch (cause) {
    if (controller.signal.aborted) return;
    if (
      cause instanceof APIError &&
      cause.code === "donation_repeat_confirmation_required"
    ) {
      repeatOpen.value = true;
      suggestions.value = cause.suggestions;
      return;
    }
    // 明确业务拒绝允许修改输入；网络和服务端未知结果仍复用原请求。
    if (cause instanceof APIError && [400, 404, 409].includes(cause.status)) {
      forgetDonationIntent();
      intent.value = undefined;
    }
    error.value = errorText(cause);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <StoreLayout>
    <main class="page donate-page grid">
      <n-alert v-if="error" type="error"
        >{{ errorText(error) }}
        <n-button text :disabled="busy" @click="load">{{
          t("common.refresh")
        }}</n-button></n-alert
      >
      <section
        v-if="loading && !settings && !intent"
        class="panel grid"
        aria-busy="true"
      >
        <n-skeleton text width="50%" /><n-skeleton
          text
          :repeat="2"
        /><n-skeleton height="40px" />
      </section>
      <section
        v-else-if="settings && !settings.enabled && !intent"
        class="panel grid"
      >
        <h1>{{ t("nav.donation") }}</h1>
        <p class="muted">{{ t("donation.unavailable") }}</p>
      </section>
      <form
        v-else-if="settings?.enabled || intent"
        class="panel grid"
        @submit.prevent="submit"
      >
        <div class="section-title">
          <h1>{{ settings?.title || t("nav.donation") }}</h1>
        </div>
        <MarkdownContent
          v-if="settings?.description"
          :content="settings.description"
        />
        <div class="field-stack">
          <label class="field-label" for="donate-nickname">{{
            t("donation.nickname")
          }}</label>
          <n-input
            v-model:value="nickname"
            :disabled="locked"
            :input-props="{ id: 'donate-nickname', autocomplete: 'nickname' }"
          />
          <small class="field-help">{{ t("donation.nickname_help") }}</small>
        </div>
        <div v-if="suggestions.length" class="field-stack">
          <span class="field-label">{{ t("donation.suggestions") }}</span>
          <div class="nickname-suggestions">
            <n-button
              v-for="candidate in suggestions"
              :key="candidate"
              @click="chooseNickname(candidate)"
              >{{ candidate }}</n-button
            >
          </div>
        </div>
        <n-checkbox v-model:checked="showPublic" :disabled="locked">{{
          t("donation.show_public")
        }}</n-checkbox>
        <small class="field-help">{{ t("donation.public_help") }}</small>
        <div class="field-stack">
          <label id="donate-currency-label" class="field-label">{{
            t("donation.currency")
          }}</label>
          <n-select
            :value="currency"
            :options="currencyOptions"
            :disabled="locked"
            aria-labelledby="donate-currency-label"
            @update:value="chooseCurrency"
          />
        </div>
        <div
          v-if="group?.amounts.length"
          class="donate-amounts"
          role="group"
          :aria-label="t('donation.presets')"
        >
          <n-button
            v-for="value in group.amounts"
            :key="value"
            :disabled="locked"
            :type="
              amount === priceText(value, currency) ? 'primary' : 'default'
            "
            :aria-pressed="amount === priceText(value, currency)"
            @click="amount = priceText(value, currency)"
            >{{ money(value, currency) }}</n-button
          >
        </div>
        <div class="field-stack">
          <label class="field-label" for="donate-amount"
            >{{ t("donation.amount") }} · {{ currency }}</label
          >
          <n-input
            v-model:value="amount"
            :disabled="locked"
            :input-props="{
              id: 'donate-amount',
              inputmode: 'decimal',
              autocomplete: 'off',
            }"
          />
          <small v-if="group" class="field-help">{{
            t("donation.range", {
              min: money(group.min_amount, currency),
              max: money(group.max_amount, currency),
            })
          }}</small>
        </div>
        <n-alert v-if="intent && !busy && !repeatOpen" type="warning">{{
          t("donation.retry_help")
        }}</n-alert>
        <n-alert v-if="storageError" type="warning">{{
          t("donation.intent_storage_help")
        }}</n-alert>
        <p class="helper">{{ t("donation.payment_help") }}</p>
        <n-button
          type="primary"
          attr-type="submit"
          :loading="busy"
          :disabled="loading && !intent"
          >{{
            receiptPath ? t("donation.view_record") : t("donation.submit")
          }}</n-button
        >
        <router-link to="/center/donations" class="text-link">{{
          t("donation.lookup")
        }}</router-link>
      </form>
      <section class="panel grid public-donations" :aria-busy="publicLoading">
        <div class="section-title">
          <h2>{{ t("donation.supporters") }}</h2>
          <n-button :loading="publicLoading" @click="loadPublic()">{{
            t("common.refresh")
          }}</n-button>
        </div>
        <p class="muted">{{ t("donation.supporters_help") }}</p>
        <n-alert v-if="publicError" type="warning">{{ publicError }}</n-alert>
        <n-skeleton
          v-if="publicLoading && !publicRecords.items.length"
          text
          :repeat="2"
        />
        <n-empty
          v-else-if="!publicRecords.items.length && !publicError"
          :description="t('donation.no_public_records')"
        />
        <article
          v-for="(record, index) in publicRecords.items"
          :key="`${record.nickname}-${record.paid_at}-${index}`"
          class="public-donation"
        >
          <strong>{{ record.nickname }}</strong
          ><span>{{ money(record.amount, record.currency) }}</span
          ><small class="muted">{{ formatTime(record.paid_at) }}</small>
        </article>
        <div
          v-if="publicRecords.page > 1 || publicRecords.has_more"
          class="form-actions"
        >
          <n-button
            :disabled="publicLoading || publicRecords.page <= 1"
            @click="loadPublic(publicRecords.page - 1)"
            >{{ t("pagination.previous") }}</n-button
          ><span>{{ t("pagination.page", { page: publicRecords.page }) }}</span
          ><n-button
            :disabled="publicLoading || !publicRecords.has_more"
            @click="loadPublic(publicRecords.page + 1)"
            >{{ t("pagination.next") }}</n-button
          >
        </div>
      </section>
    </main>
    <FormDialog
      v-model:show="repeatOpen"
      :title="t('donation.repeat_title')"
      :busy="busy"
      width="min(440px, calc(100vw - 28px))"
    >
      <p class="repeat-message">
        {{
          t("donation.repeat_help", { nickname: intent?.nickname || nickname })
        }}
      </p>
      <template #footer>
        <n-button :disabled="busy" @click="repeatOpen = false">{{
          t("donation.change_nickname")
        }}</n-button>
        <n-button type="primary" :loading="busy" @click="confirmRepeat">{{
          t("donation.confirm_repeat")
        }}</n-button>
      </template>
    </FormDialog>
  </StoreLayout>
</template>

<style scoped lang="scss">
.donate-page {
  max-width: 640px;
}
h1,
.public-donation strong {
  overflow-wrap: anywhere;
}
.donate-amounts,
.nickname-suggestions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.nickname-suggestions :deep(.n-button) {
  max-width: 100%;
  height: auto;
  min-height: 34px;
}
.nickname-suggestions :deep(.n-button__content) {
  white-space: normal;
  overflow-wrap: anywhere;
}
.public-donation {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid #e5e7eb;
}
.public-donation small {
  grid-column: 1 / -1;
}
.helper,
p,
h1 {
  margin: 0;
}
.repeat-message {
  overflow-wrap: anywhere;
  line-height: 1.7;
}
</style>
