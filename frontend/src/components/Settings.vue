<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { NAlert, NButton, NInput, NSelect, NSpin, useMessage } from "naive-ui";
import { api, errorText, site, type Settings } from "../api";
const message = useMessage(),
  data = ref<Settings>({
    title: "",
    notice: "",
    public_url: "",
    hashpay_url: "",
    merchant_id: "",
    manual_policy: "hold",
    key_configured: false,
    totp_enabled: false,
    adapters: [],
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
  manual_policy: "hold",
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
      manual_policy: data.value.manual_policy,
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
    message.success("设置已保存");
    await load();
  } catch (e) {
    error.value = errorText(e);
  } finally {
    saving.value = false;
  }
}
</script>
<template>
  <h2 class="space-bottom">系统设置</h2>
  <n-spin :show="loading"
    ><section class="panel settings-panel">
      <form v-if="loaded" @submit.prevent="save">
        <h3>站点</h3>
        <label class="field-label" for="site-title">站点名称</label
        ><n-input
          id="site-title"
          v-model:value="form.title"
          :maxlength="120"
        /><label class="field-label" for="notice">商城公告</label
        ><n-input
          id="notice"
          v-model:value="form.notice"
          type="textarea"
          :maxlength="2000"
        />
        <p class="helper">站点地址：{{ data.public_url }}</p>
        <h3 class="space-top">HashPay</h3>
        <label class="field-label" for="hashpay-url">HashPay 地址</label
        ><n-input
          id="hashpay-url"
          v-model:value="form.hashpay_url"
          placeholder="https://pay.example.com"
        /><label class="field-label" for="merchant-id">商户 ID</label
        ><n-input id="merchant-id" v-model:value="form.merchant_id" /><label
          class="field-label"
          for="merchant-key"
          >商户 RSA 私钥</label
        ><n-input
          v-model:value="form.private_key"
          type="textarea"
          placeholder="留空保留现有私钥；填写 PEM 私钥以配置或替换"
          :input-props="{
            id: 'merchant-key',
            autocomplete: 'off',
            spellcheck: false,
          }"
        /><label class="field-label" for="manual-policy"
          >HashPay 人工确认付款</label
        ><n-select
          id="manual-policy"
          v-model:value="form.manual_policy"
          :options="[
            { label: '保留订单，由店主核实后调整策略', value: 'hold' },
            { label: '接受 HashPay 管理员确认的已付款订单', value: 'accept' },
          ]"
        />
        <p v-if="data.public_url.startsWith('https://')" class="helper">
          回调地址：{{ data.public_url }}/api/webhooks/hashpay
        </p>
        <p v-else class="helper">
          HTTP 站点使用签名查询补查付款，无需设置回调地址。
        </p>
        <n-alert
          :type="data.key_configured ? 'success' : 'warning'"
          class="space-top"
          >{{
            data.key_configured
              ? "商户私钥已保存；请通过小额订单验证接入。"
              : "商户私钥尚未配置。请在上方填写并保存。"
          }}</n-alert
        >
        <p class="helper">
          TOTP：{{
            data.totp_enabled ? "已启用" : "未启用"
          }}。私钥、密钥和密码保存后不会回显。
        </p>
        <p class="helper">
          可用业务适配器：{{
            data.adapters.length
              ? data.adapters.join("、")
              : "暂无；API 商品只能保存为草稿"
          }}
        </p>
        <n-button
          type="primary"
          attr-type="submit"
          :loading="saving"
          class="space-top"
          >保存设置</n-button
        >
      </form>
      <n-alert v-if="error" type="error" class="space-top"
        >{{ error }}
        <n-button v-if="!loaded" text @click="load">重试</n-button></n-alert
      >
    </section></n-spin
  >
</template>
