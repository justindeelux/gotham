<script setup lang="ts">
import { useI18n } from "vue-i18n";

const { t } = useI18n();
</script>

<template>
  <p class="auth-footnote">
    {{ t("auth.footnote.hashPrefix") }} <span class="mono">argon2id</span>
    {{ t("auth.footnote.tokenMid") }} <span class="mono">OAuthProvider</span>
    {{ t("auth.footnote.interfaceSuffix") }}
  </p>
</template>

<style scoped>
.auth-footnote {
  margin: 4px 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
  text-align: center;
  line-height: 1.6;
}
</style>
