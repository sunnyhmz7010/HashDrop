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
  <n-alert v-if="error" type="error" class="space-bottom"
    >{{ error }} <n-button text @click="load">重试</n-button></n-alert
  ><n-spin :show="busy"
    ><div class="metrics">
      <article v-for="[key, label] in labels" :key="key" class="panel metric">
        <span class="muted">{{ label }}</span
        ><strong>{{ data?.[key!] ?? "—" }}</strong>
      </article>
    </div></n-spin
  >
  <section class="panel space-top">
    <h2>日常管理</h2>
    <p class="muted">
      先配置 HashPay，再上架商品。卡密和账号商品需要导入库存后才能购买。
    </p>
    <div class="quick-links">
      <router-link to="/admin/products">管理商品与库存 →</router-link
      ><router-link to="/admin/orders">查看待处理订单 →</router-link
      ><router-link to="/admin/settings">检查支付设置 →</router-link>
    </div>
  </section>
</template>
