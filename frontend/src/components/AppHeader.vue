<!-- 基于 HashPay Admin.vue 的顶栏结构修改，商城与后台共用尺寸和品牌入口。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { NButton, NLayoutHeader } from "naive-ui";
import { site } from "../api";
import { t } from "../i18n";
import LocaleSwitch from "./LocaleSwitch.vue";
</script>

<template>
  <n-layout-header class="topbar">
    <router-link to="/" class="brand" :title="site.title">{{
      site.title
    }}</router-link>
    <div class="topbar-actions">
      <slot name="before-actions" /><LocaleSwitch /><slot name="actions"
        ><router-link to="/center"
          ><n-button size="small" secondary>{{
            t("center.title")
          }}</n-button></router-link
        ></slot
      >
    </div>
  </n-layout-header>
</template>

<style scoped>
.brand {
  flex: 1;
  min-width: 0;
}
.topbar-actions {
  flex: 0 0 auto;
}

@media (max-width: 820px) {
  .topbar {
    gap: 8px;
  }
  .topbar-actions {
    gap: 6px;
  }
  .topbar-actions :deep(.locale-switch) {
    flex-basis: 5.5rem;
    width: 5.5rem;
  }
}
</style>
