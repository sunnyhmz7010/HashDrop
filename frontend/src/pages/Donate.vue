<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NInput, NSkeleton } from "naive-ui";
import StoreLayout from "../components/StoreLayout.vue";
import MarkdownContent from "../components/MarkdownContent.vue";
import {
  api,
  errorText,
  money,
  priceText,
  toMinor,
  type Donation,
  type DonationSettings,
} from "../api";
import { t } from "../i18n";

const router = useRouter(),
  settings = ref<DonationSettings>(),
  amount = ref("");
const loading = ref(true),
  busy = ref(false),
  error = ref(""),
  receiptPath = ref("");
const controller = new AbortController();
async function load() {
  if (busy.value) return;
  loading.value = true;
  error.value = "";
  try {
    const result = await api<DonationSettings>("/donation", {
      signal: controller.signal,
    });
    settings.value = result;
    if (result.enabled)
      amount.value = priceText(
        result.amounts[0] ?? result.min_amount,
        result.currency,
      );
  } catch (e) {
    if (!controller.signal.aborted) error.value = errorText(e);
  } finally {
    loading.value = false;
  }
}
onMounted(load);
onUnmounted(() => controller.abort());
async function submit() {
  if (busy.value || !settings.value?.enabled) return;
  if (receiptPath.value) {
    await router.push(receiptPath.value);
    return;
  }
  busy.value = true;
  error.value = "";
  try {
    const value = toMinor(amount.value, settings.value.currency);
    if (value < settings.value.min_amount || value > settings.value.max_amount)
      throw new Error(t("errors.donation_amount"));
    const result = await api<{ donation: Donation; token: string }>(
      "/donations",
      {
        method: "POST",
        body: { amount: value, currency: settings.value.currency },
        signal: controller.signal,
      },
    );
    // 创建成功后保留凭证入口，导航失败时继续同一笔赞赏。
    receiptPath.value = `/donate/orders/${result.donation.id}#token=${result.token}`;
    await router.push(receiptPath.value);
  } catch (e) {
    if (!controller.signal.aborted) error.value = errorText(e);
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
      <section v-if="loading && !settings" class="panel grid" aria-busy="true">
        <n-skeleton text width="50%" /><n-skeleton
          text
          :repeat="2"
        /><n-skeleton height="40px" />
      </section>
      <section v-else-if="settings && !settings.enabled" class="panel grid">
        <div class="section-title">
          <h1>{{ t("nav.donation") }}</h1>
        </div>
        <p class="muted">{{ t("donation.unavailable") }}</p>
      </section>
      <form
        v-else-if="settings?.enabled"
        class="panel grid"
        @submit.prevent="submit"
      >
        <div class="section-title">
          <h1>{{ settings.title }}</h1>
        </div>
        <MarkdownContent
          v-if="settings.description"
          :content="settings.description"
        />
        <div
          v-if="settings.amounts.length"
          class="donate-amounts"
          role="group"
          :aria-label="t('donation.presets')"
        >
          <n-button
            v-for="value in settings.amounts"
            :key="value"
            :disabled="busy || !!receiptPath"
            :type="
              amount === priceText(value, settings.currency)
                ? 'primary'
                : 'default'
            "
            :aria-pressed="amount === priceText(value, settings.currency)"
            @click="amount = priceText(value, settings.currency)"
            >{{ money(value, settings.currency) }}</n-button
          >
        </div>
        <div class="field-stack">
          <label class="field-label" for="donate-amount"
            >{{ t("donation.amount") }} · {{ settings.currency }}</label
          >
          <n-input
            v-model:value="amount"
            :disabled="busy || !!receiptPath"
            :input-props="{
              id: 'donate-amount',
              inputmode: 'decimal',
              autocomplete: 'off',
            }"
          />
          <small class="field-help">{{
            t("donation.range", {
              min: money(settings.min_amount, settings.currency),
              max: money(settings.max_amount, settings.currency),
            })
          }}</small>
        </div>
        <p class="helper">{{ t("donation.payment_help") }}</p>
        <n-button
          type="primary"
          attr-type="submit"
          :loading="busy"
          :disabled="loading"
          >{{
            receiptPath ? t("donation.view_record") : t("donation.submit")
          }}</n-button
        >
      </form>
    </main>
  </StoreLayout>
</template>

<style scoped lang="scss">
.donate-page {
  max-width: 640px;
}
h1 {
  overflow-wrap: anywhere;
}
.donate-amounts {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.helper {
  margin: 0;
}
</style>
