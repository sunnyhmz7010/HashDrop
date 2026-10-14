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
function release() {
  dialog.warning({
    title: "确认关闭并释放库存？",
    content:
      "系统将再次查询 HashPay，只有返回 invalid 才能释放；expired 不允许释放。HashPay 原生商户 API 不提供取消接口。",
    positiveText: "确认查询并释放",
    negativeText: "取消",
    onPositiveClick: () => action("release", { confirm: true }),
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
      <n-button :loading="loading" @click="load()">刷新</n-button>
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
  <FormDialog v-model:show="show" title="订单详情" :busy="busy"
    ><n-spin :show="busy"
      ><template v-if="selected"
        ><h2>
          {{ selected.order.product_name }} × {{ selected.order.quantity }}
        </h2>
        <p class="code-cell">{{ selected.order.id }}</p>
        <div class="row-actions">
          <n-tag>{{ paymentNames[selected.order.payment_status] }}</n-tag
          ><n-tag>{{ deliveryNames[selected.order.delivery_status] }}</n-tag>
        </div>
        <p>{{ money(selected.order.total, selected.order.currency) }}</p>
        <n-alert
          v-if="selected.order.last_error"
          type="warning"
          class="space-bottom"
          >{{ selected.order.last_error }}</n-alert
        >
        <div class="row-actions space-bottom">
          <n-button :disabled="busy" @click="action('recheck')"
            >重新核实付款</n-button
          ><n-button
            v-if="
              selected.order.payment_status === 'paid' &&
              selected.order.delivery_status !== 'done'
            "
            :disabled="busy"
            @click="action('retry')"
            >重试交付</n-button
          ><n-button
            v-if="
              selected.order.payment_status === 'review' &&
              selected.order.reservation_status === 'reserved'
            "
            :disabled="busy"
            type="error"
            secondary
            @click="release"
            >关闭并释放库存</n-button
          >
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
            ><n-button
              v-if="delivery.content"
              size="small"
              @click="copy(delivery.content)"
              >复制</n-button
            >
          </div>
          <pre v-if="delivery.content">{{ delivery.content }}</pre>
        </article>
        <template
          v-if="
            selected.order.kind === 'api' &&
            selected.order.payment_status === 'paid' &&
            selected.deliveries.some((d) => d.status === 'manual')
          "
          ><h3>核实外部开通结果后人工交付</h3>
          <n-select
            v-model:value="manualDelivery"
            :options="
              selected.deliveries
                .filter((d) => d.status === 'manual')
                .map((d) => ({ value: d.id, label: `第 ${d.ordinal} 份` }))
            "
            placeholder="选择待处理份数"
          /><n-input
            v-model:value="manualContent"
            class="space-top"
            type="textarea"
            placeholder="填写已核实的交付结果"
          /><n-button
            class="space-top"
            type="primary"
            :disabled="!manualDelivery || !manualContent || busy"
            @click="
              action('retry', {
                delivery_id: manualDelivery,
                content: manualContent,
              })
            "
            >提交人工交付</n-button
          ></template
        ></template
      ></n-spin
    ><n-alert v-if="detailError" type="error" class="space-top">{{
      detailError
    }}</n-alert></FormDialog
  >
</template>
