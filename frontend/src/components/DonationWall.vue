<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NSkeleton,
  NTag,
  useDialog,
  useMessage,
} from "naive-ui";
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
  checking = ref(""),
  deleting = ref(""),
  search = ref(""),
  appliedSearch = ref("");
const dialog = useDialog(),
  message = useMessage();
const processing = computed(() => !!checking.value || !!deleting.value);
const controller = new AbortController();
let listController: AbortController | undefined;
let listGeneration = 0;
let queryTimer: ReturnType<typeof setTimeout> | undefined;
let lastRequest = { page: 1, query: "" };
const supporters = computed(() => {
  const groups = new Map<
    string,
    {
      key: string;
      nickname: string;
      items: Donation[];
    }
  >();
  for (const donation of records.value.items) {
    const key = donation.nickname;
    let supporter = groups.get(key);
    if (!supporter) {
      supporter = {
        key,
        nickname: donation.nickname,
        items: [],
      };
      groups.set(key, supporter);
    }
    supporter.items.push(donation);
  }
  return [...groups.values()];
});

async function load(page = records.value.page, query = appliedSearch.value) {
  const nickname = query.trim();
  lastRequest = { page, query: nickname };
  const current = ++listGeneration;
  listController?.abort();
  const request = new AbortController();
  listController = request;
  loading.value = true;
  error.value = "";
  const params = new URLSearchParams({
    group_by: "nickname",
    page: String(page),
  });
  if (nickname) params.set("q", nickname);
  try {
    const result = await api<DonationList>(`/admin/donations?${params}`, {
      signal: request.signal,
    });
    if (current === listGeneration && !request.signal.aborted) {
      records.value = result;
      appliedSearch.value = nickname;
    }
  } catch (cause) {
    if (current === listGeneration && !request.signal.aborted)
      error.value = errorText(cause);
  } finally {
    if (current === listGeneration) loading.value = false;
  }
}
function refresh() {
  clearTimeout(queryTimer);
  const query = search.value.trim();
  return load(query === appliedSearch.value ? records.value.page : 1, query);
}
watch(search, (query) => {
  clearTimeout(queryTimer);
  queryTimer = setTimeout(() => void load(1, query), 250);
});
async function recheck(id: string) {
  if (processing.value || loading.value) return;
  checking.value = id;
  error.value = "";
  try {
    await api(`/admin/donations/${id}/recheck`, {
      method: "POST",
      body: {},
      signal: controller.signal,
    });
    await refresh();
  } catch (cause) {
    if (!controller.signal.aborted) error.value = errorText(cause);
  } finally {
    checking.value = "";
  }
}
function remove(donation: Donation) {
  if (processing.value || loading.value) return;
  const confirmation = dialog.warning({
    title: t("donation.delete_title"),
    content: t("donation.delete_warning", {
      nickname: donation.nickname,
      amount: money(donation.amount, donation.currency),
      time: formatTime(donation.created_at),
    }),
    positiveText: t("common.delete"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (processing.value || loading.value || controller.signal.aborted)
        return false;
      deleting.value = donation.id;
      error.value = "";
      confirmation.loading = true;
      confirmation.negativeButtonProps = { disabled: true };
      confirmation.closable = false;
      confirmation.maskClosable = false;
      confirmation.closeOnEsc = false;
      try {
        await api(`/admin/donations/${donation.id}`, {
          method: "DELETE",
          signal: controller.signal,
        });
        if (controller.signal.aborted) return true;
        const items = records.value.items.filter(
          (item) => item.id !== donation.id,
        );
        const next =
          !items.length && records.value.page > 1
            ? records.value.page - 1
            : records.value.page;
        records.value = { ...records.value, items };
        await load(next);
        message.success(t("donation.deleted"));
        return true;
      } catch (cause) {
        if (!controller.signal.aborted) message.error(errorText(cause));
        return false;
      } finally {
        deleting.value = "";
        confirmation.loading = false;
        confirmation.negativeButtonProps = { disabled: false };
        confirmation.closable = true;
        confirmation.maskClosable = true;
        confirmation.closeOnEsc = true;
      }
    },
  });
}
onMounted(() => load());
onUnmounted(() => {
  controller.abort();
  clearTimeout(queryTimer);
  listGeneration++;
  listController?.abort();
});
defineExpose({ refresh, loading, processing });
</script>

