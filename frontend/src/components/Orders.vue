<!-- 详情布局基于 HashPay src/app/components/OrderModal.vue 与 styles.scss 修改。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。
     保留原分区、信息网格和操作栏，字段替换为 HashDrop 订单与交付数据。 -->
<script setup lang="ts">
import { formatTime, t } from "../i18n";

import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NDatePicker,
  NPagination,
  NInput,
  NSelect,
  NSpin,
  NTag,
  useDialog,
  useMessage,
} from "naive-ui";
import FormDialog from "./FormDialog.vue";
import OrderTable from "./OrderTable.vue";
import {
  api,
  copyText,
  deliveryNames,
  errorText,
  kindNames,
  money,
  paymentNames,
  type Order,
  type OrderDetail,
} from "../api";
const message = useMessage(),
  dialog = useDialog(),
  items = ref<Order[]>([]),
  page = ref(1),
  total = ref(0),
  pageSize = ref(20),
  loading = ref(false),
  error = ref(""),
  show = ref(false),
  selected = ref<OrderDetail>(),
  selectedID = ref(""),
  busy = ref(false),
  detailError = ref("");
const statusOptions = computed(() => [
  { label: t("orders.all_statuses"), value: "" },
  ...Object.entries(paymentNames.value).map(([value, label]) => ({
    value,
    label,
  })),
]);
const filters = reactive({
  status: "",
  currency: "",
  min: "",
  max: "",
  from: null as number | null,
  to: null as number | null,
});
const applied = ref({ ...filters });
const filtered = computed(
  () =>
    !!(
      applied.value.status ||
      applied.value.currency ||
      applied.value.min ||
      applied.value.max ||
      applied.value.from !== null ||
      applied.value.to !== null
    ),
);
const currencies = computed(() => [
  { label: t("orders.all_currencies"), value: "" },
  ...["CNY", "USD", "EUR", "GBP", "HKD", "JPY", "USDT", "USDC"].map(
    (value) => ({ label: value, value }),
  ),
]);
let listGeneration = 0;
let listController: AbortController | undefined;
let lastRequest = { page: 1, query: { ...filters } };
const compactQuery = window.matchMedia("(max-width: 600px)");
const compact = ref(compactQuery.matches);
const updateCompact = () => {
  compact.value = compactQuery.matches;
};
onMounted(() => compactQuery.addEventListener("change", updateCompact));
onUnmounted(() => compactQuery.removeEventListener("change", updateCompact));
async function load(next = page.value, source = applied.value) {
  const query = { ...source };
  lastRequest = { page: next, query };
  const current = ++listGeneration;
  listController?.abort();
  listController = new AbortController();
  loading.value = true;
  error.value = "";
  const params = new URLSearchParams({
    page: String(next),
    status: query.status,
    currency: query.currency,
  });
  if (query.min.trim()) params.set("min_amount", query.min.trim());
  if (query.max.trim()) params.set("max_amount", query.max.trim());
  if (query.from !== null)
    params.set("created_from", String(Math.floor(query.from / 1000)));
  if (query.to !== null)
    params.set("created_to", String(Math.floor(query.to / 1000)));
  try {
    const result = await api<{
      items: Order[];
      page: number;
      total: number;
      page_size: number;
    }>("/admin/orders?" + params, { signal: listController.signal });
    if (current !== listGeneration) return;
    items.value = result.items;
    page.value = result.page;
    total.value = result.total;
    pageSize.value = result.page_size;
    applied.value = query;
  } catch (e) {
    if (current === listGeneration) error.value = errorText(e);
  } finally {
    if (current === listGeneration) loading.value = false;
  }
}
function resetFilters() {
  Object.assign(filters, {
    status: "",
    currency: "",
    min: "",
    max: "",
    from: null,
    to: null,
  });
  void load(1, filters);
}
function retryList() {
  void load(lastRequest.page, lastRequest.query);
}
onUnmounted(() => {
  listGeneration++;
  listController?.abort();
});
onMounted(() => load());
let generation = 0,
  controller: AbortController | undefined;
