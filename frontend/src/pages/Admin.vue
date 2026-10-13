<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { NButton, useMessage } from "naive-ui";
import { api, csrf, errorText } from "../api";
import Overview from "../components/Overview.vue";
import Products from "../components/Products.vue";
import Orders from "../components/Orders.vue";
import Settings from "../components/Settings.vue";
const route = useRoute(),
  router = useRouter(),
  message = useMessage(),
  busy = ref(false);
const section = computed(() => String(route.params.section ?? "overview"));
const sections = [
  { id: "overview", name: "概览" },
  { id: "products", name: "商品与库存" },
  { id: "orders", name: "订单" },
  { id: "settings", name: "系统设置" },
];
async function logout() {
  busy.value = true;
  try {
    await api("/admin/logout", { method: "POST" });
    csrf.value = "";
    await router.push("/admin/login");
  } catch (e) {
    message.error(errorText(e));
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <div class="admin-heading">
    <div>
      <p class="eyebrow">管理后台</p>
      <h1>店铺管理</h1>
    </div>
    <n-button :loading="busy" @click="logout">退出登录</n-button>
  </div>
  <nav class="admin-nav">
    <router-link
      v-for="item in sections"
      :key="item.id"
      :to="`/admin/${item.id}`"
      :class="{ active: section === item.id }"
      >{{ item.name }}</router-link
    >
  </nav>
  <Products v-if="section === 'products'" /><Orders
    v-else-if="section === 'orders'"
  /><Settings v-else-if="section === 'settings'" /><Overview v-else />
</template>
