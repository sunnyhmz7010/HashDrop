<script setup lang="ts">
import { t } from "../i18n";

import { computed, onMounted, ref } from "vue";
import { NAlert, NButton, NSpin } from "naive-ui";
import { api, errorText } from "../api";
import DonationWall from "./DonationWall.vue";
const data = ref<Record<string, number>>(),
  error = ref(""),
  busy = ref(false);
const labels = computed(() => [
  ["products", t("overview.products")],
  ["stock", t("overview.stock")],
  ["orders", t("overview.orders")],
  ["paid", t("overview.paid")],
  ["manual", t("overview.manual")],
]);
async function load() {
  busy.value = true;
  error.value = "";
  try {
    data.value = await api("/admin/overview");
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
onMounted(load);
</script>
<template>
  <div class="grid overview-page">
    <div class="section-title">
      <h2>{{ t("nav.overview") }}</h2>
      <n-button :loading="busy" @click="load">{{
        t("common.refresh")
      }}</n-button>
    </div>
    <n-alert v-if="error" type="error" class="space-bottom"
      >{{ errorText(error) }}
      <n-button text @click="load">{{ t("common.retry") }}</n-button></n-alert
    ><n-spin :show="busy"
      ><div class="metric-row">
        <article v-for="[key, label] in labels" :key="key" class="metric">
          <span class="muted">{{ label }}</span
          ><strong>{{ data?.[key!] ?? "—" }}</strong>
        </article>
      </div></n-spin
    >
    <DonationWall />
  </div>
</template>
