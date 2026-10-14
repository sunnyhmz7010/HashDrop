<!-- 登录面板基于 HashPay LoginPanel.vue 修改，认证内容替换为单管理员账号和密码。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { t } from "../i18n";

import { ref } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NInput } from "naive-ui";
import StoreLayout from "../components/StoreLayout.vue";
import { api, csrf, errorText } from "../api";
const username = ref("admin"),
  password = ref(""),
  busy = ref(false),
  error = ref(""),
  router = useRouter();
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
    await router.push("/admin");
  } catch (e) {
    error.value = errorText(e);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <StoreLayout
    ><main class="page">
      <section class="panel grid login-panel">
        <div class="section-title">
          <h1>{{ t("login.title") }}</h1>
        </div>
        <p class="muted">{{ t("login.description") }}</p>
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
      </section>
    </main></StoreLayout
  >
</template>

<style scoped lang="scss">
.login-panel > p {
  margin: 0;
}
</style>
