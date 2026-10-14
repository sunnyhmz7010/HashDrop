<!-- 基于 HashPay src/app/pages/Pay.vue 修改：保留页框，导航接入 TinyFaka。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { NButton, NLayout, NLayoutContent, NLayoutHeader } from "naive-ui";
import { site } from "../api";
</script>

<template>
  <n-layout class="shell checkout-shell">
    <n-layout-header class="checkout-topbar">
      <div class="checkout-topbar-inner">
        <router-link to="/" class="checkout-brand"
          ><strong>{{ site.title }}</strong></router-link
        >
        <nav class="topbar-actions" aria-label="商城导航">
          <router-link to="/"
            ><n-button quaternary size="small">商城</n-button></router-link
          >
          <router-link to="/claim"
            ><n-button quaternary size="small">领取订单</n-button></router-link
          >
          <router-link to="/admin"
            ><n-button quaternary size="small">管理</n-button></router-link
          >
        </nav>
      </div>
    </n-layout-header>
    <n-layout-content class="checkout-content"><slot /></n-layout-content>
  </n-layout>
</template>

<style scoped>
/* 防止长站名挤出窄屏导航；其余尺寸沿用 HashPay。 */
.checkout-brand {
  flex: 1;
}
.topbar-actions {
  flex-shrink: 0;
  gap: 0;
}
.checkout-topbar-inner {
  gap: 8px;
}
</style>
