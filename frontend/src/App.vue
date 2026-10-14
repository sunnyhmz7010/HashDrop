<script setup lang="ts">
import { locale, t } from "./i18n";

import { computed, onMounted, watch } from "vue";
import {
  NConfigProvider,
  NMessageProvider,
  NDialogProvider,
  zhCN,
  dateZhCN,
  enUS,
  dateEnUS,
} from "naive-ui";
import { api, site } from "./api";
const naiveLocale = computed(() => (locale.value === "zh-CN" ? zhCN : enUS));
const naiveDateLocale = computed(() =>
  locale.value === "zh-CN" ? dateZhCN : dateEnUS,
);
watch(
  locale,
  (value) => {
    document.documentElement.lang = value;
  },
  { immediate: true },
);
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
    console.warn(t("site.load_failed"));
  }
});
</script>
<template>
  <n-config-provider :locale="naiveLocale" :date-locale="naiveDateLocale">
    <n-message-provider
      ><n-dialog-provider> <router-view /> </n-dialog-provider
    ></n-message-provider>
  </n-config-provider>
</template>
