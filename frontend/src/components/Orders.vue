<!-- 详情布局基于 HashPay src/app/components/OrderModal.vue 与 styles.scss 修改。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。
     保留原分区、信息网格和操作栏，字段替换为 TinyFaka 订单与交付数据。 -->
<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from "vue";
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
  detailError = ref(""),
  manualContent = ref(""),
  manualDelivery = ref<string | null>(null);
const statusOptions = [
  { label: "全部付款状态", value: "" },
  ...Object.entries(paymentNames).map(([value, label]) => ({ value, label })),
];
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
  manualContent.value = "";
  manualDelivery.value = null;
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
async function action(name: string, body: unknown = {}) {
  if (busy.value || !selected.value) return;
  const id = selected.value.order.id,
    current = generation;
  controller = new AbortController();
  busy.value = true;
  detailError.value = "";
  try {
    const result = await api<OrderDetail>(`/admin/orders/${id}/${name}`, {
      method: "POST",
      body,
      signal: controller.signal,
    });
    if (current !== generation) return;
    selected.value = result;
    message.success("订单状态已更新");
    manualContent.value = "";
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
    title: "删除订单？",
    content:
      "该订单将从后台列表移除。HashPay 订单、库存预留和购买者领取链接继续保留，付款状态仍由网关决定。",
    positiveText: "删除订单",
    negativeText: "取消",
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
        message.success("订单已删除");
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
    message.success("已复制");
  } catch {
    message.warning("请手动复制内容");
  }
}
</script>
<template>
  <div class="orders-view grid">
    <div class="section-title">
      <h2>订单管理</h2>
      <n-button :loading="loading" @click="load()">刷新列表</n-button>
    </div>
    <n-alert v-if="error" type="error"
      >{{ error }} <n-button text @click="load()">重试</n-button></n-alert
    >
    <div class="orders-workbench">
      <div class="orders-filter">
        <n-select
          class="status-filter"
          v-model:value="status"
          :options="statusOptions"
          :disabled="loading"
          @update:value="() => load(1)"
          aria-label="付款状态筛选"
        />
      </div>
      <n-spin :show="loading"
        ><div class="orders-table">
          <div class="orders-table-head order-row--plain">
            <span>订单 / 商品</span><span>付款状态</span><span>金额</span
            ><span>交付 / 数量</span><span>创建时间</span>
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
              :description="status ? '没有符合条件的订单' : '暂无订单'"
            />
          </div></div
      ></n-spin>
      <div class="orders-pagination">
        <span>第 {{ page }} 页</span>
        <div class="topbar-actions">
          <n-button :disabled="page === 1 || loading" @click="load(page - 1)"
            >上一页</n-button
          ><n-button :disabled="!more || loading" @click="load(page + 1)"
            >下一页</n-button
          >
        </div>
      </div>
    </div>
  </div>
  <FormDialog v-model:show="show" title="订单详情" :busy="busy">
    <template #header>
      <div class="order-modal-title">
        <span>订单详情</span>
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
              <span>刷新状态</span>
            </span>
          </n-button>
          <n-button
            v-if="
              selected.order.payment_status === 'paid' &&
              selected.order.delivery_status !== 'done'
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
              <span>重试交付</span>
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
              <span>删除订单</span>
            </span>
          </n-button>
        </div>

        <n-alert v-if="selected.order.last_error" type="warning">
          {{ selected.order.last_error }}
        </n-alert>

        <section class="order-modal-section">
          <div class="detail-grid">
            <div class="detail-item">
              <span>订单号</span>
              <div class="detail-copy-row">
                <strong>{{ selected.order.id }}</strong>
                <n-button
                  size="small"
                  secondary
                  @click="copy(selected.order.id)"
                  >复制</n-button
                >
              </div>
            </div>
            <div class="detail-item">
              <span>HashPay 订单号</span>
              <div class="detail-copy-row">
                <strong>{{ selected.order.gateway_id || "--" }}</strong>
                <n-button
                  v-if="selected.order.gateway_id"
                  size="small"
                  secondary
                  @click="copy(selected.order.gateway_id)"
                  >复制</n-button
                >
              </div>
            </div>
            <div class="detail-item detail-item-wide">
              <span>订单信息</span>
              <strong
                >{{ selected.order.product_name }} ×
                {{ selected.order.quantity }}</strong
              >
            </div>
            <div class="detail-item">
              <span>创建时间</span>
              <strong>{{
                new Date(selected.order.created_at * 1000).toLocaleString(
                  "zh-CN",
                  { hour12: false },
                )
              }}</strong>
            </div>
            <div class="detail-item">
              <span>商品类型</span>
              <strong>{{ kindNames[selected.order.kind] }}</strong>
            </div>
          </div>
        </section>

        <section class="order-modal-section">
          <h3>支付信息</h3>
          <div class="detail-grid">
            <div class="detail-item">
              <span>订单金额</span>
              <strong>{{
                money(selected.order.total, selected.order.currency)
              }}</strong>
            </div>
            <div class="detail-item">
              <span>商品单价</span>
              <strong>{{
                money(selected.order.unit_price, selected.order.currency)
              }}</strong>
            </div>
            <div class="detail-item detail-item-wide">
              <span>支付有效期</span>
              <strong>{{
                selected.order.expires_at
                  ? new Date(selected.order.expires_at * 1000).toLocaleString(
                      "zh-CN",
                      { hour12: false },
                    )
                  : "--"
              }}</strong>
            </div>
          </div>
        </section>

        <section class="order-modal-section">
          <div class="order-delivery-title">
            <h3>交付内容</h3>
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
                >第 {{ delivery.ordinal }} 份 ·
                {{ deliveryNames[delivery.status] }}</span
              >
              <n-button
                v-if="delivery.content"
                size="small"
                secondary
                @click="copy(delivery.content)"
                >复制</n-button
              >
            </div>
            <pre v-if="delivery.content">{{ delivery.content }}</pre>
          </article>
          <n-empty
            v-if="!selected.deliveries.length"
            description="暂无交付内容"
          />
        </section>

        <section
          v-if="
            selected.order.kind === 'api' &&
            selected.order.payment_status === 'paid' &&
            selected.deliveries.some((d) => d.status === 'manual')
          "
          class="order-modal-section"
        >
          <h3>核实外部开通结果后人工交付</h3>
          <n-select
            v-model:value="manualDelivery"
            :options="
              selected.deliveries
                .filter((d) => d.status === 'manual')
                .map((d) => ({ value: d.id, label: `第 ${d.ordinal} 份` }))
            "
            placeholder="选择待处理份数"
          />
          <n-input
            v-model:value="manualContent"
            type="textarea"
            placeholder="填写已核实的交付结果"
          />
          <div class="row-actions">
            <n-button
              type="primary"
              :disabled="!manualDelivery || !manualContent || busy"
              @click="
                action('retry', {
                  delivery_id: manualDelivery,
                  content: manualContent,
                })
              "
              >提交人工交付</n-button
            >
          </div>
        </section>
      </div>
    </n-spin>
    <n-alert v-if="detailError" type="error" class="space-top">{{
      detailError
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
