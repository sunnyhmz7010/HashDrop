import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import App from "./App.vue";
import Shop from "./pages/Shop.vue";
import { loadSession } from "./api";
import "./styles.scss";

const router = createRouter({
  history: createWebHistory(),
  routes: [
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
  if (to.meta.admin) {
    try {
      if (!(await loadSession())) return "/admin/login";
    } catch {
      return "/admin/login";
    }
  }
});
createApp(App).use(router).mount("#app");
