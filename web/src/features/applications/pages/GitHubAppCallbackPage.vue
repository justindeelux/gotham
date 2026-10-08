<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace, NSpin, NText } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import {
  describeGitHubAppError,
  finishCallback,
  installUrl,
  listGitHubApps,
  recordInstallation,
} from "@/features/applications/api/githubApp";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

type Outcome = "working" | "connected" | "installed" | "failed";

const outcome = ref<Outcome>("working");
// rawError keeps the failure; the banner renders through a computed so a
// language switch refreshes it reactively.
const rawError = ref<unknown>(null);
const loadError = computed<string>(() =>
  rawError.value === null ? "" : describeGitHubAppError(rawError.value),
);

/** clearQuery drops one-time codes from the address bar. */
function clearQuery(): void {
  window.history.replaceState(null, "", "/applications/github-app/callback");
}

/** finishManifest redeems a code/state pair that landed on the SPA route. */
async function finishManifest(code: string, state: string): Promise<void> {
  await finishCallback(code, state);
  outcome.value = "connected";
}

/** finishSetup records the installation_id GitHub returned to setup_url. */
async function finishSetup(installationId: number): Promise<void> {
  const apps = await listGitHubApps();
  // GS-10 owns the Connect/Install UI; until then the setup landing
  // completes the oldest app that still waits for an installation.
  const pending = apps.find((app) => !app.connected);
  if (!pending) {
    throw new Error("no GitHub App waits for an installation");
  }
  const install = await installUrl(pending.id);
  await recordInstallation(pending.id, installationId, install.state);
  outcome.value = "installed";
}

onMounted(async () => {
  try {
    const flag = typeof route.query.github_app === "string" ? route.query.github_app : "";
    if (flag === "connected" || flag === "failed" || flag === "expired") {
      // The public API callback already stored the app and redirected here
      // with a bounded result flag.
      outcome.value = flag === "connected" ? "connected" : "failed";
      clearQuery();
      return;
    }
    const code = typeof route.query.code === "string" ? route.query.code : "";
    const state = typeof route.query.state === "string" ? route.query.state : "";
    if (code && state) {
      await finishManifest(code, state);
      clearQuery();
      return;
    }
    const installationRaw =
      typeof route.query.installation_id === "string" ? route.query.installation_id : "";
    const installationId = Number.parseInt(installationRaw, 10);
    if (installationRaw && Number.isInteger(installationId) && installationId > 0) {
      await finishSetup(installationId);
      clearQuery();
      return;
    }
    throw new Error("unknown GitHub App callback");
  } catch (error) {
    rawError.value = error;
    outcome.value = "failed";
    clearQuery();
  }
});

/** backToProjects leaves the result page for the project list. */
async function backToProjects(): Promise<void> {
  await router.replace({ name: "projects" });
}
</script>

<template>
  <div class="github-app-callback">
    <NCard class="callback-card">
      <NSpace vertical align="center" :size="16">
        <NSpin v-if="outcome === 'working' && !loadError" size="large" />
        <NAlert
          v-if="outcome === 'connected' || outcome === 'installed'"
          type="success"
          :show-icon="true"
        >
          {{
            outcome === "connected"
              ? t("applications.githubAppCallback.connected")
              : t("applications.githubAppCallback.installed")
          }}
        </NAlert>
        <NAlert v-if="outcome === 'failed'" type="error" :show-icon="true">
          {{ loadError || t("applications.githubAppCallback.failed") }}
        </NAlert>
        <NText depth="3">{{ t("applications.githubAppCallback.hint") }}</NText>
        <NButton @click="void backToProjects()">{{
          t("applications.githubAppCallback.back")
        }}</NButton>
      </NSpace>
    </NCard>
  </div>
</template>

<style scoped>
.github-app-callback {
  display: flex;
  justify-content: center;
  padding: 48px 16px;
}

.callback-card {
  max-width: 560px;
  width: 100%;
}
</style>