<template>
  <section
    class="panel grid donation-wall"
    :aria-label="t('donation.records')"
    :aria-busy="loading"
  >
    <div class="section-title donation-wall-header">
      <h2>{{ t("donation.records") }}</h2>
      <n-input
        v-model:value="search"
        class="donation-search"
        :input-props="{
          id: 'donation-search',
          'aria-label': t('donation.search'),
        }"
        :placeholder="t('donation.search')"
        :maxlength="120"
        :disabled="processing"
        clearable
      >
        <template #prefix>
          <svg
            viewBox="0 0 20 20"
            width="16"
            height="16"
            fill="none"
            aria-hidden="true"
          >
            <circle cx="8.5" cy="8.5" r="5.5" />
            <path d="m13 13 4 4" />
          </svg>
        </template>
      </n-input>
    </div>
    <n-alert v-if="error" type="error">
      {{ errorText(error) }}
      <n-button
        text
        :disabled="loading || processing"
        @click="load(lastRequest.page, lastRequest.query)"
        >{{ t("common.retry") }}</n-button
      >
    </n-alert>
    <div v-if="loading && !records.items.length" class="supporter-field">
      <n-skeleton v-for="index in 3" :key="index" height="180px" />
    </div>
    <n-empty
      v-else-if="!records.items.length && !error"
      :description="
        appliedSearch ? t('donation.no_matches') : t('donation.no_records')
      "
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
        <h3 class="supporter-name">
          {{ supporter.nickname }}
        </h3>
        <details class="supporter-history">
          <summary>
            <span class="supporter-summary">
              <span class="supporter-summary-label">
                <span>{{ t("donation.total_count") }}</span>
                <small class="history-toggle">
                  <span class="history-expand">{{
                    t("donation.expand_history")
                  }}</span>
                  <span class="history-collapse">{{
                    t("donation.collapse_history")
                  }}</span>
                  <svg
                    class="history-chevron"
                    viewBox="0 0 16 16"
                    width="14"
                    height="14"
                    fill="none"
                    aria-hidden="true"
                  >
                    <path d="m4 6 4 4 4-4" />
                  </svg>
                </small>
              </span>
              <strong class="supporter-count">{{
                t("donation.count", { count: supporter.items.length })
              }}</strong>
            </span>
          </summary>
          <div class="supporter-history-body">
            <article
              v-for="donation in supporter.items"
              :key="donation.id"
              class="donation-record"
              :aria-label="donation.id"
            >
              <div class="donation-record-line">
                <span class="donation-record-time">
                  <svg
                    viewBox="0 0 20 20"
                    width="16"
                    height="16"
                    fill="none"
                    aria-hidden="true"
                  >
                    <circle cx="10" cy="10" r="7" />
                    <path d="M10 6v4l3 2" />
                  </svg>
                  {{ formatTime(donation.created_at) }}
                </span>
                <div class="donation-record-actions">
                  <strong
                    class="donation-record-amount"
                    :class="{ 'is-paid': donation.payment_status === 'paid' }"
                    >{{ donation.payment_status === "paid" ? "+" : ""
                    }}{{ money(donation.amount, donation.currency) }}</strong
                  >
                  <n-button
                    class="donation-delete"
                    size="small"
                    quaternary
                    circle
                    type="error"
                    :aria-label="t('donation.delete_record')"
                    :title="t('donation.delete_record')"
                    :loading="deleting === donation.id"
                    :disabled="processing || loading"
                    @click="remove(donation)"
                  >
                    <template #icon>
                      <svg
                        viewBox="0 0 20 20"
                        width="18"
                        height="18"
                        fill="none"
                        aria-hidden="true"
                      >
                        <path
                          d="M3.5 5.5h13M8 3h4l1 2.5H7L8 3ZM5.5 5.5l.7 11h7.6l.7-11M8.5 8.5v5M11.5 8.5v5"
                        />
                      </svg>
                    </template>
                  </n-button>
                </div>
              </div>
              <div class="donation-record-status">
                <n-tag
                  size="small"
                  :type="
                    donation.payment_status === 'paid' ? 'success' : 'default'
                  "
                  :bordered="false"
                  >{{ paymentNames[donation.payment_status] }}</n-tag
                >
              </div>
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
                :disabled="processing || loading"
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
        :disabled="records.page <= 1 || loading || processing"
        @click="load(records.page - 1)"
        >{{ t("pagination.previous") }}</n-button
      >
      <span>{{ t("pagination.page", { page: records.page }) }}</span>
      <n-button
        :disabled="!records.has_more || loading || processing"
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
.donation-wall h2 {
  margin: 0;
}
.donation-wall-header {
  margin-bottom: 0;
}
.donation-search {
  width: 260px;
  max-width: 100%;
}
.donation-search svg,
.history-chevron,
.donation-record-time svg,
.donation-delete svg {
  flex-shrink: 0;
  stroke: currentColor;
  stroke-width: 1.6;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.donation-wall .helper {
  margin-top: 6px;
}
.supporter-field {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr));
  align-items: start;
  gap: 24px;
  padding: 24px 14px;
  border-radius: 12px;
  background: #f7f8fc;
}
.supporter-card {
  --card-accent: #2f7465;
  --stripe-start: #65bda8;
  --stripe-end: #9de7d4;
  position: relative;
  min-width: 0;
  overflow: hidden;
  background: linear-gradient(180deg, #f9fbff, #fff 100px);
  border: 1px solid #e4e8ef;
  border-radius: 18px;
  box-shadow: 0 2px 4px rgb(17 24 39 / 0.05);
  animation: supporter-float var(--float-duration) var(--float-easing)
    var(--float-delay) infinite;
  transition: border-color var(--feedback-duration) ease;
}
.supporter-card::before {
  position: absolute;
  inset: 0 0 auto;
  height: 4px;
  background: linear-gradient(90deg, var(--stripe-start), var(--stripe-end));
  content: "";
}
.supporter-card:nth-child(3n + 2) {
  --card-accent: #a85b24;
  --stripe-start: #f4cb60;
  --stripe-end: #fff09c;
}
.supporter-card:nth-child(3n) {
  --card-accent: #334155;
  --stripe-start: #9faec5;
  --stripe-end: #9cd8f5;
}
.supporter-field:hover .supporter-card,
.supporter-field:focus-within .supporter-card,
.supporter-card:has(details[open]) {
  animation-play-state: paused;
}
.supporter-card:hover,
.supporter-card:focus-within,
.supporter-card:has(details[open]) {
  border-color: #b8c8d4;
}
.supporter-name {
  margin: 0;
  padding: 26px 22px 18px;
  font-size: 18px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}
.supporter-history summary {
  padding: 0 22px 22px;
  cursor: pointer;
  list-style: none;
}
.supporter-history summary::-webkit-details-marker {
  display: none;
}
.supporter-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 16px;
  border: 1px solid #e5eaf3;
  border-radius: 14px;
  background: #f5f7fc;
  transition: background var(--feedback-duration) ease;
}
.supporter-history summary:hover .supporter-summary {
  background: #eef3fa;
}
.supporter-summary-label {
  display: grid;
  gap: 7px;
  color: #4b5563;
  font-size: 14px;
}
.supporter-count {
  color: var(--card-accent);
  font-size: 26px;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}
