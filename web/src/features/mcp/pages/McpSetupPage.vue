<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NInput,
  NModal,
  NRadio,
  NRadioGroup,
  NSpace,
  NSpin,
} from "naive-ui";
import { onMounted } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import { useAuthStore } from "@/features/auth";
import McpSnippet from "@/features/mcp/components/McpSnippet.vue";
import { useMcpSetupPage } from "@/features/mcp/composables/useMcpSetupPage";
import type { McpSnippetId } from "@/features/mcp/utils/snippets";
import { useCopyText } from "@/shared/composables/useCopyText";

const clientLabelKeys: Record<McpSnippetId, string> = {
  "claude-code": "mcp.clients.claudeCode",
  "url-headers": "mcp.clients.urlHeaders",
  "url-token": "mcp.clients.urlToken",
};

const { t } = useI18n();
const authStore = useAuthStore();
const router = useRouter();
const page = useMcpSetupPage();
const { copyText } = useCopyText();

function clientLabel(id: McpSnippetId): string {
  return t(clientLabelKeys[id]);
}

function openInstanceSettings(): void {
  void router.push({ name: "instance" });
}

onMounted(() => {
  if (authStore.isAuthenticated) {
    void authStore.fetchMe().catch(() => {});
  }
});
</script>

<template>
  <div class="mcp-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("mcp.page.eyebrow") }}</p>
        <h1>{{ t("mcp.page.title") }}</h1>
        <p class="page-desc">{{ t("mcp.page.description") }}</p>
      </div>
    </div>

    <NAlert v-if="page.errorMessage.value" type="error" :show-icon="true">
      {{ page.errorMessage.value }}
    </NAlert>
    <div v-if="page.errorMessage.value" class="retry-row">
      <NButton size="small" @click="page.load">
        {{ t("mcp.errors.retry") }}
      </NButton>
    </div>

    <NAlert
      v-if="page.connection.value && !page.connection.value.https"
      type="warning"
      :show-icon="true"
      data-testid="mcp-https-warning"
    >
      <template #header>{{ t("mcp.https.title") }}</template>
      <div class="https-body">
        <span>{{ authStore.isOwner ? t("mcp.https.owner") : t("mcp.https.member") }}</span>
        <NButton
          v-if="authStore.isOwner"
          size="small"
          @click="openInstanceSettings"
        >
          {{ t("mcp.https.open") }}
        </NButton>
      </div>
    </NAlert>

    <NSpin :show="page.loading.value">
      <div v-if="page.connection.value" class="panels">
        <NCard :title="t('mcp.access.title')">
          <NSpace vertical :size="14">
            <NRadioGroup v-model:value="page.accessLevel.value">
              <NSpace vertical :size="10">
                <NRadio value="read">
                  <span class="option">
                    <strong>{{ t("mcp.access.read.name") }}</strong>
                    <span class="option-description">
                      {{ t("mcp.access.read.description") }}
                    </span>
                  </span>
                </NRadio>
                <NRadio value="deploy">
                  <span class="option">
                    <strong>{{ t("mcp.access.deploy.name") }}</strong>
                    <span class="option-description">
                      {{ t("mcp.access.deploy.description") }}
                    </span>
                  </span>
                </NRadio>
              </NSpace>
            </NRadioGroup>
            <p class="hint">
              {{
                authStore.isOwner
                  ? t("mcp.access.hint")
                  : t("mcp.access.memberHint")
              }}
            </p>
            <NAlert
              v-if="page.createErrorMessage.value"
              type="error"
              :show-icon="true"
            >
              {{ page.createErrorMessage.value }}
            </NAlert>
            <div>
              <NButton
                type="primary"
                data-testid="mcp-create-token"
                :loading="page.createBusy.value"
                @click="page.create"
              >
                {{ t("mcp.access.create") }}
              </NButton>
            </div>
          </NSpace>
        </NCard>

        <NCard :title="t('mcp.clients.title')">
          <NSpace vertical :size="12">
            <p class="hint">{{ t("mcp.clients.hint") }}</p>
            <McpSnippet
              v-for="snippet in page.snippets.value"
              :key="snippet.id"
              :snippet="snippet"
              :label="clientLabel(snippet.id)"
            />
          </NSpace>
        </NCard>
      </div>
    </NSpin>

    <NModal
      :show="page.revealed.value !== null"
      preset="card"
      class="app-modal"
      style="width: 720px; max-width: 94vw"
      :title="t('mcp.reveal.title')"
      :mask-closable="false"
      @update:show="(value: boolean) => { if (!value) page.closeReveal(); }"
    >
      <NSpace v-if="page.revealed.value" vertical :size="14">
        <NAlert type="warning" :show-icon="true">
          {{ t("mcp.reveal.warning") }}
        </NAlert>
        <div class="token-row">
          <NInput
            :value="page.revealed.value.token"
            readonly
            class="mono token-field"
            :input-props="{
              id: 'mcp-token-secret',
              'aria-label': t('mcp.reveal.aria'),
            }"
          />
          <NButton
            @click="copyText(page.revealed.value.token, t('mcp.reveal.aria'))"
          >
            {{ t("mcp.reveal.copy") }}
          </NButton>
        </div>
        <McpSnippet
          v-for="snippet in page.revealedSnippets.value"
          :key="snippet.id"
          :snippet="snippet"
          :label="clientLabel(snippet.id)"
        />
      </NSpace>
      <template #footer>
        <NSpace justify="end">
          <NButton type="primary" data-testid="mcp-reveal-done" @click="page.closeReveal">
            {{ t("mcp.reveal.done") }}
          </NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.mcp-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.retry-row {
  margin-top: calc(var(--space-2) * -1);
}

.panels {
  display: grid;
  grid-template-columns: minmax(0, 360px) minmax(0, 1fr);
  gap: var(--space-4);
  align-items: start;
}

.hint {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-xs);
}

.https-body {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.option {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.option-description {
  color: var(--muted);
  font-size: var(--text-xs);
}

.mono {
  font-family: var(--font-mono);
}

.token-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.token-field {
  flex: 1 1 auto;
  min-width: 0;
}

@media (max-width: 900px) {
  .panels {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
