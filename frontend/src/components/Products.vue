<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NSelect,
  NSpin,
  NSwitch,
  useDialog,
  useMessage,
} from "naive-ui";
import FormDialog from "./FormDialog.vue";
import {
  api,
  errorText,
  kindNames,
  money,
  priceText,
  toMinor,
  type Product,
} from "../api";
const message = useMessage(),
  dialog = useDialog(),
  products = ref<Product[]>([]),
  loading = ref(false),
  error = ref(""),
  editor = ref(false),
  saving = ref(false),
  formError = ref("");
const model = reactive({
  id: "",
  name: "",
  description: "",
  price: "",
  currency: "CNY",
  kind: "card",
  active: false,
  static_content: "",
  config: "{}",
});
const currencies = [
  "CNY",
  "USD",
  "EUR",
  "GBP",
  "HKD",
  "JPY",
  "USDT",
  "USDC",
].map((value) => ({ label: value, value }));
const kinds = Object.entries(kindNames).map(([value, label]) => ({
  value,
  label,
}));
const stockNames: Record<string, string> = {
  available: "可用",
  reserved: "已预留",
  sold: "已售出",
};
async function load() {
  loading.value = true;
  error.value = "";
  try {
    products.value = await api("/admin/products");
  } catch (e) {
    error.value = errorText(e);
  } finally {
    loading.value = false;
  }
}
onMounted(load);
function edit(product?: Product) {
  Object.assign(
    model,
    product
      ? {
          ...product,
          price: priceText(product.price, product.currency),
          static_content: "",
          config: JSON.stringify(product.config, null, 2),
        }
      : {
          id: "",
          name: "",
          description: "",
          price: "",
          currency: "CNY",
          kind: "card",
          active: false,
          static_content: "",
          config: "{}",
        },
  );
  formError.value = "";
  editor.value = true;
}
async function save() {
  if (saving.value) return;
  saving.value = true;
  formError.value = "";
  try {
    await api(`/admin/products${model.id ? `/${model.id}` : ""}`, {
      method: model.id ? "PUT" : "POST",
      body: {
        name: model.name,
        description: model.description,
        price: toMinor(model.price, model.currency),
        currency: model.currency,
        kind: model.kind,
        active: model.active,
        static_content: model.static_content,
        config: JSON.parse(model.config),
      },
    });
    editor.value = false;
    message.success("商品已保存");
    await load();
  } catch (e) {
    formError.value = errorText(e);
  } finally {
    saving.value = false;
  }
}
const stockProduct = ref<Product>(),
  importing = ref(false),
  stockText = ref(""),
  stockError = ref(""),
  stockDialog = ref(false),
  stockLoading = ref(false),
  stockItems = ref<
    { id: string; status: string; order_id: string; created_at: number }[]
  >([]);
