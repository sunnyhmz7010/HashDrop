<script setup lang="ts">
import { onMounted, watch } from "vue";
import {
  NConfigProvider,
  NMessageProvider,
  NDialogProvider,
  zhCN,
  dateZhCN,
} from "naive-ui";
import { api, initialized, site } from "./api";
watch(
  () => site.value.title,
  (title) => {
    document.title = title;
  },
  { immediate: true },
);
onMounted(async () => {
  try {
    site.value = await api("/site");
  } catch {
    console.warn("站点信息加载失败，使用默认名称");
  }
});
</script>
<template>
  <n-config-provider
    :locale="zhCN"
    :date-locale="dateZhCN"
    :theme-overrides="{
      common: {
        primaryColor: '#18a058',
        borderRadius: '8px',
        fontFamily: 'Inter, system-ui, -apple-system, sans-serif',
      },
    }"
  >
    <n-message-provider
      ><n-dialog-provider>
        <header class="site-header">
          <div class="header-inner">
            <router-link class="brand" to="/"
              ><span class="brand-mark">T</span>{{ site.title }}</router-link
            >
            <nav v-if="initialized">
              <router-link to="/">商城</router-link
              ><router-link to="/claim">领取订单</router-link
              ><router-link to="/admin">管理</router-link>
            </nav>
          </div>
        </header>
        <main class="shell"><router-view /></main>
        <footer class="site-footer">
          {{ site.title }} · 自动交付，凭证领取
        </footer>
      </n-dialog-provider></n-message-provider
    >
  </n-config-provider>
</template>
