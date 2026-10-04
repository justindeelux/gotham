<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace, NSpin, NText } from "naive-ui";
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { http } from "@/shared/api/http";
import type { AuthResult } from "@/shared/api/token";
import { describeAuthError, useAuthStore } from "@/features/auth/stores/auth";

const authStore = useAuthStore();
const route = useRoute();
const router = useRouter();

const loadError = ref("");

/** backToLogin returns to the sign-in page after a failed callback. */
async function backToLogin(): Promise<void> {
  await router.replace({ path: "/login", query: { error: "oauth_failed" } });
}

/** clearCode drops the one-time code from the address bar. */
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

    // The code is redeemed against the HttpOnly flow cookie so a crafted
    // callback link cannot plant someone else's session in this browser.
    const response = await http.post<AuthResult>(
      "/auth/oauth/exchange",
      { code },
      { withCredentials: true },
    );
    authStore.setSession(response.data);

    clearCode();

    // Refresh through /auth/me so the shell never renders a placeholder after
    // sign-in (A2-17).
    await authStore.fetchMe().catch(() => {});

    await router.replace({ path: "/dashboard" });
  } catch (error) {
    loadError.value = describeAuthError(error);
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
            {{ loadError ? "Sign-in failed" : "Signing you in" }}
          </h2>
          <NText depth="3">
            {{
              loadError
                ? "GitHub did not return a usable session."
                : "Completing GitHub sign-in…"
            }}
          </NText>
        </div>
        <NAlert v-if="loadError" type="error" :show-icon="true">
          {{ loadError }}
        </NAlert>
        <NButton v-if="loadError" @click="backToLogin">
          Back to sign in
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
  margin: 0 0 4px;
  font-size: 20px;
  font-weight: 700;
  color: var(--fg-2);
}
</style>
