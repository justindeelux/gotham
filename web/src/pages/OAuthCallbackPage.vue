<script setup lang="ts">
import { NSpace, NSpin, NText } from "naive-ui";
import { onMounted } from "vue";
import { useRouter } from "vue-router";

import { useAuthStore } from "../stores/auth";

interface FragmentTokens {
  access_token: string;
  refresh_token: string;
}

const authStore = useAuthStore();
const router = useRouter();

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

onMounted(async () => {
  const tokens = parseFragment();
  if (!tokens) {
    await router.replace({ path: "/login", query: { error: "oauth_failed" } });
    return;
  }

  authStore.setSession(tokens);

  // Strip the tokens from the address bar before moving on.
  window.history.replaceState(null, "", "/oauth/callback");

  await router.replace({ path: "/dashboard" });
});
</script>

<template>
  <div class="auth-page">
    <NSpace vertical align="center" :size="16">
      <NSpin size="large" />
      <NText depth="3">Signing you in…</NText>
    </NSpace>
  </div>
</template>
