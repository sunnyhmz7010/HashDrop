<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { NAlert, NButton, NEmpty, NSkeleton, NTag } from "naive-ui";
import {
  api,
  errorText,
  money,
  paymentNames,
  type Donation,
  type DonationList,
} from "../api";
import { formatTime, t } from "../i18n";

const records = ref<DonationList>({
  items: [],
  page: 1,
  has_more: false,
  totals: [],
});
const loading = ref(false),
  error = ref(""),
  checking = ref("");
const controller = new AbortController();
const supporters = computed(() => {
  const groups = new Map<
    string,
    {
      key: string;
      nickname: string;
      items: Donation[];
      totals: Map<string, number>;
    }
  >();
  for (const donation of records.value.items) {
    const key = donation.nickname
      ? `nickname:${donation.nickname}`
      : `legacy:${donation.id}`;
    let supporter = groups.get(key);
    if (!supporter) {
      supporter = {
        key,
        nickname: donation.nickname,
        items: [],
        totals: new Map(),
      };
      groups.set(key, supporter);
    }
    supporter.items.push(donation);
    if (donation.payment_status === "paid")
      supporter.totals.set(
        donation.currency,
        (supporter.totals.get(donation.currency) ?? 0) + donation.amount,
      );
  }
  return [...groups.values()];
});

async function load(page = records.value.page) {
  if (loading.value) return;
  loading.value = true;
  error.value = "";
  try {
    const result = await api<DonationList>(
      `/admin/donations?group_by=nickname&page=${page}`,
      { signal: controller.signal },
    );
    if (!controller.signal.aborted) records.value = result;
  } catch (cause) {
    if (!controller.signal.aborted) error.value = errorText(cause);
  } finally {
    loading.value = false;
  }
}
async function recheck(id: string) {
  if (checking.value || loading.value) return;
  checking.value = id;
  error.value = "";
  try {
    await api(`/admin/donations/${id}/recheck`, {
      method: "POST",
      body: {},
      signal: controller.signal,
    });
    await load();
  } catch (cause) {
    if (!controller.signal.aborted) error.value = errorText(cause);
  } finally {
    checking.value = "";
  }
}
onMounted(() => load());
onUnmounted(() => controller.abort());
</script>

<template>
  <section
    class="panel grid donation-wall"
    :aria-label="t('donation.records')"
    :aria-busy="loading"
  >
    <div class="section-title">
      <h2>{{ t("donation.records") }}</h2>
      <n-button :loading="loading" :disabled="!!checking" @click="load()">{{
        t("orders.refresh_list")
      }}</n-button>
    </div>
    <n-alert v-if="error" type="error">
      {{ errorText(error) }}
      <n-button text :disabled="loading || !!checking" @click="load()">{{
        t("common.retry")
      }}</n-button>
    </n-alert>
    <div v-if="records.totals.length" class="donation-totals">
      <span class="muted">{{ t("donation.verified_total") }}</span>
      <span v-for="total in records.totals" :key="total.currency">
        <strong>{{ money(total.amount, total.currency) }}</strong>
        <small class="muted">
          · {{ t("donation.count", { count: total.count }) }}</small
        >
      </span>
    </div>
    <div v-if="loading && !records.items.length" class="supporter-field">
      <n-skeleton v-for="index in 3" :key="index" height="180px" />
    </div>
    <n-empty
      v-else-if="!records.items.length && !error"
      :description="t('donation.no_records')"
    />
    <div v-else-if="supporters.length" class="supporter-field">
      <article
        v-for="(supporter, index) in supporters"
        :key="supporter.key"
        class="supporter-card"
        :style="{
          '--float-delay': `${-(index % 7) * 1.3}s`,
          '--float-duration': `${7 + (index % 4)}s`,
        }"
      >
        <header class="supporter-head">
          <span class="supporter-avatar" aria-hidden="true">{{
            Array.from(supporter.nickname)[0] || "?"
          }}</span>
          <div class="supporter-identity">
            <h3>{{ supporter.nickname || t("donation.legacy_supporter") }}</h3>
            <small class="muted">{{
              formatTime(supporter.items[0]!.created_at)
            }}</small>
          </div>
        </header>
        <div class="supporter-amounts">
          <small class="muted">{{ t("donation.verified_total") }}</small>
          <template v-if="supporter.totals.size">
            <strong
              v-for="[currency, amount] in supporter.totals"
              :key="currency"
              >{{ money(amount, currency) }}</strong
            >
          </template>
          <span v-else class="muted">{{ t("donation.not_verified") }}</span>
        </div>
        <details class="supporter-history">
          <summary>
            <span>{{
              t("donation.history_count", { count: supporter.items.length })
            }}</span>
            <span class="history-chevron" aria-hidden="true">⌄</span>
          </summary>
          <div class="supporter-history-body">
            <article
              v-for="donation in supporter.items"
              :key="donation.id"
              class="donation-record"
              :aria-label="donation.id"
            >
              <div class="section-title">
                <strong>{{ money(donation.amount, donation.currency) }}</strong>
                <n-tag
                  size="small"
                  :type="
                    donation.payment_status === 'paid' ? 'success' : 'default'
                  "
                  :bordered="false"
                  >{{ paymentNames[donation.payment_status] }}</n-tag
                >
              </div>
              <small class="muted">{{ formatTime(donation.created_at) }}</small>
              <span>{{ donation.title }}</span>
              <small class="code-cell muted">{{ donation.id }}</small>
              <small class="muted">{{
                donation.show_public
                  ? t("donation.public_yes")
                  : t("donation.public_no")
              }}</small>
              <small v-if="donation.last_error" class="helper">{{
                errorText(donation.last_error)
              }}</small>
              <n-button
                size="small"
                :loading="checking === donation.id"
                :disabled="!!checking || loading"
                @click="recheck(donation.id)"
                >{{ t("orders.refresh_status") }}</n-button
              >
            </article>
          </div>
        </details>
      </article>
    </div>
    <div v-if="records.page > 1 || records.has_more" class="form-actions">
      <n-button
        :disabled="records.page <= 1 || loading || !!checking"
        @click="load(records.page - 1)"
        >{{ t("pagination.previous") }}</n-button
      >
      <span>{{ t("pagination.page", { page: records.page }) }}</span>
      <n-button
        :disabled="!records.has_more || loading || !!checking"
        @click="load(records.page + 1)"
        >{{ t("pagination.next") }}</n-button
      >
    </div>
  </section>
