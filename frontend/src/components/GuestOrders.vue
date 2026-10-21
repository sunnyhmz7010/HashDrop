<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useDialog, useMessage } from "naive-ui";
import { api, errorText, type OrderDetail } from "../api";
import {
  clearOrderHistory,
  orderHistory,
  rememberOrder,
  historyCleared,
} from "../claim";
import { t } from "../i18n";
import HistoryList from "./HistoryList.vue";
import OrderRow from "./OrderRow.vue";
import OrderQueryDialog from "./OrderQueryDialog.vue";

const router = useRouter(),
  dialog = useDialog(),
  message = useMessage();
const page = ref(1),
  queryOpen = ref(false),
  loading = ref(false),
  error = ref("");
const pageSize = 20;
const items = computed(() =>
  orderHistory.value
    .slice((page.value - 1) * pageSize, page.value * pageSize)
    .map((record) => record.order),
);
let generation = 0;
let controller: AbortController | undefined;
async function load(next = page.value) {
  controller?.abort();
  const current = ++generation;
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  error.value = "";
  try {
    const failures: unknown[] = [];
    page.value = Math.min(
      Math.max(1, next),
      Math.max(1, Math.ceil(orderHistory.value.length / pageSize)),
    );
    const records = orderHistory.value.slice(
      (page.value - 1) * pageSize,
      page.value * pageSize,
    );
    let position = 0;
    // 有限并发，避免大量历史凭证同时占用查询失败预算。
    await Promise.all(
      Array.from({ length: Math.min(3, records.length) }, async () => {
        while (position < records.length && !signal.aborted) {
          const record = records[position++];
          try {
            const detail = await api<OrderDetail>(
              `/orders/${encodeURIComponent(record.order.id)}`,
              { token: record.key, signal },
            );
            if (current === generation) rememberOrder(detail.order, record.key);
          } catch (cause) {
            if (!signal.aborted) failures.push(cause);
          }
        }
      }),
    );
    if (current === generation && failures.length)
      error.value = t("center.refresh_error", {
        count: failures.length,
        error: errorText(failures[0]),
      });
  } catch (cause) {
    if (current === generation) error.value = errorText(cause);
  } finally {
    if (current === generation) loading.value = false;
  }
}
function clear() {
  dialog.warning({
    title: t("center.clear_history"),
    content: t("center.clear_warning"),
    positiveText: t("center.clear_confirm"),
    negativeText: t("common.cancel"),
    onPositiveClick: () => {
      generation++;
      controller?.abort();
      loading.value = false;
      if (!clearOrderHistory()) {
        message.error(t("center.clear_failed"));
        return false;
      }
      page.value = 1;
      error.value = "";
      message.success(t("center.history_cleared"));
    },
  });
}
onMounted(() => load());
watch(historyCleared, () => {
  generation++;
  controller?.abort();
  loading.value = false;
  page.value = 1;
  error.value = "";
});
onUnmounted(() => {
  generation++;
  controller?.abort();
});
</script>

<template>
  <HistoryList
    :title="t('center.history')"
    :items="items"
    :columns="[
      t('orders.product_column'),
      t('orders.payment_status'),
      t('common.amount'),
      t('orders.delivery_column'),
      t('common.created_at'),
    ]"
    :loading="loading"
    :error="error"
    :empty-text="t('center.history_empty')"
    :clear-text="t('center.clear_history')"
    :total="orderHistory.length"
    :total-text="t('orders.total_count', { total: orderHistory.length })"
    :page="page"
    @refresh="load()"
    @query="queryOpen = true"
    @clear="clear"
    @update:page="load"
  >
    <template #row="{ item }">
      <OrderRow
        :order="item"
        @open="router.push('/center/orders/' + encodeURIComponent(item.id))"
      />
    </template>
  </HistoryList>
  <OrderQueryDialog
    v-model:show="queryOpen"
    @found="
      (detail) =>
        router.push('/center/orders/' + encodeURIComponent(detail.order.id))
    "
  />
</template>
