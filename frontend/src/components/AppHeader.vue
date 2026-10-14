<!-- 基于 HashPay Admin.vue 的顶栏结构修改，商城与后台共用尺寸和品牌入口。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { NLayoutHeader } from "naive-ui";
import { site } from "../api";
import LocaleSwitch from "./LocaleSwitch.vue";
</script>

<template>
  <n-layout-header class="topbar">
    <router-link to="/" class="brand" :title="site.title">{{
      site.title
    }}</router-link>
    <div class="topbar-actions"><slot name="actions" /><LocaleSwitch /></div>
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
</style>
