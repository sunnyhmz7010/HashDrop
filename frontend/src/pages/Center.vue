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
import LoginDialog from "../components/LoginDialog.vue";
import GuestDonations from "../components/GuestDonations.vue";
import DonationOrder from "./DonationOrder.vue";
const route = useRoute(),
  router = useRouter(),
  message = useMessage(),
  busy = ref(false),
  navOpen = ref(false),
  loginOpen = ref(false);
const section = computed(() => String(route.params.section ?? "history"));
const personalSections = ["history", "query", "donations"];
const personalPage = computed(
  () =>
    personalSections.includes(section.value) ||
    (section.value === "orders" && !!route.params.id),
);
const menuOptions = computed(() => [
  { key: "history", label: t("center.history") },
  { key: "query", label: t("center.query") },
  { key: "donations", label: t("center.donations") },
  ...(adminAuthenticated.value
    ? [
        { key: "admin-divider", type: "divider" as const },
        { key: "overview", label: t("nav.overview") },
        { key: "products", label: t("nav.products") },
        { key: "inventories", label: t("nav.inventories") },
        { key: "orders", label: t("nav.orders") },
        { key: "promotions", label: t("nav.promotions") },
        { key: "donation", label: t("nav.donation") },
        { key: "settings", label: t("nav.settings") },
      ]
    : []),
]);
watch(adminAuthenticated, (value) => {
  if (
    value === false &&
    !personalSections.includes(section.value) &&
    !route.params.id
  )
    void router.replace("/center");
});
watch(
  () => route.query.login,
  (value) => {
    if (value === "1" && !adminAuthenticated.value) loginOpen.value = true;
  },
  { immediate: true },
);
watch(loginOpen, (value) => {
  if (!value && route.query.login) {
    const { login, ...query } = route.query;
    void router.replace({ path: route.path, query, hash: route.hash });
  }
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
    if (!personalSections.includes(section.value) && !route.params.id)
      await router.push("/center");
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
        <n-button v-else size="small" secondary @click="loginOpen = true">{{
          t("center.admin_login")
        }}</n-button>
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
          <n-alert v-if="sessionError" type="error" class="space-bottom"
            >{{ t("center.session_error", { error: errorText(sessionError) })
            }}<n-button text @click="router.go(0)">{{
              t("common.retry")
            }}</n-button></n-alert
          >
          <template v-if="!sessionError || personalPage">
            <n-alert
              v-if="
                historyStorageError &&
                (section === 'history' || section === 'query')
              "
              type="warning"
              class="space-bottom"
              >{{ t("center.storage_error") }}</n-alert
            >
            <DonationOrder
              v-if="section === 'donations' && route.params.id"
              embedded
            />
            <GuestDonations v-else-if="section === 'donations'" />
            <Order
              v-else-if="
                (section === 'orders' && route.params.id) || section === 'query'
              "
            />
            <GuestOrders v-else-if="section === 'history'" />
            <template v-else-if="adminAuthenticated">
              <Products v-if="section === 'products'" />
              <Inventories v-else-if="section === 'inventories'" /><Orders
                v-else-if="section === 'orders'"
              />
              <Promotions v-else-if="section === 'promotions'" />
              <Donations v-else-if="section === 'donation'" />
              <Settings v-else-if="section === 'settings'" /><Overview v-else />
            </template>
          </template>
        </n-layout-content>
      </n-layout>
    </n-layout-content>
    <LoginDialog v-model:show="loginOpen" />
  </n-layout>
</template>
