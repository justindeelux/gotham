<script setup lang="ts">
import {
  darkTheme,
  NConfigProvider,
  NDialogProvider,
  NMessageProvider,
} from "naive-ui";
import type { GlobalThemeOverrides } from "naive-ui";
import { onMounted } from "vue";
import { RouterView, useRoute } from "vue-router";

import { useAuthStore } from "./stores/auth";

const authStore = useAuthStore();
const route = useRoute();

// A session restored from localStorage may not carry the account (OAuth mints
// the token pair before the SPA loads it). Refresh it once on mount so the shell
// does not render a placeholder until a manual reload (A2-17/F3). Skip it on the
// callback route, where the exchange installs a fresh session that this delayed
// response could otherwise overwrite.
onMounted(() => {
  if (route.path === "/oauth/callback") {
    return;
  }
  if (authStore.isAuthenticated && authStore.user === null) {
    void authStore.fetchMe().catch(() => {});
  }
});

// Gotham dark-theme mapping for Naive UI, derived from
// web/src/styles/tokens.css (docs/design is the source of truth).
const themeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: "#5865f2",
    primaryColorHover: "#4752c4",
    primaryColorPressed: "#4c57d0",
    primaryColorSuppl: "rgba(88, 101, 242, 0.16)",
    infoColor: "#5865f2",
    infoColorHover: "#4752c4",
    infoColorPressed: "#4c57d0",
    infoColorSuppl: "rgba(88, 101, 242, 0.16)",
    successColor: "#23a55a",
    successColorHover: "#1d8a4b",
    successColorPressed: "#1a7a43",
    successColorSuppl: "rgba(35, 165, 90, 0.16)",
    warningColor: "#f0b232",
    warningColorHover: "#d99f2c",
    warningColorPressed: "#c08d27",
    warningColorSuppl: "rgba(240, 178, 50, 0.16)",
    errorColor: "#f23f43",
    errorColorHover: "#d9383c",
    errorColorPressed: "#c03236",
    errorColorSuppl: "rgba(242, 63, 67, 0.16)",
    textColorBase: "#dbdee1",
    textColor1: "#f2f3f5",
    textColor2: "#dbdee1",
    textColor3: "#949ba4",
    textColorDisabled: "#80848e",
    placeholderColor: "#80848e",
    placeholderColorDisabled: "rgba(128, 132, 142, 0.5)",
    dividerColor: "rgba(255, 255, 255, 0.06)",
    borderColor: "rgba(255, 255, 255, 0.06)",
    bodyColor: "#313338",
    cardColor: "#2b2d31",
    modalColor: "#2b2d31",
    popoverColor: "#3b3e44",
    tableColor: "#2b2d31",
    tableHeaderColor: "#232428",
    inputColor: "#1e1f22",
    inputColorDisabled: "rgba(30, 31, 34, 0.6)",
    actionColor: "#26272b",
    hoverColor: "rgba(78, 80, 88, 0.3)",
    pressedColor: "rgba(78, 80, 88, 0.6)",
    boxShadow1: "0 0 0 1px rgba(255, 255, 255, 0.06)",
    boxShadow2:
      "rgba(0, 0, 0, 0.4) 0px 2px 4px, 0 0 0 1px rgba(255, 255, 255, 0.06)",
    boxShadow3:
      "rgba(0, 0, 0, 0.5) 0px 8px 24px, 0 0 0 1px rgba(255, 255, 255, 0.06)",
    fontFamily:
      '"Google Sans", "Helvetica Neue", Helvetica, Arial, sans-serif',
    fontFamilyMono:
      '"JetBrains Mono", Consolas, "Andale Mono", "Courier New", monospace',
    borderRadius: "4px",
    borderRadiusSmall: "4px",
  },
  Input: {
    color: "#1e1f22",
    colorFocus: "#1e1f22",
    textColor: "#dbdee1",
    placeholderColor: "#80848e",
    caretColor: "#5865f2",
    border: "1px solid #1e1f22",
    borderHover: "1px solid #3f4147",
    borderFocus: "1px solid #5865f2",
    boxShadowFocus: "0 0 0 3px rgba(88, 101, 242, 0.3)",
    borderRadius: "4px",
  },
  Button: {
    borderRadiusTiny: "4px",
    borderRadiusSmall: "4px",
    borderRadiusMedium: "4px",
    borderRadiusLarge: "4px",
    colorPrimary: "#5865f2",
    colorHoverPrimary: "#4752c4",
    colorPressedPrimary: "#4c57d0",
    colorFocusPrimary: "#5865f2",
    borderPrimary: "1px solid #5865f2",
    borderHoverPrimary: "1px solid #4752c4",
    borderPressedPrimary: "1px solid #4c57d0",
    borderFocusPrimary: "1px solid #5865f2",
    textColorPrimary: "#ffffff",
    textColorHoverPrimary: "#ffffff",
    textColorPressedPrimary: "#ffffff",
    textColorFocusPrimary: "#ffffff",
    rippleColorPrimary: "rgba(88, 101, 242, 0.4)",
  },
  Alert: {
    borderRadius: "4px",
    titleTextColor: "#f2f3f5",
    contentTextColor: "#dbdee1",
    colorInfo: "rgba(88, 101, 242, 0.1)",
    borderInfo: "1px solid rgba(88, 101, 242, 0.34)",
    iconColorInfo: "#97a0f7",
    titleTextColorInfo: "#f2f3f5",
    contentTextColorInfo: "#dbdee1",
    colorSuccess: "rgba(35, 165, 90, 0.16)",
    borderSuccess: "1px solid rgba(35, 165, 90, 0.34)",
    iconColorSuccess: "#61be88",
    titleTextColorSuccess: "#f2f3f5",
    contentTextColorSuccess: "#dbdee1",
    colorWarning: "rgba(240, 178, 50, 0.16)",
    borderWarning: "1px solid rgba(240, 178, 50, 0.34)",
    iconColorWarning: "#f3c057",
    titleTextColorWarning: "#f2f3f5",
    contentTextColorWarning: "#dbdee1",
    colorError: "rgba(242, 63, 67, 0.16)",
    borderError: "1px solid rgba(242, 63, 67, 0.34)",
    iconColorError: "#f67c7c",
    titleTextColorError: "#f2f3f5",
    contentTextColorError: "#dbdee1",
  },
  Card: {
    color: "#2b2d31",
    colorModal: "#2b2d31",
    colorPopover: "#3b3e44",
    colorEmbedded: "#1e1f22",
    textColor: "#dbdee1",
    titleTextColor: "#f2f3f5",
    borderColor: "rgba(255, 255, 255, 0.06)",
    actionColor: "#26272b",
    borderRadius: "8px",
    boxShadow: "0 0 0 1px rgba(255, 255, 255, 0.06)",
  },
  Form: {
    labelTextColor: "#949ba4",
    asteriskColor: "#f23f43",
    feedbackTextColorError: "#f67c7c",
    feedbackTextColorWarning: "#f3c057",
    feedbackTextColor: "#949ba4",
  },
};
</script>

<template>
  <NConfigProvider :theme="darkTheme" :theme-overrides="themeOverrides">
    <NDialogProvider>
      <NMessageProvider>
        <RouterView />
      </NMessageProvider>
    </NDialogProvider>
  </NConfigProvider>
</template>