.history-toggle {
  display: flex;
  align-items: center;
  gap: 5px;
  color: #6b7280;
  font-size: 13px;
}
.history-collapse,
.supporter-history[open] .history-expand {
  display: none;
}
.supporter-history[open] .history-collapse {
  display: inline;
}
.history-chevron {
  transition: transform var(--feedback-duration) ease;
}
.supporter-history[open] .history-chevron {
  transform: rotate(180deg);
}
.supporter-history-body {
  display: grid;
  gap: 12px;
  max-height: 440px;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 20px 22px 22px;
  border-top: 1px solid #eef0f4;
}
.donation-record {
  display: grid;
  gap: 8px;
  padding: 14px;
  border: 1px solid #e7ecf2;
  border-radius: 14px;
  background: #f8faff;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.donation-record-line {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.donation-record-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  margin-left: auto;
}
.donation-record-time {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #667085;
  font-size: 13px;
}
.donation-record-amount {
  padding: 6px 10px;
  border: 1px solid #e1e7ef;
  border-radius: 10px;
  background: #edf1f6;
  color: #475569;
  font-size: 16px;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}
.donation-record-amount.is-paid {
  border-color: #cce8df;
  background: #e7f4ef;
  color: #367964;
}
.donation-record .n-button {
  justify-self: start;
}
.donation-delete {
  flex-shrink: 0;
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
  .donation-wall-header {
    align-items: stretch;
    flex-direction: column;
  }
  .donation-search {
    width: 100%;
  }
  .supporter-field {
    padding: 20px 8px;
    gap: 20px;
  }
  .supporter-name {
    padding: 22px 18px 16px;
  }
  .supporter-history summary {
    padding: 0 18px 18px;
  }
  .supporter-history-body {
    padding: 18px;
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
