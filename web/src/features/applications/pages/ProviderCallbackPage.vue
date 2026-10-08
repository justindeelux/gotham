<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

/** providerName renders the connected provider from the result flag. */
const providerName = computed<string>(() => {
  const raw = typeof route.query.provider === "string" ? route.query.provider : "";
  switch (raw.toLowerCase()) {
    case "github":
      return "GitHub";
    case "gitlab":
      return "GitLab";
    case "gitea":
      return "Gitea";
    default:
      return raw;
  }
});

/** connected is true only for the success flag the backend redirects with. */
const connected = computed<boolean>(() => route.query.status === "ok" && providerName.value !== "");

/** hint picks the failure explanation from the fixed reason code. */
const hint = computed<string>(() =>
  route.query.reason === "failed"
    ? t("applications.providerCallback.failedHint")
    : t("applications.providerCallback.invalidHint"),
);

/** backToGitSources returns to the Git sources page, which owns the Connect
 * and Disconnect controls (GS-10). */
async function backToGitSources(): Promise<void> {
  await router.replace({ name: "git-sources" });
}
</script>

<template>
  <div class="callback-page">
    <NCard class="callback-card">
      <NSpace vertical align="center" :size="16">
        <h2 class="callback-title">
          {{
            connected
              ? t("applications.providerCallback.connected", { provider: providerName })
              : t("applications.providerCallback.failed")
          }}
        </h2>
        <NAlert :type="connected ? 'success' : 'error'" :show-icon="true">
          {{
            connected
              ? t("applications.providerCallback.connectedHint")
              : hint
          }}
        </NAlert>
        <NButton @click="backToGitSources">
          {{ t("applications.providerCallback.back") }}
        </NButton>
      </NSpace>
    </NCard>
  </div>
</template>

<style scoped>
.callback-page {
  display: flex;
  justify-content: center;
  padding: 48px 16px;
}

.callback-card {
  max-width: 520px;
  width: 100%;
}

.callback-title {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
}
</style>
