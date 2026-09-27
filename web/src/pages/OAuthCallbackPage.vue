<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace, NSpin, NText } from "naive-ui";
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";

import { describeAuthError, useAuthStore } from "../stores/auth";

interface FragmentTokens {
  access_token: string;
  refresh_token: string;
}

const authStore = useAuthStore();
const router = useRouter();

const loadError = ref("");

/** parseFragment reads the OAuth token pair from the URL fragment. */
function parseFragment(): FragmentTokens | null {
  const fragment = window.location.hash.replace(/^#/, "");
  if (!fragment) {
    return null;
  }

  const params = new URLSearchParams(fragment);
  const accessToken = params.get("access_token");
  const refreshToken = params.get("refresh_token");
  if (!accessToken || !refreshToken) {
    return null;
  }

  return { access_token: accessToken, refresh_token: refreshToken };
}

/** backToLogin returns to the sign-in page after a failed callback. */
async function backToLogin(): Promise<void> {
  await router.replace({ path: "/login", query: { error: "oauth_failed" } });
}

onMounted(async () => {
  try {
    const tokens = parseFragment();
    if (!tokens) {
      await backToLogin();
      return;
    }

    authStore.setSession(tokens);

    // Strip the tokens from the address bar before moving on.
    window.history.replaceState(null, "", "/oauth/callback");

    await router.replace({ path: "/dashboard" });
  } catch (error) {
    loadError.value = describeAuthError(error);
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
