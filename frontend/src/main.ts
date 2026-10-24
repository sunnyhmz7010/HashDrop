import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import App from "./App.vue";
import Shop from "./pages/Shop.vue";
import {
  api,
  csrf,
  initialized,
  loadSession,
  adminAuthenticated,
  sessionError,
  errorText,
} from "./api";
import "./styles.scss";

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/setup", component: () => import("./pages/Setup.vue") },
    { path: "/", component: Shop },
    { path: "/donate", component: () => import("./pages/Donate.vue") },
    { path: "/products/:id", component: () => import("./pages/Product.vue") },
    {
      path: "/center/:section?/:id?",
      component: () => import("./pages/Center.vue"),
      meta: { center: true },
    },
    { path: "/:pathMatch(.*)*", component: Shop },
  ],
});
router.beforeEach(async (to) => {
  if (initialized.value === undefined) {
    try {
      initialized.value = (
        await api<{ initialized: boolean }>("/setup/status")
      ).initialized;
    } catch {
      // 网络失败时保留目标路由，不能据此判断尚未初始化。
    }
  }
  if (initialized.value === false && to.path !== "/setup") return "/setup";
  if (initialized.value === true && to.path === "/setup")
    return { path: "/center", query: { login: "1" } };
  if (to.meta.center) {
    // 与 HashPay 一样只在会话未知时确认登录；页面接口仍逐次鉴权并处理 401。
    if (adminAuthenticated.value === undefined) {
      sessionError.value = "";
      try {
        await loadSession();
      } catch (error) {
        csrf.value = "";
        sessionError.value = errorText(error);
        return;
      }
    }
    const section = String(to.params.section ?? "");
    if (
      !adminAuthenticated.value &&
      !["", "history", "donations"].includes(section) &&
      !(section === "orders" && to.params.id)
    )
      return { path: "/center", query: { login: "1" } };
  }
});
createApp(App).use(router).mount("#app");
