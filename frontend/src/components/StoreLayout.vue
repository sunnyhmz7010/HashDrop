<!-- 基于 HashPay src/app/pages/Pay.vue 修改：保留页框，顶栏共用 HashDrop 品牌与语言入口。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { NButton, NLayout, NLayoutContent } from "naive-ui";
import { t } from "../i18n";
import AppHeader from "./AppHeader.vue";
</script>

<template>
  <n-layout class="shell checkout-shell">
    <AppHeader
      ><template #actions
        ><router-link to="/center"
          ><n-button size="small" secondary>{{
            t("center.title")
          }}</n-button></router-link
        ><slot name="actions" /></template
    ></AppHeader>
    <n-layout-content class="checkout-content"><slot /></n-layout-content>
  </n-layout>
</template>
