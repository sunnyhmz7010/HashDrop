<!-- 详情布局基于 HashPay src/app/components/OrderModal.vue 与 styles.scss 修改。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。
     保留原分区、信息网格和操作栏，字段替换为 HashDrop 订单与交付数据。 -->
<script setup lang="ts">
import { formatTime, t } from "../i18n";

import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NSelect,
  NSpin,
  NTag,
  useDialog,
  useMessage,
} from "naive-ui";
import FormDialog from "./FormDialog.vue";
import OrderRow from "./OrderRow.vue";
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
  more = ref(false),
  status = ref(""),
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
async function load(next = page.value) {
  if (loading.value) return;
  loading.value = true;
  error.value = "";
  try {
    const result = await api<{ items: Order[]; has_more: boolean }>(
      `/admin/orders?page=${next}&status=${status.value}`,
    );
    items.value = result.items;
    page.value = next;
    more.value = result.has_more;
  } catch (e) {
    error.value = errorText(e);
  } finally {
    loading.value = false;
  }
}
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
      <h2>{{ t("orders.title") }}</h2>
      <n-button :loading="loading" @click="load()">{{
        t("orders.refresh_list")
      }}</n-button>
    </div>
    <n-alert v-if="error" type="error"
      >{{ errorText(error) }}
      <n-button text @click="load()">{{ t("common.retry") }}</n-button></n-alert
    >
    <div class="orders-workbench">
      <div class="orders-filter">
        <n-select
          class="status-filter"
          v-model:value="status"
          :options="statusOptions"
          :disabled="loading"
          @update:value="() => load(1)"
          :aria-label="t('orders.status_filter')"
        />
      </div>
      <n-spin :show="loading"
        ><div class="orders-table">
          <div class="orders-table-head order-row--plain">
            <span>{{ t("orders.product_column") }}</span
            ><span>{{ t("orders.payment_status") }}</span
            ><span>{{ t("common.amount") }}</span
            ><span>{{ t("orders.delivery_column") }}</span
            ><span>{{ t("common.created_at") }}</span>
          </div>
          <OrderRow
            v-for="order in items"
            :key="order.id"
            :order="order"
            @open="open(order)"
          />
          <div
            v-if="!items.length && !loading && !error"
            class="orders-table-empty"
          >
            <n-empty
              :description="
                status ? t('orders.filtered_empty') : t('orders.empty')
              "
            />
          </div></div
      ></n-spin>
      <div class="orders-pagination">
        <span>{{ t("pagination.page", { page }) }}</span>
        <div class="topbar-actions">
          <n-button :disabled="page === 1 || loading" @click="load(page - 1)">{{
            t("pagination.previous")
          }}</n-button
          ><n-button :disabled="!more || loading" @click="load(page + 1)">{{
            t("pagination.next")
          }}</n-button>
        </div>
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
