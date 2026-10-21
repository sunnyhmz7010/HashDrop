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
  NSkeleton,
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
const editingDelivery = ref(""),
  deliveryContent = ref(""),
  activeAction = ref("");
const statusOptions = computed(() => [
  { label: t("orders.all_statuses"), value: "" },
  ...Object.entries(paymentNames.value).map(([value, label]) => ({
    value,
    label,
  })),
]);
const filters = reactive({
  status: "",
  delivery_status: "",
  q: "",
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
      applied.value.delivery_status ||
      applied.value.q.trim() ||
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
let queryTimer: ReturnType<typeof setTimeout> | undefined;
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
    delivery_status: query.delivery_status,
    q: query.q.trim(),
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
    delivery_status: "",
    q: "",
    currency: "",
    min: "",
    max: "",
    from: null,
    to: null,
  });
  applyFilters();
}
function applyFilters() {
  clearTimeout(queryTimer);
  void load(1, filters);
}
// 对齐 HashPay 的搜索防抖；金额、时间等高级条件仍以显式筛选为准。
watch(
  () => [filters.q, filters.status, filters.delivery_status],
  () => {
    clearTimeout(queryTimer);
    queryTimer = setTimeout(() => {
      void load(1, {
        ...lastRequest.query,
        q: filters.q,
        status: filters.status,
        delivery_status: filters.delivery_status,
      });
    }, 250);
  },
  { flush: "sync" },
);
function retryList() {
  void load(lastRequest.page, lastRequest.query);
}
onUnmounted(() => {
  clearTimeout(queryTimer);
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
  activeAction.value = "";
}
watch(show, (value) => {
  if (!value) cancelDetail();
});
onUnmounted(cancelDetail);
async function open(order: Order | string) {
  const id = typeof order === "string" ? order : order.id;
  cancelDetail();
  const current = generation;
  controller = new AbortController();
  selectedID.value = id;
  selected.value = undefined;
  editingDelivery.value = deliveryContent.value = "";
  show.value = true;
  detailError.value = "";
  busy.value = true;
  try {
    const result = await api<OrderDetail>(`/admin/orders/${id}`, {
      signal: controller.signal,
    });
    if (current === generation && selectedID.value === id)
      selected.value = result;
  } catch (e) {
    if (current === generation) detailError.value = errorText(e);
  } finally {
    if (current === generation) busy.value = false;
  }
}
async function action(name: string, body: unknown = {}) {
  if (busy.value || !selected.value) return;
  const id = selected.value.order.id,
    current = generation;
  controller = new AbortController();
  busy.value = true;
  activeAction.value = name;
  detailError.value = "";
  try {
    const result = await api<OrderDetail>(`/admin/orders/${id}/${name}`, {
      method: "POST",
      body,
      signal: controller.signal,
    });
    if (current !== generation) return;
    selected.value = result;
    message.success(t("orders.updated"));
    await load();
    return true;
  } catch (e) {
    if (current === generation) detailError.value = errorText(e);
  } finally {
    if (current === generation) {
      busy.value = false;
      activeAction.value = "";
    }
  }
}
async function fulfill() {
  if (!editingDelivery.value || !deliveryContent.value.trim()) return;
  if (
    await action("fulfill", {
      delivery_id: editingDelivery.value,
      content: deliveryContent.value,
    })
  )
    editingDelivery.value = deliveryContent.value = "";
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
      <form class="orders-filter" @submit.prevent="applyFilters">
        <div class="field-stack orders-search">
          <label class="field-label" for="orders-search">{{
            t("orders.search")
          }}</label>
          <n-input
            v-model:value="filters.q"
            :maxlength="120"
            :input-props="{ id: 'orders-search' }"
            :placeholder="t('orders.search_help')"
            clearable
          />
        </div>
        <div class="field-stack">
          <label class="field-label">{{ t("delivery.status") }}</label>
          <n-select
            v-model:value="filters.delivery_status"
            :aria-label="t('delivery.status')"
            :options="[
              { label: t('orders.all_deliveries'), value: '' },
              ...Object.entries(deliveryNames).map(([value, label]) => ({
                value,
                label,
              })),
            ]"
          />
        </div>
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
  <FormDialog
    v-model:show="show"
    :title="t('orders.detail')"
    :busy="busy && !!selected"
  >
    <template #header>
      <div class="order-modal-title">
        <span>{{ t("orders.detail") }}</span>
        <n-tag v-if="selected" size="small">
          {{ paymentNames[selected.order.payment_status] }}
        </n-tag>
      </div>
    </template>
    <div v-if="busy && !selected" class="order-modal-body" aria-busy="true">
      <div class="row-actions">
        <n-skeleton width="74px" height="56px" :repeat="3" />
      </div>
      <section v-for="section in 2" :key="section" class="order-modal-section">
        <div class="detail-grid">
          <div v-for="field in 4" :key="field" class="detail-item">
            <n-skeleton text width="40%" /><n-skeleton text width="80%" />
          </div>
        </div>
      </section>
    </div>
    <div v-if="selected" class="order-modal-body">
      <div class="order-modal-tools">
        <n-button
          :disabled="busy"
          :loading="activeAction === 'recheck'"
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
            selected.order.kind !== 'manual' &&
            kindNames[selected.order.kind]
          "
          :disabled="busy"
          :loading="activeAction === 'retry'"
          secondary
          size="small"
          @click="action('retry')"
        >
          <span class="tool-button-content">
            <span class="tool-icon tool-icon-notify" aria-hidden="true"></span>
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
            <span class="tool-icon tool-icon-delete" aria-hidden="true"></span>
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
              }}<small v-if="discount.code"> · {{ discount.code }}</small></span
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
        <template v-if="selected.order.buyer_note">
          <h3>{{ t("orders.buyer_note") }}</h3>
          <pre class="order-note">{{ selected.order.buyer_note }}</pre>
        </template>
        <template v-if="selected.order.delivery_note">
          <h3>{{ t("product.delivery_note") }}</h3>
          <p class="order-note">{{ selected.order.delivery_note }}</p>
        </template>
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
          <form
            v-else-if="editingDelivery === delivery.id"
            class="grid"
            @submit.prevent="fulfill"
          >
            <label
              class="field-label"
              :for="'delivery-content-' + delivery.id"
              >{{ t("delivery.content") }}</label
            >
            <n-input
              v-model:value="deliveryContent"
              type="textarea"
              :maxlength="65536"
              :autosize="{ minRows: 4, maxRows: 12 }"
              :disabled="busy"
              :input-props="{ id: 'delivery-content-' + delivery.id }"
              :placeholder="t('delivery.fulfill_help')"
            />
            <small class="field-help">{{ t("delivery.immutable_help") }}</small>
            <div class="row-actions">
              <n-button
                type="primary"
                attr-type="submit"
                :loading="busy"
                :disabled="!deliveryContent.trim() || busy"
                >{{ t("delivery.fulfill") }}</n-button
              >
              <n-button
                :disabled="busy"
                @click="
                  editingDelivery = '';
                  deliveryContent = '';
                "
                >{{ t("common.cancel") }}</n-button
              >
            </div>
          </form>
          <n-button
            v-else-if="
              selected.order.payment_status === 'paid' &&
              delivery.status !== 'done'
            "
            secondary
            size="small"
            :disabled="busy || !!editingDelivery"
            @click="
              editingDelivery = delivery.id;
              deliveryContent = '';
            "
            >{{ t("delivery.fulfill") }}</n-button
          >
        </article>
        <n-empty
          v-if="!selected.deliveries.length"
          :description="t('delivery.empty')"
        />
      </section>
    </div>
    <n-alert v-if="detailError" type="error" class="space-top">
      {{ errorText(detailError) }}
      <n-button
        v-if="!selected"
        text
        :loading="busy"
        @click="open(selectedID)"
        >{{ t("common.retry") }}</n-button
      >
    </n-alert>
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
.orders-search {
  grid-column: span 2;
}
.order-note {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font: inherit;
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
  .orders-search {
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
