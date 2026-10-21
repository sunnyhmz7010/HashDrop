<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import {
  NAlert,
  NButton,
  NDatePicker,
  NEmpty,
  NInput,
  NInputNumber,
  NSelect,
  NSkeleton,
  NSwitch,
  NTag,
  useDialog,
  useMessage,
} from "naive-ui";
import {
  api,
  copyText,
  errorText,
  money,
  priceText,
  toMinor,
  type Product,
  type Promotion,
} from "../api";
import { formatTime, t } from "../i18n";
import FormDialog from "./FormDialog.vue";

const items = ref<Promotion[]>([]),
  products = ref<Product[]>([]);
const loading = ref(false),
  loaded = ref(false),
  error = ref(""),
  editor = ref(false),
  saving = ref(false),
  deleting = ref(""),
  formError = ref("");
const message = useMessage(),
  dialog = useDialog();
const model = reactive({
  id: "",
  name: "",
  kind: "automatic" as Promotion["kind"],
  code: "",
  discount_type: "percentage" as Promotion["discount_type"],
  value: "20",
  currency: "",
  min_total: "",
  min_quantity: 1,
  scope: "all" as Promotion["scope"],
  product_ids: [] as string[],
  stackable: false,
  enabled: true,
  starts_at: null as number | null,
  ends_at: null as number | null,
  usage_limit: 0,
});
let controller: AbortController | undefined,
  generation = 0;
async function load() {
  const current = ++generation;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const [rules, candidates] = await Promise.all([
      api<Promotion[]>("/admin/promotions", { signal: controller.signal }),
      api<Product[]>("/admin/products", { signal: controller.signal }),
    ]);
    if (current === generation) {
      items.value = rules;
      products.value = candidates;
      loaded.value = true;
    }
  } catch (e) {
    if (current === generation) error.value = errorText(e);
  } finally {
    if (current === generation) loading.value = false;
  }
}
onMounted(load);
onUnmounted(() => {
  generation++;
  controller?.abort();
});
const kinds = computed(() => [
  { label: t("promotion.automatic"), value: "automatic" },
  { label: t("promotion.code_kind"), value: "code" },
]);
const types = computed(() => [
  { label: t("promotion.percentage"), value: "percentage" },
  { label: t("promotion.fixed"), value: "fixed" },
]);
const scopes = computed(() => [
  { label: t("promotion.all"), value: "all" },
  { label: t("promotion.selected"), value: "selected" },
]);
const currencies = computed(() => [
  { label: t("promotion.any_currency"), value: "" },
  ...["CNY", "USD", "EUR", "GBP", "HKD", "JPY", "USDT", "USDC"].map(
    (value) => ({ label: value, value }),
  ),
]);
const productOptions = computed(() => [
  ...products.value.map((p) => ({ label: p.name, value: p.id })),
  ...model.product_ids
    .filter((id) => !products.value.some((p) => p.id === id))
    .map((id) => ({
      label: t("promotion.deleted_product"),
      value: id,
    })),
]);
function edit(p?: Promotion) {
  formError.value = "";
  Object.assign(
    model,
    p
      ? {
          ...p,
          product_ids: [...p.product_ids],
          value:
            p.discount_type === "percentage"
              ? String(p.value / 100)
              : priceText(p.value, p.currency),
          min_total: p.min_total ? priceText(p.min_total, p.currency) : "",
          starts_at: p.starts_at ? p.starts_at * 1000 : null,
          ends_at: p.ends_at ? p.ends_at * 1000 : null,
        }
      : {
          id: "",
          name: "",
          kind: "automatic",
          code: "",
          discount_type: "percentage",
          value: "20",
          currency: "",
          min_total: "",
          min_quantity: 1,
          scope: "all",
          product_ids: [],
          stackable: false,
          enabled: true,
          starts_at: null,
          ends_at: null,
          usage_limit: 0,
        },
  );
  editor.value = true;
}
function setType(value: Promotion["discount_type"]) {
  model.discount_type = value;
  model.value = "";
  if (value === "fixed" && !model.currency) model.currency = "CNY";
}
function generateCode() {
  const bytes = crypto.getRandomValues(new Uint8Array(8));
  model.code = Array.from(bytes, (value) => value.toString(16).padStart(2, "0"))
    .join("")
    .toUpperCase();
}
async function copyCode(p: Promotion) {
  try {
    await copyText(p.code);
    message.success(t("common.copied"));
  } catch {
    message.error(t("common.copy_fallback"));
  }
}
async function save() {
  if (saving.value) return;
  formError.value = "";
  saving.value = true;
  try {
    const value =
      model.discount_type === "percentage"
        ? toMinor(model.value, "CNY")
        : toMinor(model.value, model.currency);
    if (model.discount_type === "percentage" && value > 9999)
      throw new Error(t("errors.promotion_value"));
    const body = {
      name: model.name,
      kind: model.kind,
      code: model.kind === "code" ? model.code : "",
      discount_type: model.discount_type,
      value,
      currency: model.currency,
      min_total:
        model.min_total.trim() && !/^0(?:\.0*)?$/.test(model.min_total.trim())
          ? toMinor(model.min_total, model.currency)
          : 0,
      min_quantity: model.min_quantity,
      scope: model.scope,
      product_ids: model.scope === "selected" ? model.product_ids : [],
      stackable: model.stackable,
      enabled: model.enabled,
      starts_at: model.starts_at ? Math.floor(model.starts_at / 1000) : 0,
      ends_at: model.ends_at ? Math.floor(model.ends_at / 1000) : 0,
      usage_limit: model.usage_limit,
    };
    await api(
      model.id ? `/admin/promotions/${model.id}` : "/admin/promotions",
      { method: model.id ? "PUT" : "POST", body },
    );
    editor.value = false;
    message.success(t("promotion.saved"));
    await load();
  } catch (e) {
    formError.value = errorText(e);
  } finally {
    saving.value = false;
  }
}
function remove(p: Promotion) {
  if (deleting.value) return;
  const confirmation = dialog.warning({
    title: t("promotion.delete_title"),
    content: t("promotion.delete_warning", { name: p.name }),
    positiveText: t("common.delete"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (deleting.value) return false;
      deleting.value = p.id;
      Object.assign(confirmation, {
        loading: true,
        closable: false,
        maskClosable: false,
        closeOnEsc: false,
        negativeButtonProps: { disabled: true },
      });
      try {
        await api(`/admin/promotions/${p.id}`, { method: "DELETE" });
        items.value = items.value.filter((item) => item.id !== p.id);
        message.success(t("promotion.deleted"));
        await load();
        return true;
      } catch (e) {
        message.error(errorText(e));
        return false;
      } finally {
        deleting.value = "";
        Object.assign(confirmation, {
          loading: false,
          closable: true,
          maskClosable: true,
          closeOnEsc: true,
          negativeButtonProps: { disabled: false },
        });
      }
    },
  });
}
function offer(p: Promotion) {
  return p.discount_type === "percentage"
    ? t("promotion.percent_value", { value: p.value / 100 })
    : t("promotion.fixed_value", { value: money(p.value, p.currency) });
}
function target(p: Promotion) {
  return p.scope === "all"
    ? t("promotion.all")
    : p.product_ids
        .map(
          (id) =>
            products.value.find((product) => product.id === id)?.name ??
            t("promotion.deleted_product"),
        )
        .join("、");
}
function status(p: Promotion) {
  const now = Date.now() / 1000;
  if (!p.enabled) return t("promotion.disabled");
  if (p.ends_at && p.ends_at <= now) return t("promotion.ended");
  if (p.starts_at > now) return t("promotion.scheduled");
  if (p.usage_limit && p.used >= p.usage_limit) return t("promotion.exhausted");
  return t("promotion.active");
}
</script>

