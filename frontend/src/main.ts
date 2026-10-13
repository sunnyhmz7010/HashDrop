import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import App from "./App.vue";
import Shop from "./pages/Shop.vue";
import { api, initialized, loadSession } from "./api";
import "./styles.scss";

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/setup", component: () => import("./pages/Setup.vue") },
    { path: "/", component: Shop },
    { path: "/products/:id", component: () => import("./pages/Product.vue") },
    { path: "/orders/:id", component: () => import("./pages/Order.vue") },
    { path: "/claim", component: () => import("./pages/Order.vue") },
    { path: "/admin/login", component: () => import("./pages/Login.vue") },
    {
      path: "/admin/:section?",
      component: () => import("./pages/Admin.vue"),
      meta: { admin: true },
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
      // 网络失败时保留目标路由和订单 Fragment，不能据此判断尚未初始化。
    }
  }
  if (initialized.value === false && to.path !== "/setup") return "/setup";
  if (initialized.value === true && to.path === "/setup") return "/admin/login";
  if (to.meta.admin) {
    try {
      if (!(await loadSession())) return "/admin/login";
      initialized.value = true;
    } catch {
      return "/admin/login";
    }
  }
});
createApp(App).use(router).mount("#app");
