import { ref } from "vue";

export const csrf = ref("");
export const site = ref({ title: "TinyFaka", notice: "" });
export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
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
  });
  const value = await response.json();
  if (!response.ok) {
    if (response.status === 401 && path.startsWith("/admin/")) csrf.value = "";
    throw new APIError(response.status, value.error ?? "请求失败");
  }
  return value as T;
}
export interface Product {
  id: string;
  name: string;
  description: string;
  price: number;
  currency: string;
  kind: string;
  active: boolean;
  available: number;
  reserved: number;
  sold: number;
  config: Record<string, unknown>;
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
  gateway_id: string;
  checkout_url: string;
  payment_status: string;
  delivery_status: string;
  reservation_status: string;
  expires_at: number;
  created_at: number;
  last_error: string;
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
  manual_policy: string;
  key_configured: boolean;
  totp_enabled: boolean;
  adapters: string[];
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
    throw new Error("价格格式或小数位数不正确");
  const n = Number(parts[0] + (parts[1] ?? "").padEnd(scale, "0"));
  if (!Number.isSafeInteger(n) || n <= 0 || n > 9_000_000_000_000)
    throw new Error("价格超出范围");
  return n;
}
export function priceText(minor: number, currency: string): string {
  return money(minor, currency).split(" ")[0]!;
}
export const kindNames: Record<string, string> = {
  card: "卡密",
  account: "账号",
  static: "固定文本",
  api: "业务开通",
};
export const paymentNames: Record<string, string> = {
  creating: "正在创建支付单",
  pending: "等待付款",
  paid: "已付款",
  review: "需要核实",
  closed: "已关闭",
};
export const deliveryNames: Record<string, string> = {
  pending: "等待交付",
  processing: "交付处理中",
  done: "交付完成",
  manual: "人工处理",
};
export function errorText(error: unknown): string {
  return error instanceof Error ? error.message : "操作失败，请重试";
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
