<script setup lang="ts">
import { t } from "../i18n";

import { onMounted, reactive, ref } from "vue";
import { NAlert, NButton, NInput, NSpin, useMessage } from "naive-ui";
import { api, errorText, site, type Settings } from "../api";
const message = useMessage(),
  data = ref<Settings>({
    title: "",
    notice: "",
    public_url: "",
    hashpay_url: "",
    merchant_id: "",
    key_configured: false,
  }),
  loading = ref(false),
  loaded = ref(false),
  saving = ref(false),
  error = ref("");
const form = reactive({
  title: "",
  notice: "",
  hashpay_url: "",
  merchant_id: "",
  private_key: "",
});
async function load() {
  loading.value = true;
  error.value = "";
  try {
    data.value = await api<Settings>("/admin/settings");
    site.value = { title: data.value.title, notice: data.value.notice };
    Object.assign(form, {
      title: data.value.title,
      notice: data.value.notice,
      hashpay_url: data.value.hashpay_url,
      merchant_id: data.value.merchant_id,
      private_key: "",
    });
    loaded.value = true;
  } catch (e) {
    error.value = errorText(e);
  } finally {
    loading.value = false;
  }
}
onMounted(load);
async function save() {
  if (saving.value || !loaded.value) return;
  saving.value = true;
  error.value = "";
  try {
    await api("/admin/settings", { method: "PUT", body: form });
    message.success(t("settings.saved"));
    await load();
  } catch (e) {
    error.value = errorText(e);
  } finally {
    saving.value = false;
  }
}
</script>
<template>
  <!-- 卡片与字段排列裁剪自 HashPay pages/Settings.vue，沿用 TinyFaka 设置接口。 -->
  <div class="grid">
    <div class="section-title">
      <h2>{{ t("nav.settings") }}</h2>
      <n-button
        type="primary"
        attr-type="submit"
        form="settings-form"
        :loading="saving"
        :disabled="!loaded || loading"
        >{{ t("settings.save") }}</n-button
      >
    </div>
    <n-alert v-if="error" type="error"
      >{{ errorText(error) }}
      <n-button v-if="!loaded" text @click="load">{{
        t("common.retry")
      }}</n-button></n-alert
    >
    <n-spin :show="loading">
      <form
        v-if="loaded"
        id="settings-form"
        class="grid"
        @submit.prevent="save"
      >
        <section class="panel grid">
          <div class="section-title">
            <h2>{{ t("settings.site") }}</h2>
          </div>
          <div class="form-grid two">
            <div class="field-stack">
              <label class="field-label" for="site-title-input">{{
                t("settings.site_name")
              }}</label>
              <n-input
                id="site-title"
                v-model:value="form.title"
                :maxlength="120"
                :input-props="{ id: 'site-title-input' }"
              />
            </div>
            <div class="field-stack">
              <label class="field-label" for="site-url">{{
                t("settings.site_url")
              }}</label>
              <n-input
                :value="data.public_url"
                readonly
                :input-props="{ id: 'site-url' }"
              />
            </div>
            <div class="field-stack settings-wide">
              <label class="field-label" for="notice-input">{{
                t("settings.notice")
              }}</label>
              <n-input
                id="notice"
                v-model:value="form.notice"
                type="textarea"
                :maxlength="2000"
                :input-props="{ id: 'notice-input' }"
              />
            </div>
          </div>
        </section>
        <section class="panel grid">
          <div class="section-title"><h2>HashPay</h2></div>
          <div class="form-grid two">
            <div class="field-stack">
              <label class="field-label" for="hashpay-url-input">{{
                t("settings.gateway_url")
              }}</label>
              <n-input
                id="hashpay-url"
                v-model:value="form.hashpay_url"
                placeholder="https://pay.example.com"
                :input-props="{ id: 'hashpay-url-input' }"
              />
            </div>
            <div class="field-stack">
              <label class="field-label" for="merchant-id-input">{{
                t("settings.merchant_id")
              }}</label>
              <n-input
                id="merchant-id"
                v-model:value="form.merchant_id"
                :input-props="{ id: 'merchant-id-input' }"
              />
            </div>
            <div class="field-stack settings-wide">
              <label class="field-label" for="merchant-key">{{
                t("settings.private_key")
              }}</label>
              <n-input
                v-model:value="form.private_key"
                type="textarea"
                :placeholder="t('settings.key_placeholder')"
                :input-props="{
                  id: 'merchant-key',
                  autocomplete: 'off',
                  spellcheck: false,
                }"
              />
            </div>
          </div>
          <p v-if="data.public_url.startsWith('https://')" class="helper">
            {{ t("settings.callback") }}
            {{ data.public_url }}/api/webhooks/hashpay
          </p>
          <p v-else class="helper">{{ t("settings.http_help") }}</p>
          <n-alert :type="data.key_configured ? 'success' : 'warning'">{{
            data.key_configured
              ? t("settings.key_saved")
              : t("settings.key_missing")
          }}</n-alert>
        </section>
      </form>
    </n-spin>
  </div>
</template>

<style scoped lang="scss">
.settings-wide {
  grid-column: 1 / -1;
}

#settings-form .helper {
  margin: 0;
}
</style>
