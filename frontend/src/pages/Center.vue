<script setup lang="ts">
import { t } from "../i18n";

import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  NButton,
  NAlert,
  NDrawer,
  NDrawerContent,
  NLayout,
  NLayoutContent,
  NLayoutSider,
  NMenu,
  useMessage,
} from "naive-ui";
import { api, csrf, errorText, adminAuthenticated, sessionError } from "../api";
import { historyStorageError } from "../claim";
import { version } from "../../package.json";
import Overview from "../components/Overview.vue";
import Products from "../components/Products.vue";
import Inventories from "../components/Inventories.vue";
import Orders from "../components/Orders.vue";
import Promotions from "../components/Promotions.vue";
import Settings from "../components/Settings.vue";
import Donations from "../components/Donations.vue";
import AppHeader from "../components/AppHeader.vue";
import GuestOrders from "../components/GuestOrders.vue";
import Order from "./Order.vue";
import Login from "./Login.vue";
const route = useRoute(),
  router = useRouter(),
  message = useMessage(),
  busy = ref(false),
  navOpen = ref(false);
const section = computed(() =>
  String(
    route.params.section ?? (adminAuthenticated.value ? "overview" : "orders"),
  ),
);
const menuOptions = computed(() =>
  adminAuthenticated.value
    ? [
        { key: "overview", label: t("nav.overview") },
        { key: "products", label: t("nav.products") },
        { key: "inventories", label: t("nav.inventories") },
        { key: "orders", label: t("nav.orders") },
        { key: "promotions", label: t("nav.promotions") },
        { key: "donation", label: t("nav.donation") },
        { key: "settings", label: t("nav.settings") },
      ]
    : [
        { key: "orders", label: t("center.history") },
        { key: "query", label: t("center.query") },
      ],
);
watch(adminAuthenticated, (value) => {
  if (value === false && !["orders", "query", "login"].includes(section.value))
    void router.replace("/center/login");
});
function openPage(key: string) {
  navOpen.value = false;
  void router.push(`/center/${key}`);
}
async function logout() {
  if (busy.value) return;
  busy.value = true;
  try {
    await api("/admin/logout", { method: "POST" });
    csrf.value = "";
    adminAuthenticated.value = false;
    await router.push("/center/login");
  } catch (e) {
    message.error(errorText(e));
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <!-- 基于 HashPay src/app/pages/Admin.vue 修改，接入 HashDrop 路由和密码会话。
       Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
  <n-layout class="shell">
    <AppHeader>
      <template #before-actions>
        <n-button
          class="mobile-menu-button"
          circle
          quaternary
          size="small"
          :aria-label="t('nav.open_menu')"
          @click="navOpen = true"
        >
          <span class="hamburger-icon" aria-hidden="true"></span>
        </n-button>
      </template>
      <template #actions>
        <n-button
          v-if="adminAuthenticated"
          size="small"
          secondary
          :loading="busy"
          @click="logout"
          >{{ t("nav.logout") }}</n-button
        >
        <router-link v-else-if="section !== 'login'" to="/center/login"
          ><n-button size="small" secondary>{{
            t("center.admin_login")
          }}</n-button></router-link
        >
      </template>
    </AppHeader>
    <n-layout-content class="workspace-layout" content-class="workspace-page">
      <n-drawer v-model:show="navOpen" placement="left" :width="260">
        <n-drawer-content closable :title="t('nav.menu')">
          <div class="workspace-nav workspace-nav--drawer">
            <n-menu
              :value="section"
              :options="menuOptions"
              @update:value="openPage"
            />
            <div class="workspace-release">
              <span>HashDrop v{{ version }}</span>
            </div>
          </div>
        </n-drawer-content>
      </n-drawer>
      <n-layout has-sider class="workspace-frame">
        <n-layout-sider bordered :width="210">
          <div class="workspace-nav">
            <n-menu
              :value="section"
              :options="menuOptions"
              @update:value="openPage"
            />
            <div class="workspace-release">
              <span>HashDrop v{{ version }}</span>
            </div>
          </div>
        </n-layout-sider>
        <n-layout-content class="workspace-content">
          <n-alert v-if="sessionError" type="error"
            >{{ t("center.session_error", { error: errorText(sessionError) })
            }}<n-button text @click="router.go(0)">{{
              t("common.retry")
            }}</n-button></n-alert
          >
          <template v-else>
            <n-alert
              v-if="historyStorageError && !adminAuthenticated"
              type="warning"
              class="space-bottom"
              >{{ t("center.storage_error") }}</n-alert
            >
            <Login v-if="section === 'login'" />
            <Order v-else-if="route.params.id || section === 'query'" />
            <GuestOrders v-else-if="!adminAuthenticated" />
            <Products v-else-if="section === 'products'" />
            <Inventories v-else-if="section === 'inventories'" /><Orders
              v-else-if="section === 'orders'"
            />
            <Promotions v-else-if="section === 'promotions'" />
            <Donations v-else-if="section === 'donation'" />
            <Settings v-else-if="section === 'settings'" /><Overview v-else />
          </template>
        </n-layout-content>
      </n-layout>
    </n-layout-content>
  </n-layout>
</template>
