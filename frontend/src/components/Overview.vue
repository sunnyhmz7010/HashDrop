<script setup lang="ts">
import { t } from "../i18n";

import { computed, onMounted, onUnmounted, ref } from "vue";
import { NAlert, NButton, NSkeleton } from "naive-ui";
import { api, errorText } from "../api";
import DonationWall from "./DonationWall.vue";
const data = ref<Record<string, number>>(),
  error = ref(""),
  busy = ref(false);
const donationWall = ref<InstanceType<typeof DonationWall>>();
const labels = computed(() => [
  ["products", t("overview.products")],
  ["stock", t("overview.stock")],
  ["orders", t("overview.orders")],
  ["paid", t("overview.paid")],
  ["delivery_attention", t("overview.manual")],
]);
const controller = new AbortController();
async function load() {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  try {
    const result = await api<Record<string, number>>("/admin/overview", {
      signal: controller.signal,
    });
    if (!controller.signal.aborted) data.value = result;
  } catch (e) {
    if (!controller.signal.aborted) error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
function refresh() {
  return Promise.all([load(), donationWall.value?.refresh()]);
}
onMounted(load);
onUnmounted(() => controller.abort());
</script>
<template>
  <div class="grid overview-page">
    <div class="section-title">
      <h2>{{ t("nav.overview") }}</h2>
      <n-button
        :loading="busy || donationWall?.loading"
        :disabled="donationWall?.processing"
        @click="refresh"
        >{{ t("common.refresh") }}</n-button
      >
    </div>
    <n-alert v-if="error" type="error" class="space-bottom"
      >{{ errorText(error) }}
      <n-button text @click="load">{{ t("common.retry") }}</n-button></n-alert
    >
    <div class="metric-row" :aria-busy="busy">
      <article v-for="[key, label] in labels" :key="key" class="metric">
        <span class="muted">{{ label }}</span>
        <n-skeleton v-if="busy && !data" text width="60%" />
        <strong v-else>{{ data?.[key!] ?? "—" }}</strong>
      </article>
    </div>
    <DonationWall ref="donationWall" />
  </div>
</template>