</template>

<style scoped lang="scss">
.donation-wall {
  --float-distance: -6px;
  --float-easing: ease-in-out;
  --feedback-duration: 180ms;
  overflow: hidden;
}
.donation-wall h2,
.supporter-identity h3 {
  margin: 0;
}
.donation-wall .helper {
  margin-top: 6px;
}
.donation-totals {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 8px 18px;
  font-size: 13px;
}
.supporter-field {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 240px), 1fr));
  align-items: start;
  gap: 28px 20px;
  padding: 24px 12px;
  border-radius: 8px;
  background: #f6f7f9;
}
.supporter-card {
  min-width: 0;
  padding: 18px;
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  box-shadow: 0 3px 12px rgb(17 24 39 / 0.04);
  animation: supporter-float var(--float-duration) var(--float-easing)
    var(--float-delay) infinite;
  transition: border-color var(--feedback-duration) ease;
}
.supporter-card:nth-child(3n + 2) {
  margin-top: 12px;
}
.supporter-field:hover .supporter-card,
.supporter-field:focus-within .supporter-card,
.supporter-card:has(details[open]) {
  animation-play-state: paused;
}
.supporter-card:hover,
.supporter-card:focus-within,
.supporter-card:has(details[open]) {
  border-color: #18a058;
}
.supporter-head {
  display: flex;
  align-items: center;
  gap: 10px;
}
.supporter-avatar {
  display: grid;
  place-items: center;
  flex: 0 0 38px;
  height: 38px;
  border-radius: 12px;
  background: #eef8f1;
  color: #18a058;
  font-size: 18px;
  font-weight: 600;
}
.supporter-identity {
  min-width: 0;
  overflow-wrap: anywhere;
}
.supporter-identity h3 {
  font-size: 14px;
}
.supporter-identity small {
  font-size: 11px;
}
.supporter-amounts {
  display: grid;
  gap: 4px;
  margin: 18px 0;
  overflow-wrap: anywhere;
}
.supporter-amounts strong {
  font-size: 20px;
  font-variant-numeric: tabular-nums;
}
.supporter-history summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid #eef0f3;
  color: #6b7280;
  font-size: 12px;
  cursor: pointer;
  list-style: none;
}
.supporter-history summary::-webkit-details-marker {
  display: none;
}
.history-chevron {
  transition: transform var(--feedback-duration) ease;
}
.supporter-history[open] .history-chevron {
  transform: rotate(180deg);
}
.supporter-history-body {
  max-height: 320px;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.donation-record {
  display: grid;
  gap: 6px;
  padding: 14px 0;
  font-size: 12px;
  overflow-wrap: anywhere;
}
.donation-record + .donation-record {
  border-top: 1px solid #eef0f3;
}
.donation-record .n-button {
  justify-self: start;
}
@keyframes supporter-float {
  0%,
  100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(var(--float-distance));
  }
}
@media (max-width: 600px) {
  .supporter-card:nth-child(3n + 2) {
    margin-top: 0;
  }
}
@media (prefers-reduced-motion: reduce) {
  .supporter-card {
    animation: none;
    transition: none;
  }
  .history-chevron {
    transition: none;
  }
}
</style>
