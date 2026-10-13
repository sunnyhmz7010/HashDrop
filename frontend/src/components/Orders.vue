<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NModal,
  NSelect,
  NSpin,
  NTag,
  useDialog,
  useMessage,
} from "naive-ui";
import {
  api,
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
    await navigator.clipboard.writeText(value);
    message.success("已复制");
  } catch {
    message.warning("请手动复制内容");
  }
}
</script>
<template>
  <div class="section-title space-bottom">
    <h2>订单管理</h2>
    <n-select
      class="status-filter"
      v-model:value="status"
      :options="statusOptions"
      :disabled="loading"
      @update:value="() => load(1)"
      aria-label="付款状态筛选"
    />
  </div>
  <n-alert v-if="error" type="error" class="space-bottom"
    >{{ error }} <n-button text @click="load()">重试</n-button></n-alert
  ><n-spin :show="loading"
    ><div v-if="items.length" class="panel table-panel">
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>订单号</th>
              <th>商品 / 数量</th>
              <th>金额</th>
              <th>付款</th>
              <th>交付</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="order in items" :key="order.id">
              <td class="code-cell">{{ order.id }}</td>
              <td class="name-cell">
                {{ order.product_name }} × {{ order.quantity }}
              </td>
              <td>{{ money(order.total, order.currency) }}</td>
              <td>{{ paymentNames[order.payment_status] }}</td>
              <td>{{ deliveryNames[order.delivery_status] }}</td>
              <td>
                <n-button size="small" @click="open(order)">查看</n-button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <div v-else-if="!loading && !error" class="panel empty-panel">
      <n-empty :description="status ? '没有符合条件的订单' : '暂无订单'" /></div
  ></n-spin>
  <div class="pagination">
    <n-button :disabled="page === 1 || loading" @click="load(page - 1)"
      >上一页</n-button
    ><span>第 {{ page }} 页</span
    ><n-button :disabled="!more || loading" @click="load(page + 1)"
      >下一页</n-button
    >
  </div>
  <n-modal
    v-model:show="show"
    preset="card"
    title="订单详情"
    class="form-modal"
    :mask-closable="!busy"
    :closable="!busy"
    :close-on-esc="!busy"
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
    }}</n-alert></n-modal
  >
</template>
