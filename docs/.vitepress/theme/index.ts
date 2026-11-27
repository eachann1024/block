import type { Theme } from "vitepress";
import DefaultTheme from "vitepress/theme-without-fonts";
import DocsHome from "./components/DocsHome.vue";
import "./ocean-paper.css";

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component("DocsHome", DocsHome);
  },
} satisfies Theme;
