<!-- 登录面板基于 HashPay LoginPanel.vue 修改，认证内容替换为单管理员账号和密码。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { t } from "../i18n";

import { ref, watch } from "vue";
import { NAlert, NButton, NInput } from "naive-ui";
import { api, csrf, errorText, loadSession, sessionError } from "../api";
import FormDialog from "./FormDialog.vue";
const show = defineModel<boolean>("show", { required: true });
const username = ref("admin"),
  password = ref(""),
  busy = ref(false),
  error = ref("");
watch(show, () => {
  password.value = "";
  error.value = "";
});
async function login() {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  try {
    const result = await api<{ csrf: string }>("/admin/login", {
      method: "POST",
      body: {
        username: username.value,
        password: password.value,
      },
    });
    csrf.value = result.csrf;
    password.value = "";
    if (!(await loadSession())) throw new Error(t("errors.request_failed"));
    sessionError.value = "";
    show.value = false;
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <FormDialog
    v-model:show="show"
    :title="t('login.title')"
    :busy="busy"
    width="min(440px, calc(100vw - 28px))"
  >
    <form class="form-grid" @submit.prevent="login">
      <div class="field-stack">
        <label class="field-label" for="login-username">{{
          t("login.username")
        }}</label>
        <n-input
          id="username"
          v-model:value="username"
          :maxlength="120"
          :input-props="{ id: 'login-username', autocomplete: 'username' }"
        />
      </div>
      <div class="field-stack">
        <label class="field-label" for="password">{{
          t("login.password")
        }}</label
        ><n-input
          id="password"
          v-model:value="password"
          type="password"
          show-password-on="click"
          :input-props="{ autocomplete: 'current-password' }"
        />
      </div>
      <n-alert v-if="error" type="error">{{ errorText(error) }}</n-alert
      ><n-button attr-type="submit" type="primary" block :loading="busy">{{
        t("login.submit")
      }}</n-button>
    </form>
  </FormDialog>
</template>
