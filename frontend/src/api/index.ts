import { t, translateError } from "../i18n";
import { computed, ref } from "vue";

export const csrf = ref("");
export const adminAuthenticated = ref<boolean>();
export const sessionError = ref("");
export const initialized = ref<boolean>();
export const site = ref({ title: "HashDrop", notice: "" });
export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
    public code = "",
    public suggestions: string[] = [],
  ) {
    super(message);
  }
}
async function request(
  path: string,
  options: {
    method?: string;
    body?: unknown;
    token?: string;
    signal?: AbortSignal;
  } = {},
): Promise<Response> {
  const headers: Record<string, string> = {};
  if (options.body !== undefined && !(options.body instanceof FormData))
    headers["Content-Type"] = "application/json";
  if (options.token) {
    // Header 仅传 ASCII，避免浏览器拒绝中文或规范化密钥首尾空格。
    const bytes = new TextEncoder().encode(options.token);
    const encoded = btoa(String.fromCharCode(...bytes))
      .replaceAll("+", "-")
      .replaceAll("/", "_")
      .replace(/=+$/, "");
    headers.Authorization = `ClaimKey ${encoded}`;
  }
  if (csrf.value) headers["X-CSRF-Token"] = csrf.value;
  const response = await fetch(`/api${path}`, {
    method: options.method ?? "GET",
    headers,
    credentials: "same-origin",
    cache: "no-store",
    body:
      options.body instanceof FormData
        ? options.body
        : options.body === undefined
          ? undefined
          : JSON.stringify(options.body),
    signal: options.signal,
  }).catch((error: unknown) => {
    if (options.signal?.aborted) throw error;
    throw new APIError(0, t("errors.network"));
  });
  // 即使代理返回的 401 不是 JSON，也必须及时失效前端会话。
  if (
    response.status === 401 &&
    path.startsWith("/admin/") &&
    path !== "/admin/login"
  ) {
    csrf.value = "";
    adminAuthenticated.value = false;
  }
  if (!response.ok) {
    const value = await response
      .json()
      .catch(() => ({ error: t("errors.request_failed") }));
    throw new APIError(
      response.status,
      value.error ?? t("errors.request_failed"),
      typeof value.code === "string" ? value.code : "",
      Array.isArray(value.suggestions)
        ? value.suggestions.filter((item: unknown) => typeof item === "string")
        : [],
    );
  }
  return response;
}
export async function api<T>(
  path: string,
  options: Parameters<typeof request>[1] = {},
): Promise<T> {
  const response = await request(path, options);
  return response.json().catch(() => {
    throw new APIError(response.status, t("errors.response_json"));
  });
}
export async function assetBlob(
  path: string,
  token?: string,
  signal?: AbortSignal,
): Promise<{ blob: Blob; name: string }> {
  const response = await request(path, { token, signal });
  return {
    blob: await response.blob(),
    name: assetFilename(response.headers.get("Content-Disposition")),
  };
}
function assetFilename(disposition: string | null): string {
  const parameters = new Map<string, string>();
  // 按完整参数读取，避免文件名内的分号被误识别为新参数。
  for (const match of (disposition ?? "").matchAll(
    /(?:^|;)\s*([a-z0-9_.*-]+)\s*=\s*(?:"((?:[^"\\]|\\.)*)"|([^;]*))/gi,
  )) {
    parameters.set(
      match[1]!.toLowerCase(),
      match[2] !== undefined
        ? match[2].replace(/\\(.)/g, "$1")
        : match[3]!.trim(),
    );
  }
  let name = parameters.get("filename") ?? "";
  const extended = parameters.get("filename*")?.match(/^UTF-8'[^']*'(.*)$/i);
  if (extended) {
    try {
      name = decodeURIComponent(extended[1]!);
    } catch {
      // 非法编码回退到普通 filename；标题不参与下载文件名。
      name = parameters.get("filename") ?? "";
    }
  }
  name = name
    .replace(/[/\\]/g, "_")
    .replace(/[\u0000-\u001f\u007f]/g, "")
    .trim();
  return name && name !== "." && name !== ".." ? name : "download";
}
export interface ContentBlock {
  id: string;
  type: "text" | "image" | "file" | "link" | "stock";
  text?: string;
  label?: string;
  url?: string;
  asset_id?: string;
  quantity?: number;
}
export interface ContentDocument {
  blocks: ContentBlock[];
}
export interface Asset {
  id: string;
  name: string;
  media_type: string;
  size: number;
}
export interface InventoryBinding {
  product_id: string;
  slot_id: string;
}
export interface Product {
  id: string;
  name: string;
  description: string;
  details: string;
  price: number;
  sale_price?: number;
  offer_name?: string;
  currency: string;
  inventory_strategy: "random" | "oldest" | "newest";
  inventory_count: number;
  stock_unlimited: boolean;
  delivery_plan?: ContentDocument;
  delivery_note: string;
  min_quantity: number;
  max_quantity: number;
  active: boolean;
  hidden: boolean;
  available: number;
  reserved: number;
  sold: number;
}
export interface Inventory {
  id: string;
  name: string;
  reusable: boolean;
  available: number;
  reserved: number;
  sold: number;
  product_count: number;
  bindings: InventoryBinding[];
}
export interface Order {
  id: string;
  product_id: string;
  product_name: string;
  unit_price: number;
  currency: string;
  quantity: number;
  total: number;
  original_total: number;
  discount_total: number;
  discounts: OrderDiscount[];
  gateway_id: string;
  checkout_url: string;
  payment_status: string;
  delivery_status: string;
  reservation_status: string;
  expires_at: number;
  created_at: number;
  last_error: string;
  delivery_note: string;
}
export interface OrderDiscount {
  id: string;
  name: string;
  code: string;
  amount: number;
}
export interface OrderQuote {
  original_total: number;
  discount_total: number;
  total: number;
  currency: string;
  discounts: OrderDiscount[];
}
export interface Promotion {
  id: string;
  name: string;
  kind: "automatic" | "code";
  code: string;
  discount_type: "percentage" | "fixed";
  value: number;
  currency: string;
  min_total: number;
  min_quantity: number;
  scope: "all" | "selected";
  product_ids: string[];
  stackable: boolean;
  enabled: boolean;
  starts_at: number;
  ends_at: number;
  usage_limit: number;
  used: number;
}
export interface Delivery {
  id: string;
  ordinal: number;
  status: string;
  content?: ContentDocument;
  error?: string;
}
export interface OrderDetail {
  order: Order;
  deliveries: Delivery[];
}
export interface Settings {
  title: string;
  notice: string;
  public_url: string;
  hashpay_url: string;
  merchant_id: string;
  key_configured: boolean;
}
export interface DonationSettings {
  enabled: boolean;
  title: string;
  description: string;
  currencies: DonationCurrencySettings[];
}
export interface DonationCurrencySettings {
  currency: string;
  amounts: number[];
  min_amount: number;
  max_amount: number;
}
export interface Donation {
  id: string;
  title: string;
  nickname: string;
  show_public: boolean;
  amount: number;
  currency: string;
  gateway_id: string;
  checkout_url: string;
  payment_status: string;
  expires_at: number;
  paid_at: number;
  created_at: number;
  last_error: string;
}
export interface PublicDonation {
  nickname: string;
  amount: number;
  currency: string;
  paid_at: number;
}
export interface PublicDonationList {
  items: PublicDonation[];
  page: number;
  has_more: boolean;
}
export interface DonationList {
  items: Donation[];
  page: number;
  has_more: boolean;
  totals: { currency: string; count: number; amount: number }[];
}
export interface DonationLookupList {
  items: Donation[];
  page: number;
  has_more: boolean;
  matched_request_id: string;
}
const scales: Record<string, number> = {
  CNY: 2,
  USD: 2,
  EUR: 2,
  GBP: 2,
  HKD: 2,
  JPY: 0,
  USDT: 6,
  USDC: 6,
};
export function money(minor: number, currency: string): string {
  const scale = scales[currency] ?? 2;
  return `${(minor / 10 ** scale).toFixed(scale)} ${currency}`;
}
export function toMinor(value: string, currency: string): number {
  const scale = scales[currency] ?? 2;
  const parts = value.trim().split(".");
  if (
    parts.length > 2 ||
    !/^\d+$/.test(parts[0] ?? "") ||
    !/^[0-9]*$/.test(parts[1] ?? "") ||
    (parts[1]?.length ?? 0) > scale
  )
    throw new Error(t("errors.price_format"));
  const n = Number(parts[0] + (parts[1] ?? "").padEnd(scale, "0"));
  if (!Number.isSafeInteger(n) || n <= 0 || n > 9_000_000_000_000)
    throw new Error(t("errors.price_range"));
  return n;
}
export function priceText(minor: number, currency: string): string {
  return money(minor, currency).split(" ")[0]!;
}
export const paymentNames = computed<Record<string, string>>(() => ({
  creating: t("payment.creating"),
  pending: t("payment.pending"),
  paid: t("payment.paid"),
  expired: t("payment.expired"),
  invalid: t("payment.invalid"),
  review: t("payment.review"),
  closed: t("payment.closed"),
}));
export const deliveryNames = computed<Record<string, string>>(() => ({
  pending: t("delivery.pending"),
  partial: t("delivery.partial"),
  failed: t("delivery.failed"),
  done: t("delivery.done"),
  cancelled: t("delivery.cancelled"),
}));
export function errorText(error: unknown): string {
  if (error instanceof SyntaxError) return t("errors.invalid_json");
  const message =
    error instanceof Error
      ? error.message
      : typeof error === "string"
        ? error
        : t("errors.retry");
  return translateError(message);
}
export async function copyText(
  value: string,
  source: Element | null = document.activeElement,
): Promise<void> {
  if (window.isSecureContext && navigator.clipboard) {
    await navigator.clipboard.writeText(value);
    return;
  }
  // 普通 HTTP 页面没有 Clipboard API，保留浏览器的用户操作复制能力。
  const input = document.createElement("textarea");
  input.value = value;
  // 只读文本仍可选中复制，同时避免移动端唤起输入法。
  input.readOnly = true;
  input.style.position = "fixed";
  input.style.opacity = "0";
  // 留在当前弹窗内，避免焦点约束将焦点转回可编辑输入框。
  const container = source?.closest('[role="dialog"]') ?? document.body;
  container.appendChild(input);
  try {
    input.select();
    if (!document.execCommand("copy")) throw new Error(t("errors.copy_failed"));
  } finally {
    input.remove();
  }
}
export async function loadSession(): Promise<boolean> {
  try {
    const result = await api<{ csrf: string }>("/admin/session");
    csrf.value = result.csrf;
    adminAuthenticated.value = true;
    return true;
  } catch (error) {
    if (error instanceof APIError && error.status === 401) return false;
    throw error;
  }
}
