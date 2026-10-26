<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace, NSpin, NText } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { http } from "@/shared/api/http";
import type { AuthResult } from "@/shared/api/token";
import { describeAuthError, useAuthStore } from "@/features/auth/stores/auth";

const { t } = useI18n();
const authStore = useAuthStore();
const route = useRoute();
const router = useRouter();

const rawError = ref<unknown>(null);
const loadError = computed<string>(() =>
  rawError.value === null ? "" : describeAuthError(rawError.value),
);

async function backToLogin(): Promise<void> {
  await router.replace({ path: "/login", query: { error: "oauth_failed" } });
}

function clearCode(): void {
  window.history.replaceState(null, "", "/oauth/callback");
}

onMounted(async () => {
  try {
    const code = typeof route.query.code === "string" ? route.query.code : "";
    if (!code) {
      await backToLogin();
      return;
    }

    const response = await http.post<AuthResult>(
      "/auth/oauth/exchange",
      { code },
      { withCredentials: true },
    );

    if (response.data.mfa_required && response.data.mfa_token) {
      authStore.setMfaChallenge(response.data.mfa_token);
      clearCode();
      await router.replace({ path: "/login" });
      return;
    }

    authStore.setSession(response.data);

    clearCode();

    await authStore.fetchMe().catch(() => {});

    await router.replace({ path: "/dashboard" });
  } catch (error) {
    rawError.value = error;
    clearCode();
  }
});
</script>

<template>
  <div class="auth-page">
    <NCard class="auth-card">
      <NSpace vertical align="center" :size="16" class="callback-body">
        <NSpin v-if="!loadError" size="large" />
        <div class="callback-copy">
          <h2 class="callback-title">
            {{ loadError ? t("auth.oauth.failed") : t("auth.oauth.signingIn") }}
          </h2>
          <NText depth="3">
            {{
              loadError
                ? t("auth.oauth.noSession")
                : t("auth.oauth.inProgress")
            }}
          </NText>
        </div>
        <NAlert v-if="loadError" type="error" :show-icon="true">
          {{ loadError }}
        </NAlert>
        <NButton v-if="loadError" @click="backToLogin">
          {{ t("auth.oauth.back") }}
        </NButton>
      </NSpace>
    </NCard>
  </div>
</template>

<style scoped>
.callback-body {
  padding: 8px 0;
}

.callback-copy {
  text-align: center;
}

.callback-title {
  margin: 0 0 var(--space-1);
  font-size: var(--text-xl);
  font-weight: 700;
  color: var(--fg-2);
}
</style>