function cancelDetail() {
  generation++;
  controller?.abort();
  busy.value = false;
}
watch(show, (value) => {
  if (!value) cancelDetail();
});
onUnmounted(cancelDetail);
async function open(order: Order) {
  cancelDetail();
  const current = generation;
  controller = new AbortController();
  selectedID.value = order.id;
  selected.value = undefined;
  show.value = true;
  detailError.value = "";
  busy.value = true;
  try {
    const result = await api<OrderDetail>(`/admin/orders/${order.id}`, {
      signal: controller.signal,
    });
    if (current === generation && selectedID.value === order.id)
      selected.value = result;
  } catch (e) {
    if (current === generation) detailError.value = errorText(e);
  } finally {
    if (current === generation) busy.value = false;
  }
}
async function action(name: string) {
  if (busy.value || !selected.value) return;
  const id = selected.value.order.id,
    current = generation;
  controller = new AbortController();
  busy.value = true;
  detailError.value = "";
  try {
    const result = await api<OrderDetail>(`/admin/orders/${id}/${name}`, {
      method: "POST",
      body: {},
      signal: controller.signal,
    });
    if (current !== generation) return;
    selected.value = result;
    message.success(t("orders.updated"));
    await load();
  } catch (e) {
    if (current === generation) detailError.value = errorText(e);
  } finally {
    if (current === generation) busy.value = false;
  }
}
function remove() {
  if (busy.value || !selected.value) return;
  const id = selected.value.order.id,
    current = generation;
  const confirmation = dialog.warning({
    title: t("orders.delete_title"),
    content: t("orders.delete_warning"),
    positiveText: t("orders.delete"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (busy.value || current !== generation || selectedID.value !== id)
        return false;
      busy.value = true;
      confirmation.loading = true;
      confirmation.negativeButtonProps = { disabled: true };
      confirmation.closable = false;
      confirmation.maskClosable = false;
      confirmation.closeOnEsc = false;
      detailError.value = "";
      controller = new AbortController();
      try {
        await api(`/admin/orders/${id}`, {
          method: "DELETE",
          signal: controller.signal,
        });
        if (current !== generation) return true;
        const next =
          items.value.length === 1 && page.value > 1
            ? page.value - 1
            : page.value;
        items.value = items.value.filter((item) => item.id !== id);
        show.value = false;
        selected.value = undefined;
        message.success(t("orders.deleted"));
        await load(next);
        return true;
      } catch (e) {
        if (current === generation) {
          detailError.value = errorText(e);
          message.error(detailError.value);
        }
        return false;
      } finally {
        if (current === generation) busy.value = false;
        confirmation.loading = false;
        confirmation.negativeButtonProps = { disabled: false };
        confirmation.closable = true;
        confirmation.maskClosable = true;
        confirmation.closeOnEsc = true;
      }
    },
  });
}
async function copy(value: string) {
  try {
    await copyText(value);
    message.success(t("common.copied"));
  } catch {
    message.warning(t("common.copy_manual"));
  }
}
</script>
<template>
  <div class="orders-view grid">
    <div class="section-title">
      <h2>{{ t("nav.orders") }}</h2>
      <n-button :loading="loading" @click="load()">{{
        t("orders.refresh_list")
      }}</n-button>
    </div>
    <n-alert v-if="error" type="error"
      >{{ errorText(error) }}
      <n-button text @click="retryList">{{
        t("common.retry")
      }}</n-button></n-alert
    >
    <div class="orders-workbench">
      <form class="orders-filter" @submit.prevent="load(1, filters)">
        <div class="field-stack">
          <label class="field-label">{{ t("orders.status_filter") }}</label
          ><n-select
            v-model:value="filters.status"
            :options="statusOptions"
            :aria-label="t('orders.status_filter')"
          />
        </div>
        <div class="field-stack orders-date-filter">
          <label class="field-label">{{ t("orders.created_range") }}</label>
          <div class="date-filter-inputs">
            <n-date-picker
              v-model:value="filters.from"
              type="datetime"
              clearable
              :placeholder="t('orders.created_from')"
            />
            <n-date-picker
              v-model:value="filters.to"
              type="datetime"
              clearable
              :placeholder="t('orders.created_to')"
            />
          </div>
        </div>
        <div class="field-stack">
          <label class="field-label">{{ t("orders.currency_filter") }}</label
          ><n-select
            v-model:value="filters.currency"
            :options="currencies"
            :aria-label="t('orders.currency_filter')"
            @update:value="
              (value) => {
                if (!value) filters.min = filters.max = '';
              }
            "
          />
        </div>
        <div class="field-stack">
          <label class="field-label" for="orders-min">{{
            t("orders.min_amount")
          }}</label
          ><n-input
            v-model:value="filters.min"
            :disabled="!filters.currency"
            :input-props="{ id: 'orders-min', inputmode: 'decimal' }"
          />
        </div>
        <div class="field-stack">
          <label class="field-label" for="orders-max">{{
            t("orders.max_amount")
          }}</label
          ><n-input
            v-model:value="filters.max"
            :disabled="!filters.currency"
            :input-props="{ id: 'orders-max', inputmode: 'decimal' }"
          />
        </div>
        <div class="row-actions">
          <n-button type="primary" attr-type="submit" :loading="loading">{{
            t("orders.apply_filters")
          }}</n-button
          ><n-button :disabled="loading" @click="resetFilters">{{
            t("orders.reset_filters")
          }}</n-button>
        </div>
      </form>
      <OrderTable
        :items="items"
        :loading="loading"
        :empty-text="filtered ? t('orders.filtered_empty') : t('orders.empty')"
        :show-empty="!error"
        @open="open"
      />
      <div class="orders-pagination">
        <span>{{ t("orders.total_count", { total }) }}</span
        ><n-pagination
          :page="page"
          :item-count="total"
          :page-size="pageSize"
          :page-slot="compact ? 5 : 7"
          size="small"
          :disabled="loading"
          @update:page="(next) => load(next)"
        />
      </div>
    </div>
  </div>
  <FormDialog v-model:show="show" :title="t('orders.detail')" :busy="busy">
    <template #header>
      <div class="order-modal-title">
        <span>{{ t("orders.detail") }}</span>
        <n-tag v-if="selected" size="small">
          {{ paymentNames[selected.order.payment_status] }}
        </n-tag>
      </div>
    </template>
    <n-spin :show="busy">
      <div v-if="selected" class="order-modal-body">
        <div class="order-modal-tools">
          <n-button
            :disabled="busy"
            secondary
            size="small"
            @click="action('recheck')"
          >
            <span class="tool-button-content">
              <span class="tool-icon tool-icon-check" aria-hidden="true"></span>
              <span>{{ t("orders.refresh_status") }}</span>
            </span>
          </n-button>
          <n-button
            v-if="
              selected.order.payment_status === 'paid' &&
              selected.order.delivery_status !== 'done' &&
              kindNames[selected.order.kind]
            "
            :disabled="busy"
            secondary
            size="small"
            @click="action('retry')"
          >
            <span class="tool-button-content">
              <span
                class="tool-icon tool-icon-notify"
                aria-hidden="true"
              ></span>
              <span>{{ t("orders.retry_delivery") }}</span>
            </span>
          </n-button>
          <n-button
            :disabled="busy"
            secondary
            size="small"
            type="error"
            @click="remove"
          >
            <span class="tool-button-content">
              <span
                class="tool-icon tool-icon-delete"
                aria-hidden="true"
              ></span>
              <span>{{ t("orders.delete") }}</span>
            </span>
          </n-button>
        </div>

        <n-alert v-if="selected.order.last_error" type="warning">
          {{ errorText(selected.order.last_error) }}
        </n-alert>

        <section class="order-modal-section">
          <div class="detail-grid">
            <div class="detail-item">
              <span>{{ t("orders.id") }}</span>
              <div class="detail-copy-row">
                <strong>{{ selected.order.id }}</strong>
                <n-button
                  size="small"
                  secondary
                  @click="copy(selected.order.id)"
                  >{{ t("common.copy") }}</n-button
                >
              </div>
            </div>
            <div class="detail-item">
              <span>{{ t("orders.gateway_id") }}</span>
              <div class="detail-copy-row">
                <strong>{{ selected.order.gateway_id || "--" }}</strong>
                <n-button
                  v-if="selected.order.gateway_id"
                  size="small"
                  secondary
                  @click="copy(selected.order.gateway_id)"
                  >{{ t("common.copy") }}</n-button
                >
              </div>
            </div>
            <div class="detail-item detail-item-wide">
              <span>{{ t("orders.info") }}</span>
              <strong
                >{{ selected.order.product_name }} ×
                {{ selected.order.quantity }}</strong
              >
            </div>
            <div class="detail-item">
              <span>{{ t("common.created_at") }}</span>
              <strong>{{ formatTime(selected.order.created_at) }}</strong>
            </div>
            <div class="detail-item">
              <span>{{ t("product.kind") }}</span>
              <strong>{{
                kindNames[selected.order.kind] || t("kind.unsupported")
              }}</strong>
            </div>
          </div>
        </section>

        <section class="order-modal-section">
          <h3>{{ t("orders.payment_info") }}</h3>
          <div class="detail-grid">
            <div v-if="selected.order.discount_total > 0" class="detail-item">
              <span>{{ t("promotion.original_total") }}</span
              ><strong>{{
                money(selected.order.original_total, selected.order.currency)
              }}</strong>
            </div>
            <div
              v-for="discount in selected.order.discounts"
              :key="discount.id"
              class="detail-item"
            >
              <span
                >{{ discount.name
                }}<small v-if="discount.code">
                  · {{ discount.code }}</small
                ></span
              ><strong
                >−{{ money(discount.amount, selected.order.currency) }}</strong
              >
            </div>
            <div class="detail-item">
              <span>{{ t("orders.amount") }}</span>
              <strong>{{
                money(selected.order.total, selected.order.currency)
              }}</strong>
            </div>
            <div class="detail-item">
              <span>{{ t("product.unit_price") }}</span>
              <strong>{{
                money(selected.order.unit_price, selected.order.currency)
              }}</strong>
            </div>
            <div class="detail-item detail-item-wide">
              <span>{{ t("orders.deadline") }}</span>
              <strong>{{
                selected.order.expires_at
                  ? formatTime(selected.order.expires_at)
                  : "--"
              }}</strong>
            </div>
          </div>
        </section>

        <section class="order-modal-section">
          <div class="order-delivery-title">
            <h3>{{ t("delivery.content") }}</h3>
            <n-tag size="small">{{
              deliveryNames[selected.order.delivery_status]
            }}</n-tag>
          </div>
          <article
            v-for="delivery in selected.deliveries"
            :key="delivery.id"
            class="delivery-item"
          >
            <div class="section-title">
              <span
                >{{ t("delivery.item", { ordinal: delivery.ordinal }) }} ·
                {{ deliveryNames[delivery.status] }}</span
              >
              <n-button
                v-if="delivery.content"
                size="small"
                secondary
                @click="copy(delivery.content)"
                >{{ t("common.copy") }}</n-button
              >
            </div>
            <pre v-if="delivery.content">{{ delivery.content }}</pre>
          </article>
          <n-empty
            v-if="!selected.deliveries.length"
            :description="t('delivery.empty')"
          />
        </section>
      </div>
    </n-spin>
    <n-alert v-if="detailError" type="error" class="space-top">{{
      errorText(detailError)
    }}</n-alert>
  </FormDialog>
