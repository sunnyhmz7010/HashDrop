<script setup lang="ts">
import { onMounted, ref } from "vue";
import { NAlert, NButton, NSpin } from "naive-ui";
import { api, errorText } from "../api";
const data = ref<Record<string, number>>(),
  error = ref(""),
  busy = ref(false);
const labels = [
  ["products", "商品数量"],
  ["stock", "可用库存"],
  ["orders", "订单总数"],
  ["paid", "已付款订单"],
  ["manual", "需要处理"],
];
async function load() {
  busy.value = true;
  error.value = "";
  try {
    data.value = await api("/admin/overview");
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="grid overview-page">
    <div class="section-title">
      <h2>概览</h2>
      <n-button :loading="busy" @click="load">刷新</n-button>
    </div>
    <n-alert v-if="error" type="error" class="space-bottom"
      >{{ error }} <n-button text @click="load">重试</n-button></n-alert
    ><n-spin :show="busy"
      ><div class="metric-row">
        <article v-for="[key, label] in labels" :key="key" class="metric">
          <span class="muted">{{ label }}</span
          ><strong>{{ data?.[key!] ?? "—" }}</strong>
        </article>
      </div></n-spin
    >
  </div>
</template>
