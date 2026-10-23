<script setup lang="ts">
import { NAlert, NButton, NCard, NSpin, NText } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { acceptInvite, describeTeamError } from "@/features/teams";
import { stripErrorPrefix } from "@/features/servers";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const token = ref<string>(String(route.query.token ?? ""));
const accepting = ref(false);
const rawFailure = ref<unknown>(null);
const knownTeamStatuses = [400, 401, 403, 404, 409, 410];
const error = computed<string | null>(() => {
  if (rawFailure.value === null) {
    return null;
  }
  if (
    rawFailureMessage(rawFailure.value) === "" &&
    !knownTeamStatuses.includes(rawFailureStatus(rawFailure.value))
  ) {
    return t("common.errors.unexpected");
  }
  return `${t("common.errors.requestFailed")}: ${describeTeamError(rawFailure.value)}`;
});
const joined = ref("");

function rawFailureStatus(failure: unknown): number {
  if (typeof failure !== "object" || failure === null) {
    return -1;
  }
  const status = (failure as { status?: unknown }).status;
  return typeof status === "number" ? status : -1;
}

function rawFailureMessage(failure: unknown): string {
  if (typeof failure !== "object" || failure === null) {
    return "";
  }
  const message = (failure as { message?: unknown }).message;
  return typeof message === "string" ? stripErrorPrefix(message).trim() : "";
}

async function handleAccept(): Promise<void> {
  accepting.value = true;
  rawFailure.value = null;
  try {
    const team = await acceptInvite(token.value);
    joined.value = team.name;
    scrubToken();
  } catch (err) {
    rawFailure.value = err;
  } finally {
    accepting.value = false;
  }
}

function scrubToken(): void {
  void router.replace({ query: {} });
}

function goToTeams(): void {
  void router.push({ name: "teams" });
}

onMounted(() => {
  if (token.value) {
    void handleAccept();
  }
});
</script>

<template>
  <div class="invite-page">
    <NCard :title="t('auth.invite.title')">
      <NSpin :show="accepting">
        <div class="stack">
          <NAlert v-if="joined" type="success" :show-icon="true">
            {{ t("auth.invite.joined", { name: joined }) }}
          </NAlert>
          <NAlert v-else-if="!token" type="warning" :show-icon="true">
            {{ t("auth.invite.noToken") }}
          </NAlert>
          <NAlert v-else-if="error" type="error" :show-icon="true">
            {{ error }}
          </NAlert>
          <NText v-if="!joined" depth="3">
            {{ t("auth.invite.hint") }}
          </NText>
          <NButton type="primary" @click="goToTeams">
            {{ joined ? t("auth.invite.openTeams") : t("auth.invite.backToTeams") }}
          </NButton>
        </div>
      </NSpin>
    </NCard>
  </div>
</template>

<style scoped>
.invite-page {
  max-width: 560px;
}

.stack {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  align-items: flex-start;
}
</style>