<template>
  <div class="grid">
    <div class="section-title">
      <h2>{{ t("nav.promotions") }}</h2>
      <div class="row-actions">
        <n-button :loading="loading" @click="load">{{
          t("common.refresh")
        }}</n-button
        ><n-button
          type="primary"
          :disabled="loading || !!error"
          @click="edit()"
          >{{ t("promotion.add") }}</n-button
        >
      </div>
    </div>
    <n-alert v-if="error" type="error"
      >{{ errorText(error) }}
      <n-button text @click="load">{{ t("common.retry") }}</n-button></n-alert
    >
    <div :aria-busy="loading">
      <div
        v-if="items.length || (loading && !loaded)"
        class="panel promotion-table-panel"
      >
        <div class="table-wrap">
          <table class="promotions-table">
            <thead>
              <tr>
                <th>{{ t("promotion.name") }}</th>
                <th>{{ t("promotion.offer") }}</th>
                <th>{{ t("promotion.scope") }}</th>
                <th>{{ t("promotion.schedule") }}</th>
                <th>{{ t("promotion.usage") }}</th>
                <th>{{ t("common.status") }}</th>
                <th>{{ t("common.actions") }}</th>
              </tr>
            </thead>
            <tbody>
              <template v-if="loading && !loaded">
                <tr v-for="row in 3" :key="row" aria-hidden="true">
                  <td v-for="column in 7" :key="column"><n-skeleton text /></td>
                </tr>
              </template>
              <tr v-for="p in items" :key="p.id">
                <td>
                  <strong>{{ p.name }}</strong>
                  <div class="muted">
                    {{
                      p.kind === "automatic" ? t("promotion.automatic") : p.code
                    }}
                    <n-button
                      v-if="p.kind === 'code'"
                      text
                      size="tiny"
                      @click="copyCode(p)"
                      >{{ t("common.copy") }}</n-button
                    >
                  </div>
                </td>
                <td>
                  {{ offer(p) }}
                  <div class="muted">
                    {{
                      p.stackable
                        ? t("promotion.stackable")
                        : t("promotion.exclusive")
                    }}
                  </div>
                  <small v-if="p.min_total">{{
                    t("promotion.threshold_value", {
                      value: money(p.min_total, p.currency),
                    })
                  }}</small
                  ><small v-if="p.min_quantity > 1">
                    ·
                    {{
                      t("promotion.quantity_value", { value: p.min_quantity })
                    }}</small
                  >
                </td>
                <td class="target-cell">
                  {{ target(p) }}
                  <div v-if="p.currency" class="muted">{{ p.currency }}</div>
                </td>
                <td>
                  <template v-if="p.starts_at || p.ends_at"
                    ><div>
                      {{
                        p.starts_at
                          ? formatTime(p.starts_at)
                          : t("promotion.immediately")
                      }}
                    </div>
                    <div>
                      {{
                        p.ends_at
                          ? formatTime(p.ends_at)
                          : t("promotion.no_end")
                      }}
                    </div></template
                  ><template v-else>{{ t("promotion.permanent") }}</template>
                </td>
                <td>
                  {{ p.used }} / {{ p.usage_limit || t("promotion.unlimited") }}
                </td>
                <td>
                  <n-tag size="small">{{ status(p) }}</n-tag>
                </td>
                <td>
                  <div class="row-actions">
                    <n-button
                      size="small"
                      :disabled="deleting === p.id"
                      @click="edit(p)"
                      >{{ t("common.edit") }}</n-button
                    ><n-button
                      size="small"
                      secondary
                      type="error"
                      :loading="deleting === p.id"
                      @click="remove(p)"
                      >{{ t("common.delete") }}</n-button
                    >
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
      <div v-else-if="loaded && !error" class="panel empty-panel">
        <n-empty :description="t('promotion.empty')" />
      </div>
    </div>
  </div>
  <FormDialog
    v-model:show="editor"
    :title="model.id ? t('promotion.edit') : t('promotion.add')"
    :busy="saving"
  >
    <form id="promotion-form" class="form-grid two" @submit.prevent="save">
      <div class="wide">
        <label class="field-label" for="promotion-name">{{
          t("promotion.name")
        }}</label
        ><n-input
          id="promotion-name"
          v-model:value="model.name"
          :maxlength="120"
          :disabled="saving"
        />
      </div>
      <div>
        <label class="field-label" for="promotion-kind">{{
          t("promotion.kind")
        }}</label
        ><n-select
          id="promotion-kind"
          v-model:value="model.kind"
          :options="kinds"
          :disabled="saving"
        />
      </div>
      <div>
        <label class="field-label">{{ t("common.status") }}</label
        ><label class="promotion-switch"
          ><n-switch
            v-model:value="model.enabled"
            :disabled="saving"
            :aria-label="t('promotion.enabled')"
          /><span>{{ t("promotion.enabled") }}</span></label
        >
      </div>
      <div v-if="model.kind === 'code'" class="wide">
        <label class="field-label" for="promotion-code">{{
          t("promotion.code")
        }}</label>
        <div class="code-editor">
          <n-input
            id="promotion-code"
            v-model:value="model.code"
            :maxlength="64"
            :disabled="saving"
          /><n-button :disabled="saving" @click="generateCode">{{
            t("promotion.generate")
          }}</n-button>
        </div>
        <small class="field-help">{{ t("promotion.code_help") }}</small>
      </div>
      <div>
        <label class="field-label" for="promotion-type">{{
          t("promotion.offer")
        }}</label
        ><n-select
          id="promotion-type"
          :value="model.discount_type"
          :options="types"
          :disabled="saving"
          @update:value="setType"
        />
      </div>
      <div>
        <label class="field-label" for="promotion-value">{{
          model.discount_type === "percentage"
            ? t("promotion.percent_label")
            : t("promotion.fixed_label")
        }}</label
        ><n-input
          id="promotion-value"
          v-model:value="model.value"
          :disabled="saving"
          :input-props="{ inputmode: 'decimal' }"
        />
      </div>
      <div>
        <label class="field-label" for="promotion-currency">{{
          t("common.currency")
        }}</label
        ><n-select
          id="promotion-currency"
          v-model:value="model.currency"
          :options="currencies"
          :disabled="saving"
        />
      </div>
      <div>
        <label class="field-label" for="promotion-scope">{{
          t("promotion.scope")
        }}</label
        ><n-select
          id="promotion-scope"
          v-model:value="model.scope"
          :options="scopes"
          :disabled="saving"
        />
      </div>
      <div v-if="model.scope === 'selected'" class="wide">
        <label class="field-label" for="promotion-products">{{
          t("promotion.products")
        }}</label
        ><n-select
          id="promotion-products"
          v-model:value="model.product_ids"
          multiple
          filterable
          :options="productOptions"
          :disabled="saving"
        />
      </div>
      <div>
        <label class="field-label" for="promotion-min-total">{{
          t("promotion.min_total")
        }}</label
        ><n-input
          id="promotion-min-total"
          v-model:value="model.min_total"
          :disabled="saving"
          placeholder="0"
          :input-props="{ inputmode: 'decimal' }"
        />
      </div>
      <div>
        <label class="field-label" for="promotion-min-quantity">{{
          t("promotion.min_quantity")
        }}</label
        ><n-input-number
          :value="model.min_quantity"
          @update:value="(value) => (model.min_quantity = value ?? 1)"
          :min="1"
          :max="100"
          :precision="0"
          :disabled="saving"
          :input-props="{ id: 'promotion-min-quantity' }"
        />
      </div>
      <div>
        <label class="field-label" for="promotion-start">{{
          t("promotion.starts_at")
        }}</label
        ><n-date-picker
          id="promotion-start"
          v-model:value="model.starts_at"
          type="datetime"
          clearable
          :disabled="saving"
        />
      </div>
      <div>
        <label class="field-label" for="promotion-end">{{
          t("promotion.ends_at")
        }}</label
        ><n-date-picker
          id="promotion-end"
          v-model:value="model.ends_at"
          type="datetime"
          clearable
          :disabled="saving"
        />
      </div>
      <div>
        <label class="field-label" for="promotion-limit">{{
          t("promotion.usage_limit")
        }}</label
        ><n-input-number
          :value="model.usage_limit"
          @update:value="(value) => (model.usage_limit = value ?? 0)"
          :min="0"
          :max="1000000000"
          :precision="0"
          :disabled="saving"
          :input-props="{ id: 'promotion-limit' }"
        />
      </div>
      <div>
        <label class="field-label">{{ t("promotion.stacking") }}</label
        ><label class="promotion-switch"
          ><n-switch
            v-model:value="model.stackable"
            :disabled="saving"
            :aria-label="t('promotion.allow_stacking')"
          /><span>{{ t("promotion.allow_stacking") }}</span></label
        >
      </div>
      <small class="field-help wide">{{ t("promotion.rules_help") }}</small>
      <n-alert v-if="formError" class="wide" type="error">{{
        errorText(formError)
      }}</n-alert>
    </form>
    <template #footer
      ><n-button :disabled="saving" @click="editor = false">{{
        t("common.cancel")
      }}</n-button
      ><n-button
        type="primary"
        attr-type="submit"
        form="promotion-form"
        :loading="saving"
        >{{ t("promotion.save") }}</n-button
      ></template
    >
  </FormDialog>
