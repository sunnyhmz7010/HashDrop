<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NInput } from "naive-ui";
import StoreLayout from "../components/StoreLayout.vue";
import { api, csrf, errorText } from "../api";
const password = ref(""),
  totp = ref(""),
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
      body: { password: password.value, totp: totp.value },
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
        <div class="section-title"><h1>登录后台</h1></div>
        <p class="muted">管理商品、库存与订单。</p>
        <form class="login-form" @submit.prevent="login">
          <label class="field-label" for="password">管理员密码</label
          ><n-input
            id="password"
            v-model:value="password"
            type="password"
            show-password-on="click"
            :input-props="{ autocomplete: 'current-password' }"
          /><label class="field-label" for="totp"
            >动态验证码（已启用时填写）</label
          ><n-input
            id="totp"
            v-model:value="totp"
            :maxlength="6"
            :input-props="{
              inputmode: 'numeric',
              autocomplete: 'one-time-code',
            }"
          /><n-alert v-if="error" type="error" class="space-top">{{
            error
          }}</n-alert
          ><n-button
            attr-type="submit"
            type="primary"
            block
            class="space-top"
            :loading="busy"
            >登录</n-button
          >
        </form>
      </section>
    </main></StoreLayout
  >
</template>

<style scoped lang="scss">
.login-form > .field-label {
  margin: 12px 0 7px;
}

.login-panel > p {
  margin: 0;
}
</style>
