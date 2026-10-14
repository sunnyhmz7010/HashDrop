<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { NAlert, NButton, NInput, NSelect, NSwitch } from "naive-ui";
import { api, errorText, initialized, site } from "../api";

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
  manual_policy: "hold",
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
        public_url: new URL(form.public_url.trim()).origin,
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
</script>
<template>
  <main class="setup-shell">
    <section class="setup-panel">
      <div class="section-title"><h1>初始化 TinyFaka</h1></div>
      <p class="muted">设置管理员和站点，保存后即可进入后台。</p>
      <n-alert v-if="finished" type="success">
        初始化已完成，请通过配置的站点地址登录。
        <a :href="loginURL">进入管理员登录</a>
      </n-alert>
      <form v-else @submit.prevent="submit">
        <n-alert v-if="insecure" type="warning" class="space-bottom">
          当前连接为
          HTTP，提交的密码和私钥会明文传输，请在可信网络中完成初始化。
        </n-alert>
        <fieldset :disabled="busy">
          <h3>初始化凭证</h3>
          <p class="helper">
            从当前容器启动日志复制 setup_token；服务重启后请使用新凭证。
          </p>
          <label class="field-label" for="setup-token">一次性初始化凭证</label>
          <n-input
            v-model:value="token"
            type="password"
            show-password-on="click"
            :input-props="{ id: 'setup-token', autocomplete: 'off' }"
          />
          <h3 class="space-top">站点与管理员</h3>
          <label class="field-label" for="setup-title">站点名称</label>
          <n-input
            v-model:value="form.title"
            :maxlength="120"
            :input-props="{ id: 'setup-title' }"
          />
          <label class="field-label" for="setup-url">站点地址</label>
          <n-input
            v-model:value="form.public_url"
            placeholder="http://服务器地址:8080"
            :input-props="{ id: 'setup-url' }"
          />
          <p class="helper">
            填写浏览器实际访问的完整地址，不带路径。登录和领取链接使用此地址。
          </p>
          <label class="field-label" for="setup-notice">商城公告</label>
          <n-input
            v-model:value="form.notice"
            type="textarea"
            :maxlength="2000"
            :input-props="{ id: 'setup-notice' }"
          />
          <label class="field-label" for="setup-password">管理员密码</label>
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
          <label class="field-label" for="setup-confirm">确认密码</label>
          <n-input
            v-model:value="form.password_confirm"
            type="password"
            :maxlength="256"
            :input-props="{ id: 'setup-confirm', autocomplete: 'new-password' }"
          />
          <p class="helper">密码至少 12 字符。此系统只有一位管理员。</p>
          <div class="space-top">
            <n-switch
              v-model:value="totpEnabled"
              :disabled="busy"
              aria-label="启用 TOTP"
            />
            启用 TOTP 动态验证
          </div>
          <template v-if="totpEnabled">
            <label class="field-label" for="setup-totp">TOTP 密钥</label>
            <n-input
              v-model:value="form.totp_secret"
              type="password"
              :input-props="{ id: 'setup-totp', autocomplete: 'off' }"
            />
            <p class="helper">
              填写验证器中的 Base32 密钥并单独备份；初始化后不会回显。
            </p>
            <label class="field-label" for="setup-code">当前动态验证码</label>
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
          <h3 class="space-top">HashPay（可稍后在后台填写）</h3>
          <label class="field-label" for="setup-gateway">HashPay 地址</label>
          <n-input
            v-model:value="form.hashpay_url"
            placeholder="https://pay.example.com"
            :input-props="{ id: 'setup-gateway' }"
          />
          <label class="field-label" for="setup-merchant">商户 ID</label>
          <n-input
            v-model:value="form.merchant_id"
            :input-props="{ id: 'setup-merchant' }"
          />
          <label class="field-label" for="setup-key">商户 RSA 私钥</label>
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
          <p class="helper">
            地址、商户 ID、PEM 私钥一起填写，或全部留空。私钥保存后不会回显。
          </p>
          <label class="field-label">HashPay 人工确认付款</label>
          <n-select
            v-model:value="form.manual_policy"
            :disabled="busy"
            :options="[
              { label: '保留订单，由店主核实后调整策略', value: 'hold' },
              { label: '接受 HashPay 管理员确认的已付款订单', value: 'accept' },
            ]"
          />
          <p class="helper">
            HTTP 站点使用签名查询补查付款。HashPay 回调地址只接受 HTTPS。
          </p>
        </fieldset>
        <n-alert v-if="error" type="error" class="space-top">{{
          error
        }}</n-alert>
        <n-button
          type="primary"
          attr-type="submit"
          :loading="busy"
          class="space-top"
          >完成初始化</n-button
        >
      </form>
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
