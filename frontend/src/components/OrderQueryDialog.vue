<script setup lang="ts">
import { onUnmounted, ref, watch } from "vue";
import { NAlert, NButton, NInput } from "naive-ui";
import { api, errorText, type OrderDetail } from "../api";
import {
  forgetPurchase,
  historyCleared,
  rememberClaimKey,
  rememberOrder,
} from "../claim";
import { t } from "../i18n";
import FormDialog from "./FormDialog.vue";

const show = defineModel<boolean>("show", { required: true });
const emit = defineEmits<{ found: [detail: OrderDetail, key: string] }>();
const key = ref(""),
  busy = ref(false),
  error = ref("");
let controller: AbortController | undefined;
function reset() {
  controller?.abort();
  key.value = error.value = "";
  busy.value = false;
}
watch(show, reset);
watch(historyCleared, () => {
  show.value = false;
  reset();
});
onUnmounted(() => controller?.abort());
async function lookup() {
  if (busy.value) return;
  const value = key.value;
  if (!value.trim() || Array.from(value).length > 128) {
    error.value = t("errors.claim_key");
    return;
  }
  busy.value = true;
  error.value = "";
  controller = new AbortController();
  const signal = controller.signal;
  try {
    const detail = await api<OrderDetail>("/orders/lookup", {
      method: "POST",
      body: { key: value },
      signal,
    });
    if (signal.aborted) return;
    rememberOrder(detail.order, value);
    rememberClaimKey(value);
    if (detail.order.delivery_status === "done")
      forgetPurchase(detail.order.id);
    show.value = false;
    emit("found", detail, value);
  } catch (cause) {
    if (!signal.aborted) error.value = errorText(cause);
  } finally {
    if (!signal.aborted) busy.value = false;
  }
}
</script>

<template>
  <FormDialog
    v-model:show="show"
    :title="t('center.order_query')"
    :busy="busy"
    width="min(440px, calc(100vw - 28px))"
  >
    <form id="order-query-form" class="form-grid" @submit.prevent="lookup">
      <p class="muted">{{ t("claim.private_help") }}</p>
      <div class="field-stack">
        <label class="field-label" for="claim-key">{{ t("claim.key") }}</label>
        <n-input
          v-model:value="key"
          :disabled="busy"
          :placeholder="t('claim.lookup_placeholder')"
          :input-props="{
            id: 'claim-key',
            autocomplete: 'off',
            spellcheck: false,
          }"
        />
      </div>
      <n-alert v-if="error" type="error">{{ errorText(error) }}</n-alert>
    </form>
    <template #footer>
      <n-button :disabled="busy" @click="show = false">{{
        t("common.cancel")
      }}</n-button>
      <n-button
        type="primary"
        attr-type="submit"
        form="order-query-form"
        :loading="busy"
        :disabled="busy || !key.trim()"
        >{{ t("common.query") }}</n-button
      >
    </template>
  </FormDialog>
</template>