</template>

<style scoped>
.orders-workbench {
  grid-template-columns: minmax(0, 1fr);
}
.orders-workbench > * {
  min-width: 0;
}
.orders-filter {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  min-width: 0;
  align-items: end;
}
.orders-filter .field-stack {
  min-width: 0;
  grid-template-columns: minmax(0, 1fr);
}
.date-filter-inputs {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  min-width: 0;
}
.orders-date-filter {
  grid-column: span 2;
}
.orders-pagination {
  flex-wrap: wrap;
}
@media (max-width: 820px) {
  .date-filter-inputs {
    grid-template-columns: minmax(0, 1fr);
  }
  .orders-filter {
    grid-template-columns: minmax(0, 1fr);
  }
  .orders-date-filter {
    grid-column: auto;
  }
}

.order-modal-title,
.order-delivery-title {
  align-items: center;
  display: flex;
  gap: 10px;
}

/* 正文滚动由 FormDialog 负责，避免在原网格外再嵌套滚动区。 */
.order-modal-body {
  display: grid;
  gap: 16px;
}

.order-modal-section {
  display: grid;
  gap: 10px;
}

.order-modal-tools {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.order-modal-tools .n-button {
  height: 56px;
  min-width: 74px;
}

.tool-button-content {
  align-items: center;
  display: inline-flex;
  flex-direction: column;
  font-size: 12px;
  gap: 5px;
  justify-content: center;
  line-height: 1;
}

.tool-icon {
  display: inline-block;
  height: 16px;
  position: relative;
  width: 16px;
}

.tool-icon-check {
  border: 1.6px solid currentColor;
  border-radius: 50%;
}

.tool-icon-check::after {
  border-bottom: 1.8px solid currentColor;
  border-right: 1.8px solid currentColor;
  content: "";
  height: 7px;
  left: 5px;
  position: absolute;
  top: 2px;
  transform: rotate(45deg);
  width: 4px;
}

.tool-icon-notify {
  border: 1.6px solid currentColor;
  border-radius: 50%;
}

.tool-icon-notify::before,
.tool-icon-notify::after {
  border: 1.6px solid currentColor;
  border-left: 0;
  border-bottom: 0;
  content: "";
  height: 5px;
  position: absolute;
  right: -2px;
  top: 2px;
  transform: rotate(45deg);
  width: 5px;
}

.tool-icon-notify::after {
  left: -2px;
  right: auto;
  top: 7px;
  transform: rotate(225deg);
}

.tool-icon-delete::before,
.tool-icon-delete::after {
  background: currentColor;
  border-radius: 999px;
  content: "";
  height: 1.8px;
  left: 1px;
  position: absolute;
  top: 7px;
  width: 14px;
}

.tool-icon-delete::before {
  transform: rotate(45deg);
}

.tool-icon-delete::after {
  transform: rotate(-45deg);
}

.order-modal-section h3 {
  font-size: 15px;
  margin: 0;
}

.detail-grid {
  display: grid;
  gap: 8px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.detail-item {
  background: #f9fafb;
  border: 1px solid #eef0f3;
  border-radius: 8px;
  display: grid;
  gap: 5px;
  min-width: 0;
  padding: 10px;
}

.detail-item span {
  color: #6b7280;
  font-size: 12px;
}

.detail-item strong {
  color: #111827;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.detail-item-wide {
  grid-column: 1 / -1;
}

.detail-copy-row {
  align-items: center;
  display: flex;
  gap: 8px;
  min-width: 0;
}

.detail-copy-row strong {
  flex: 1;
  min-width: 0;
}

@media (max-width: 820px) {
  .detail-grid {
    grid-template-columns: 1fr;
  }
  .detail-copy-row {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