</template>

<style scoped lang="scss">
.promotion-table-panel {
  padding: 0;
  overflow: hidden;
}
.table-wrap {
  overflow-x: auto;
}
.promotions-table {
  min-width: 1080px;
  width: 100%;
  border-collapse: collapse;
  text-align: left;
  font-size: 13px;
}
.promotions-table th {
  color: #6b7280;
  background: #f9fafb;
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}
.promotions-table th,
.promotions-table td {
  padding: 9px 10px;
  border-bottom: 1px solid #eef0f3;
}
.promotions-table tr:last-child td {
  border-bottom: 0;
}
.promotions-table td {
  white-space: nowrap;
}
.promotions-table td:first-child {
  min-width: 140px;
  max-width: 240px;
  white-space: normal;
  overflow-wrap: anywhere;
}
.form-grid > * {
  min-width: 0;
}
.form-grid .wide {
  grid-column: 1 / -1;
}
.form-grid :deep(.n-date-picker) {
  width: 100%;
}
.promotions-table .target-cell {
  min-width: 160px;
  max-width: 250px;
  overflow-wrap: anywhere;
  white-space: normal;
}
.promotion-switch {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 34px;
}
.code-editor {
  display: flex;
  gap: 8px;
}
.code-editor :deep(.n-input) {
  min-width: 0;
}
</style>
