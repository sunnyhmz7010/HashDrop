<script setup lang="ts">
import { t } from "../i18n";

import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NInput } from "naive-ui";
import { api, csrf, errorText, initialized, site } from "../api";
import LocaleSwitch from "../components/LocaleSwitch.vue";

const router = useRouter(),
  busy = ref(false),
  error = ref(""),
  finished = ref(false),
  loginURL = ref(""),
  insecure = location.protocol === "http:";
const form = reactive({
  title: "HashDrop",
  public_url: location.origin,
  username: "admin",
  password: "",
  password_confirm: "",
  hashpay_url: "",
  merchant_id: "",
  private_key: "",
});
const callbackURL = computed(() => {
  try {
    const publicURL = validatedSiteURL();
    return publicURL.startsWith("https://")
      ? `${publicURL}/api/webhooks/hashpay`
      : "";
  } catch {
    // 站点地址尚未输入完整时，不展示无效或过期的回调地址。
    return "";
  }
});
onMounted(async () => {
  try {
    const status = await api<{ initialized: boolean }>("/setup/status");
    initialized.value = status.initialized;
    if (status.initialized) await router.replace("/center?login=1");
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
      body: {
        ...form,
        public_url: validatedSiteURL(),
      },
    });
    initialized.value = true;
    site.value = { title: result.title, notice: "" };
    form.password_confirm = form.private_key = "";
    finished.value = true;
    loginURL.value = `${result.public_url}/center?login=1`;
    // 复用现有登录校验；跨域时不能把密码或会话凭证放入跳转链接。
    if (result.public_url === location.origin) {
      const session = await api<{ csrf: string }>("/admin/login", {
        method: "POST",
        body: { username: form.username, password: form.password },
      });
      csrf.value = session.csrf;
      await router.replace("/center");
    } else location.replace(loginURL.value);
  } catch (e) {
    error.value = errorText(e);
  } finally {
    if (finished.value) form.password = "";
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
      <div class="section-title setup-heading">
        <h1>{{ t("setup.title") }}</h1>
        <LocaleSwitch />
      </div>
      <n-alert v-if="finished" type="success"
        >{{ t("setup.finished") }}<a :href="loginURL">{{ t("setup.login") }}</a>
      </n-alert>
      <form v-else @submit.prevent="submit">
        <n-alert v-if="insecure" type="warning" class="space-bottom">{{
          t("setup.http_warning")
        }}</n-alert>
        <fieldset :disabled="busy">
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
          <label class="field-label" for="setup-username">{{
            t("login.username")
          }}</label>
          <n-input
            v-model:value="form.username"
            :maxlength="120"
            :input-props="{ id: 'setup-username', autocomplete: 'username' }"
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
          <label class="field-label" for="setup-callback">{{
            t("settings.callback")
          }}</label>
          <n-input
            :value="callbackURL"
            readonly
            :placeholder="t('setup.callback_placeholder')"
            :input-props="{ id: 'setup-callback' }"
          />
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
      <n-alert v-if="finished && error" type="warning" class="space-top">{{
        t("setup.auto_login_failed", { error: errorText(error) })
      }}</n-alert>
    </section>
  </main>
</template>

<style scoped lang="scss">
.setup-heading {
  flex-wrap: wrap;
  gap: 12px;
}

.setup-heading h1 {
  flex: 1;
  min-width: 0;
  font-size: 20px;
}

.setup-heading :deep(.locale-switch) {
  flex-basis: 7rem;
  width: 7rem;
}

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
