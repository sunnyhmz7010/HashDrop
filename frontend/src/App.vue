<script setup lang="ts">
import { onMounted, watch } from "vue";
import {
  NConfigProvider,
  NMessageProvider,
  NDialogProvider,
  zhCN,
  dateZhCN,
} from "naive-ui";
import { api, site } from "./api";
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
  <n-config-provider :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider
      ><n-dialog-provider> <router-view /> </n-dialog-provider
    ></n-message-provider>
  </n-config-provider>
</template>
