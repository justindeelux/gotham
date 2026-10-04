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

import { useAuthStore } from "@/features/auth";

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
    // Design token for floating surfaces (popovers, dropdowns); the previous
    // #3b3e44 invent a lighter surface that matched no token (B3-7). Kept as a
    // literal because Naive UI parses these colours and rejects CSS vars /
    // color-mix() (console error: `[seemly/rgba]: Invalid color value`).
    // #121315 == color-mix(in oklab, #1e1f22 78%, black) == tokens.css --float.
    popoverColor: "#121315",
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
    borderFocus: "1px solid var(--accent-ink)",
    boxShadowFocus:
      "0 0 0 2px var(--bg), 0 0 0 4px var(--accent-ink)",
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
    // Non-solid variants paint their text with the semantic fill colour, which
    // measures ~3.0:1 (primary), 3.66:1 (error), 4.34:1 (success) on the card
    // surface — below WCAG AA. Use the design "ink" tokens (fill mixed toward
    // white) so ghost/text/quaternary text reaches >=4.5:1 in every state
    // (B3-8). Solid buttons keep white text, which already passes.
    textColorHover: "var(--fg-2)",
    textColorPressed: "var(--fg-2)",
    textColorFocus: "var(--fg-2)",
    textColorTextHover: "var(--accent-ink-hover)",
    textColorTextPressed: "var(--accent-ink-hover)",
    textColorTextFocus: "var(--accent-ink-hover)",
    textColorGhostHover: "var(--fg-2)",
    textColorGhostPressed: "var(--fg-2)",
    textColorGhostFocus: "var(--fg-2)",
    textColorTextPrimary: "var(--accent-ink)",
    textColorTextHoverPrimary: "var(--accent-ink-hover)",
    textColorTextPressedPrimary: "var(--accent-ink-hover)",
    textColorTextFocusPrimary: "var(--accent-ink-hover)",
    textColorGhostPrimary: "var(--accent-ink)",
    textColorGhostHoverPrimary: "var(--fg-2)",
    textColorGhostPressedPrimary: "var(--fg-2)",
    textColorGhostFocusPrimary: "var(--fg-2)",
    textColorTextInfo: "var(--accent-ink)",
    textColorTextHoverInfo: "var(--accent-ink-hover)",
    textColorTextPressedInfo: "var(--accent-ink-hover)",
    textColorTextFocusInfo: "var(--accent-ink-hover)",
    textColorGhostInfo: "var(--accent-ink)",
    textColorGhostHoverInfo: "var(--fg-2)",
    textColorGhostPressedInfo: "var(--fg-2)",
    textColorGhostFocusInfo: "var(--fg-2)",
    textColorTextSuccess: "var(--success-ink)",
    textColorTextHoverSuccess: "var(--success-ink)",
    textColorTextPressedSuccess: "var(--success-ink)",
    textColorTextFocusSuccess: "var(--success-ink)",
    textColorGhostSuccess: "var(--success-ink)",
    textColorGhostHoverSuccess: "var(--fg-2)",
    textColorGhostPressedSuccess: "var(--fg-2)",
    textColorGhostFocusSuccess: "var(--fg-2)",
    textColorTextWarning: "var(--warn-ink)",
    textColorTextHoverWarning: "var(--warn-ink)",
    textColorTextPressedWarning: "var(--warn-ink)",
    textColorTextFocusWarning: "var(--warn-ink)",
    textColorGhostWarning: "var(--warn-ink)",
    textColorGhostHoverWarning: "var(--fg-2)",
    textColorGhostPressedWarning: "var(--fg-2)",
    textColorGhostFocusWarning: "var(--fg-2)",
    textColorTextError: "var(--danger-ink)",
    textColorTextHoverError: "var(--danger-ink)",
    textColorTextPressedError: "var(--danger-ink)",
    textColorTextFocusError: "var(--danger-ink)",
    textColorGhostError: "var(--danger-ink)",
    textColorGhostHoverError: "var(--fg-2)",
    textColorGhostPressedError: "var(--fg-2)",
    textColorGhostFocusError: "var(--fg-2)",
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
    colorPopover: "#121315",
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
    // JUS-16: validation error text is smaller than labels/inputs (12px, the
    // --text-xs token) while the #f67c7c error ink keeps readable contrast on
    // the dark surfaces. Set once here so every NFormItem follows it.
    feedbackFontSizeSmall: "12px",
    feedbackFontSizeMedium: "12px",
    feedbackFontSizeLarge: "12px",
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