function importFor(p: Product) {
  stockProduct.value = p;
  stockText.value = "";
  stockError.value = "";
  importing.value = true;
}
async function importItems() {
  if (saving.value || !stockProduct.value) return;
  saving.value = true;
  stockError.value = "";
  try {
    const result = await api<{ added: number; duplicates: number }>(
      `/admin/products/${stockProduct.value.id}/stock`,
      { method: "POST", body: { content: stockText.value } },
    );
    importing.value = false;
    stockText.value = "";
    message.success(
      `新增 ${result.added} 份，跳过重复 ${result.duplicates} 份`,
    );
    await load();
  } catch (e) {
    stockError.value = errorText(e);
  } finally {
    saving.value = false;
  }
}
let stockGeneration = 0;
let stockController: AbortController | undefined;
function cancelStock() {
  stockGeneration++;
  stockController?.abort();
  stockLoading.value = false;
}
watch(stockDialog, (value) => {
  if (!value) cancelStock();
});
onUnmounted(cancelStock);
async function viewStock(p: Product) {
  cancelStock();
  const current = stockGeneration;
  stockController = new AbortController();
  stockProduct.value = p;
  stockItems.value = [];
  stockError.value = "";
  stockDialog.value = true;
  stockLoading.value = true;
  try {
    const result = await api<typeof stockItems.value>(
      `/admin/products/${p.id}/stock`,
      { signal: stockController.signal },
    );
    if (current === stockGeneration) stockItems.value = result;
  } catch (e) {
    if (current === stockGeneration) stockError.value = errorText(e);
  } finally {
    if (current === stockGeneration) stockLoading.value = false;
  }
}
function removeStock(id: string) {
  const p = stockProduct.value;
  if (!p) return;
  dialog.warning({
    title: "删除这份库存？",
    content: "删除后无法恢复，仅允许删除尚未预留的库存。",
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await api(`/admin/products/${p.id}/stock/${id}`, { method: "DELETE" });
        await viewStock(p);
        await load();
      } catch (e) {
        message.error(errorText(e));
        return false;
      }
    },
  });
}
</script>
<template>
  <div class="section-title space-bottom">
    <h2>商品与库存</h2>
    <n-button type="primary" @click="edit()">新增商品</n-button>
  </div>
  <n-alert v-if="error" type="error" class="space-bottom"
    >{{ error }} <n-button text @click="load">重试</n-button></n-alert
  >
  <n-spin :show="loading"
    ><div v-if="products.length" class="panel table-panel">
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>商品</th>
              <th>类型</th>
              <th>价格</th>
              <th>库存：可用 / 预留 / 已售</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in products" :key="p.id">
              <td class="name-cell">{{ p.name }}</td>
              <td>{{ kindNames[p.kind] }}</td>
              <td>{{ money(p.price, p.currency) }}</td>
              <td>
                {{
                  ["card", "account"].includes(p.kind)
                    ? `${p.available} / ${p.reserved} / ${p.sold}`
                    : "不消耗库存"
                }}
              </td>
              <td>{{ p.active ? "已上架" : "已下架" }}</td>
              <td>
                <div class="row-actions">
                  <n-button size="small" @click="edit(p)">编辑</n-button
                  ><template v-if="['card', 'account'].includes(p.kind)"
                    ><n-button size="small" @click="importFor(p)">导入</n-button
                    ><n-button size="small" @click="viewStock(p)"
                      >库存</n-button
                    ></template
                  >
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <div v-else-if="!loading && !error" class="panel empty-panel">
      <n-empty description="还没有商品"
        ><template #extra
          ><n-button @click="edit()">新增商品</n-button></template
        ></n-empty
      >
    </div></n-spin
  >
  <FormDialog
    v-model:show="editor"
    :title="model.id ? '编辑商品' : '新增商品'"
    :busy="saving"
    ><form id="product-form" @submit.prevent="save">
      <div class="form-grid two">
        <div class="wide">
          <label class="field-label" for="product-name">商品名称</label
          ><n-input
            id="product-name"
            v-model:value="model.name"
            :maxlength="120"
          />
        </div>
        <div>
          <label class="field-label" for="product-kind">交付类型</label
          ><n-select
            id="product-kind"
            v-model:value="model.kind"
            :options="kinds"
            :disabled="!!model.id"
          />
        </div>
        <div>
          <label class="field-label" for="currency">币种</label
          ><n-select
            id="currency"
            v-model:value="model.currency"
            :options="currencies"
          />
        </div>
        <div>
          <label class="field-label" for="price">售价</label
          ><n-input
            id="price"
            v-model:value="model.price"
            placeholder="例如 10.00"
            :input-props="{ inputmode: 'decimal' }"
          />
        </div>
        <div>
          <label class="field-label" for="active">上架状态</label
          ><n-switch id="active" v-model:value="model.active" />
          <span class="muted">{{ model.active ? "上架" : "下架" }}</span>
        </div>
        <div class="wide">
          <label class="field-label" for="description">商品说明</label
          ><n-input
            id="description"
            v-model:value="model.description"
            type="textarea"
            :maxlength="4000"
          />
        </div>
        <div v-if="model.kind === 'static'" class="wide">
          <label class="field-label" for="static-content">固定交付内容</label
          ><n-input
            id="static-content"
            v-model:value="model.static_content"
            type="textarea"
            :placeholder="
              model.id
                ? '留空保留现有内容；新内容只影响后续订单'
                : '填写付款后展示的内容'
            "
          />
        </div>
        <div v-if="model.kind === 'api'" class="wide">
          <n-alert type="info"
            >只支持服务端已部署的适配器。未部署时可保存草稿，不能上架。</n-alert
          ><label class="field-label" for="api-config">非敏感适配器配置</label
          ><n-input
            id="api-config"
            v-model:value="model.config"
            type="textarea"
            placeholder='{"adapter":"名称","parameters":{}}'
          />
        </div>
      </div>
      <n-alert v-if="formError" type="error" class="space-top">{{
        formError
      }}</n-alert>
    </form>
    <template #footer>
      <n-button :disabled="saving" @click="editor = false">取消</n-button
      ><n-button
        attr-type="submit"
        form="product-form"
        type="primary"
        :loading="saving"
        >保存商品</n-button
      >
    </template>
  </FormDialog>
  <FormDialog
    v-model:show="importing"
    :title="`导入库存 · ${stockProduct?.name ?? ''}`"
    :busy="saving"
    ><p class="muted">
      每行一份卡密或账号。重复内容会自动跳过；账号与密码请放在同一行。
    </p>
    <n-input
      v-model:value="stockText"
      type="textarea"
      :autosize="{ minRows: 8, maxRows: 18 }"
      placeholder="每行一份库存"
      aria-label="库存内容"
    /><n-alert v-if="stockError" type="error" class="space-top">{{
      stockError
    }}</n-alert>
    <template #footer>
      <n-button :disabled="saving" @click="importing = false">取消</n-button
      ><n-button type="primary" :loading="saving" @click="importItems"
        >导入库存</n-button
      >
    </template></FormDialog
  >
  <FormDialog
    v-model:show="stockDialog"
    :title="`库存 · ${stockProduct?.name ?? ''}`"
    ><n-alert v-if="stockError" type="error">{{ stockError }}</n-alert
    ><n-spin :show="stockLoading"
      ><div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>库存编号</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in stockItems" :key="item.id">
              <td class="code-cell">{{ item.id }}</td>
              <td>{{ stockNames[item.status] }}</td>
              <td>
                <n-button
                  v-if="item.status === 'available'"
                  size="small"
                  type="error"
                  secondary
                  @click="removeStock(item.id)"
                  >删除</n-button
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <n-empty
        v-if="!stockItems.length && !stockLoading"
        description="暂无库存"
    /></n-spin>
    <p class="helper">
      显示最新 200 条记录，敏感内容不在列表中回显。
    </p></FormDialog
  >
</template>

<style scoped lang="scss">
.table-panel {
  padding: 0;
  overflow: hidden;
}

.table-scroll {
  overflow-x: auto;
}

.table-scroll table {
  width: 100%;
  border-collapse: collapse;
  text-align: left;
  font-size: 13px;
}

.table-scroll th {
  color: #6b7280;
  background: #f9fafb;
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}

.table-scroll th,
.table-scroll td {
  padding: 9px 10px;
  border-bottom: 1px solid #eef0f3;
}

.table-scroll tr:last-child td {
  border-bottom: 0;
}

.table-scroll td {
  white-space: nowrap;
}

.table-scroll .name-cell {
  min-width: 8rem;
  max-width: 16rem;
  white-space: normal;
  overflow-wrap: anywhere;
}

.table-scroll .row-actions {
  flex-wrap: nowrap;
}

.form-grid .field-label {
  margin-bottom: 7px;
}

.form-grid .wide {
  grid-column: 1 / -1;
}
</style>
