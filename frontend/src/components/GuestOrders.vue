<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NPagination, useDialog, useMessage } from "naive-ui";
import { api, errorText, type OrderDetail } from "../api";
import {
  clearOrderHistory,
  orderHistory,
  rememberOrder,
  historyCleared,
} from "../claim";
import { t } from "../i18n";
import OrderTable from "./OrderTable.vue";

const router = useRouter(),
  dialog = useDialog(),
  message = useMessage();
const page = ref(1),
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
  <div class="grid guest-history">
    <div class="section-title">
      <h2>{{ t("center.history") }}</h2>
      <div class="row-actions">
        <n-button :loading="loading" @click="load()">{{
          t("common.refresh")
        }}</n-button>
        <n-button
          type="primary"
          @click="router.push({ path: '/center/query', query: { new: '1' } })"
          >{{ t("center.import_key") }}</n-button
        >
        <n-button :disabled="loading || !orderHistory.length" @click="clear">{{
          t("center.clear_history")
        }}</n-button>
      </div>
    </div>
    <p class="muted history-help">{{ t("center.history_help") }}</p>
    <n-alert v-if="error" type="warning"
      >{{ error
      }}<n-button text @click="load()">{{
        t("common.retry")
      }}</n-button></n-alert
    >
    <div class="orders-workbench">
      <OrderTable
        :items="items"
        :loading="loading"
        :empty-text="t('center.history_empty')"
        :show-empty="!error"
        @open="
          (order) =>
            router.push('/center/orders/' + encodeURIComponent(order.id))
        "
      />
      <div class="orders-pagination">
        <span>{{
          t("orders.total_count", { total: orderHistory.length })
        }}</span
        ><n-pagination
          :page="page"
          :page-size="pageSize"
          :item-count="orderHistory.length"
          :page-slot="5"
          size="small"
          :disabled="loading"
          @update:page="(next) => load(next)"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.orders-workbench {
  grid-template-columns: minmax(0, 1fr);
}
.orders-workbench > * {
  min-width: 0;
}
.orders-pagination {
  flex-wrap: wrap;
}
.history-help {
  margin: 0;
}
@media (max-width: 820px) {
  .section-title {
    flex-wrap: wrap;
  }
}
</style>
