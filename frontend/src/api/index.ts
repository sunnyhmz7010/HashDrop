import { t, translateError } from "../i18n";
import { computed, ref } from "vue";

export const csrf = ref("");
export const initialized = ref<boolean>();
export const site = ref({ title: "TinyFaka", notice: "" });
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
export async function api<T>(
  path: string,
  options: {
    method?: string;
    body?: unknown;
    token?: string;
    signal?: AbortSignal;
  } = {},
): Promise<T> {
  const headers: Record<string, string> = {};
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  if (options.token) headers.Authorization = `Bearer ${options.token}`;
  if (csrf.value) headers["X-CSRF-Token"] = csrf.value;
  const response = await fetch(`/api${path}`, {
    method: options.method ?? "GET",
    headers,
    credentials: "same-origin",
    cache: "no-store",
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
  }).catch((error: unknown) => {
    if (options.signal?.aborted) throw error;
    throw new APIError(0, t("errors.network"));
  });
  const value = await response.json().catch(() => {
    throw new APIError(response.status, t("errors.response_json"));
  });
  if (!response.ok) {
    if (response.status === 401 && path.startsWith("/admin/")) csrf.value = "";
    throw new APIError(
      response.status,
      value.error ?? t("errors.request_failed"),
      typeof value.code === "string" ? value.code : "",
      Array.isArray(value.suggestions)
        ? value.suggestions.filter((item: unknown) => typeof item === "string")
        : [],
    );
  }
  return value as T;
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
  inventory_id?: string;
  inventory_name?: string;
  kind: string;
  active: boolean;
  hidden: boolean;
  available: number;
  reserved: number;
  sold: number;
}
export interface Inventory {
  id: string;
  name: string;
  kind: string;
  has_content: boolean;
  available: number;
  reserved: number;
  sold: number;
  product_count: number;
}
export interface Order {
  id: string;
  product_id: string;
  product_name: string;
  kind: string;
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
  content?: string;
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
  currency: string;
  amounts: number[];
  min_amount: number;
  max_amount: number;
}
export interface Donation {
  id: string;
  title: string;
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
export interface DonationList {
  items: Donation[];
  page: number;
  has_more: boolean;
  totals: { currency: string; count: number; amount: number }[];
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
export const kindNames = computed<Record<string, string>>(() => ({
  card: t("kind.card"),
  account: t("kind.account"),
  static: t("kind.static"),
}));
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
  processing: t("delivery.processing"),
  done: t("delivery.done"),
  cancelled: t("delivery.cancelled"),
  manual: t("delivery.manual"),
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
export async function copyText(value: string): Promise<void> {
  if (window.isSecureContext && navigator.clipboard) {
    await navigator.clipboard.writeText(value);
    return;
  }
  // 普通 HTTP 页面没有 Clipboard API，保留浏览器的用户操作复制能力。
  const input = document.createElement("textarea");
  input.value = value;
  input.style.position = "fixed";
  input.style.opacity = "0";
  document.body.appendChild(input);
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
    return true;
  } catch (error) {
    if (error instanceof APIError && error.status === 401) return false;
    throw error;
  }
}
