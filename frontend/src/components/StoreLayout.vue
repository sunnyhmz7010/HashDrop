<!-- 基于 HashPay src/app/pages/Pay.vue 修改：保留页框，顶栏共用 TinyFaka 品牌与语言入口。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { NLayout, NLayoutContent } from "naive-ui";
import AppHeader from "./AppHeader.vue";
</script>

<template>
  <n-layout class="shell checkout-shell">
    <AppHeader
      ><template #actions><slot name="actions" /></template
    ></AppHeader>
    <n-layout-content class="checkout-content"><slot /></n-layout-content>
  </n-layout>
</template>
