<script setup lang="ts">
import { t } from "../i18n";

import { onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NInput, NSwitch } from "naive-ui";
import { api, errorText, initialized, site } from "../api";
import LocaleSwitch from "../components/LocaleSwitch.vue";

const router = useRouter(),
  token = ref(new URLSearchParams(location.hash.slice(1)).get("setup") ?? ""),
  busy = ref(false),
  error = ref(""),
  finished = ref(false),
  loginURL = ref(""),
  totpEnabled = ref(false),
  insecure = location.protocol === "http:";
const form = reactive({
  title: "TinyFaka",
  public_url: location.origin,
  notice: "",
  password: "",
  password_confirm: "",
  totp_secret: "",
  totp_code: "",
  hashpay_url: "",
  merchant_id: "",
  private_key: "",
});
onMounted(async () => {
  try {
    const status = await api<{ initialized: boolean }>("/setup/status");
    initialized.value = status.initialized;
    if (status.initialized) await router.replace("/admin/login");
  } catch (e) {
    error.value = errorText(e);
  }
});
async function submit() {
  if (busy.value || finished.value) return;
  busy.value = true;
  error.value = "";
  try {
    const result = await api<{ title: string; public_url: string }>("/setup", {
      method: "POST",
      token: token.value.trim(),
      body: {
        ...form,
        public_url: validatedSiteURL(),
        totp_enabled: totpEnabled.value,
        totp_secret: totpEnabled.value ? form.totp_secret : "",
        totp_code: totpEnabled.value ? form.totp_code : "",
      },
    });
    initialized.value = true;
    site.value = { title: result.title, notice: form.notice };
    token.value = "";
    form.password = form.password_confirm = form.private_key = "";
    form.totp_secret = form.totp_code = "";
    finished.value = true;
    loginURL.value = `${result.public_url}/admin/login`;
    // 登录的 Origin 必须与站点地址一致，保存后引导到实际配置地址。
    if (result.public_url === location.origin)
      await router.replace("/admin/login");
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
function validatedSiteURL(): string {
  try {
    return new URL(form.public_url.trim()).origin;
  } catch {
    throw new Error(t("errors.site_url_invalid"));
  }
}
</script>
<template>
  <main class="setup-shell">
    <section class="setup-panel">
      <div class="section-title">
        <h1>{{ t("setup.title") }}</h1>
      </div>
      <p class="muted">{{ t("setup.description") }}</p>
      <n-alert v-if="finished" type="success"
        >{{ t("setup.finished") }}<a :href="loginURL">{{ t("setup.login") }}</a>
      </n-alert>
      <form v-else @submit.prevent="submit">
        <n-alert v-if="insecure" type="warning" class="space-bottom">{{
          t("setup.http_warning")
        }}</n-alert>
        <fieldset :disabled="busy">
          <h3>{{ t("setup.credential_title") }}</h3>
          <p class="helper">{{ t("setup.credential_help") }}</p>
          <label class="field-label" for="setup-token">{{
            t("setup.credential")
          }}</label>
          <n-input
            v-model:value="token"
            type="password"
            show-password-on="click"
            :input-props="{ id: 'setup-token', autocomplete: 'off' }"
          />
          <h3 class="space-top">{{ t("setup.site_admin") }}</h3>
          <label class="field-label" for="setup-title">{{
            t("settings.site_name")
          }}</label>
          <n-input
            v-model:value="form.title"
            :maxlength="120"
            :input-props="{ id: 'setup-title' }"
          />
          <label class="field-label" for="setup-url">{{
            t("settings.site_url")
          }}</label>
          <n-input
            v-model:value="form.public_url"
            :placeholder="t('setup.url_placeholder')"
            :input-props="{ id: 'setup-url' }"
          />
          <p class="helper">{{ t("setup.url_help") }}</p>
          <label class="field-label" for="setup-notice">{{
            t("settings.notice")
          }}</label>
          <n-input
            v-model:value="form.notice"
            type="textarea"
            :maxlength="2000"
            :input-props="{ id: 'setup-notice' }"
          />
          <label class="field-label" for="setup-password">{{
            t("login.password")
          }}</label>
          <n-input
            v-model:value="form.password"
            type="password"
            show-password-on="click"
            :maxlength="256"
            :input-props="{
              id: 'setup-password',
              autocomplete: 'new-password',
            }"
          />
          <label class="field-label" for="setup-confirm">{{
            t("setup.confirm_password")
          }}</label>
          <n-input
            v-model:value="form.password_confirm"
            type="password"
            :maxlength="256"
            :input-props="{ id: 'setup-confirm', autocomplete: 'new-password' }"
          />
          <p class="helper">{{ t("setup.password_help") }}</p>
          <div class="space-top">
            <n-switch
              v-model:value="totpEnabled"
              :disabled="busy"
              :aria-label="t('setup.enable_totp')"
            />{{ t("setup.totp_label") }}
          </div>
          <template v-if="totpEnabled">
            <label class="field-label" for="setup-totp">{{
              t("setup.totp_secret")
            }}</label>
            <n-input
              v-model:value="form.totp_secret"
              type="password"
              :input-props="{ id: 'setup-totp', autocomplete: 'off' }"
            />
            <p class="helper">{{ t("setup.totp_help") }}</p>
            <label class="field-label" for="setup-code">{{
              t("setup.totp_code")
            }}</label>
            <n-input
              v-model:value="form.totp_code"
              :maxlength="6"
              :input-props="{
                id: 'setup-code',
                inputmode: 'numeric',
                autocomplete: 'one-time-code',
              }"
            />
          </template>
          <h3 class="space-top">{{ t("setup.gateway_title") }}</h3>
          <label class="field-label" for="setup-gateway">{{
            t("settings.gateway_url")
          }}</label>
          <n-input
            v-model:value="form.hashpay_url"
            placeholder="https://pay.example.com"
            :input-props="{ id: 'setup-gateway' }"
          />
          <label class="field-label" for="setup-merchant">{{
            t("settings.merchant_id")
          }}</label>
          <n-input
            v-model:value="form.merchant_id"
            :input-props="{ id: 'setup-merchant' }"
          />
          <label class="field-label" for="setup-key">{{
            t("settings.private_key")
          }}</label>
          <n-input
            v-model:value="form.private_key"
            type="textarea"
            placeholder="-----BEGIN PRIVATE KEY-----"
            :input-props="{
              id: 'setup-key',
              autocomplete: 'off',
              spellcheck: false,
            }"
          />
          <p class="helper">{{ t("setup.gateway_help") }}</p>
          <p class="helper">{{ t("setup.callback_help") }}</p>
        </fieldset>
        <n-alert v-if="error" type="error" class="space-top">{{
          errorText(error)
        }}</n-alert>
        <n-button
          type="primary"
          attr-type="submit"
          :loading="busy"
          class="space-top"
          >{{ t("setup.submit") }}</n-button
        >
      </form>
      <div class="setup-locale"><LocaleSwitch /></div>
    </section>
  </main>
</template>

<style scoped lang="scss">
.setup-panel .field-label {
  margin: 12px 0 7px;
}

.setup-panel fieldset {
  border: 0;
  min-width: 0;
  margin: 0;
  padding: 0;
}

.setup-panel h3 {
  font-size: 15px;
}

.setup-panel .helper {
  margin: 7px 0;
}

.setup-panel .n-button {
  max-width: 100%;
}
</style>
