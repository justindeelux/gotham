<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NInput,
  NPopconfirm,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import { computed, onMounted, toRef } from "vue";
import { useI18n } from "vue-i18n";

import type {
  Application,
  ApplicationDomain,
} from "@/features/applications/api/applications";
import { useApplicationDomain } from "@/features/applications/composables/useApplicationDomain";
import { useApplicationDomains } from "@/features/applications/composables/useApplicationDomains";
import {
  certificateStatusLabel,
  certificateStatusTagType,
  useProxyStore,
  type Certificate,
} from "@/features/domains";

interface Props {
  application: Application;
}

const props = defineProps<Props>();

const domain = useApplicationDomain(toRef(props, "application"));
const aliases = useApplicationDomains(toRef(props, "application"));
const proxyStore = useProxyStore();

const { t } = useI18n();

/** aliasRows lists every non-primary hostname; the primary stays above. */
const aliasRows = computed<ApplicationDomain[]>(() =>
  aliases.domains.value.filter((row) => !row.is_primary),
);

/** certOf resolves the certificate intent recorded for one hostname. */
function certOf(host: string): Certificate | null {
  return (
    proxyStore.certificates.find(
      (item) => item.application_id === props.application.id && item.domain === host,
    ) ?? null
  );
}

onMounted(() => {
  void proxyStore.fetchCertificates().catch(() => undefined);
});
</script>

<template>
  <NSpace vertical :size="16">
    <NCard :title="t('applications.domain.title')">
      <NSpace vertical :size="12">
        <NText depth="3">
          {{ t("applications.domain.hint") }}
        </NText>
        <NAlert
          v-if="domain.domainError.value"
          type="error"
          :show-icon="true"
        >
          {{ domain.domainError.value }}
        </NAlert>
        <NSpace align="center" :size="8" :wrap="false">
          <NInput
            v-model:value="domain.baseDomain.value"
            class="mono"
            style="max-width: 360px"
            placeholder="app.example.com"
            :input-props="{ 'aria-label': t('applications.domain.baseAria') }"
            @keyup.enter="domain.handleSaveDomain"
          />
          <NButton
            type="primary"
            :loading="domain.savingDomain.value"
            @click="domain.handleSaveDomain"
          >
            {{ t("applications.domain.save") }}
          </NButton>
        </NSpace>
        <NText depth="3" class="small">
          {{ t("applications.domain.emptyHint") }}
        </NText>
      </NSpace>
    </NCard>

    <NCard :title="t('applications.domain.aliasTitle')">
      <NSpace vertical :size="12">
        <NAlert
          v-if="aliases.domainsError.value"
          type="error"
          :show-icon="true"
        >
          {{ aliases.domainsError.value }}
        </NAlert>
        <NSpace vertical :size="8">
          <NSpace align="center" :size="8" :wrap="false">
            <NInput
              v-model:value="aliases.newDomain.value"
              class="mono"
              style="max-width: 360px"
              placeholder="www.example.com"
              :input-props="{ 'aria-label': t('applications.domain.aliasAria') }"
              @keyup.enter="aliases.handleAddDomain"
            />
            <NButton
              type="primary"
              ghost
              :loading="aliases.addingDomain.value"
              @click="aliases.handleAddDomain"
            >
              {{ t("applications.domain.add") }}
            </NButton>
          </NSpace>
          <NAlert
            v-if="aliases.addError.value"
            type="error"
            :show-icon="true"
          >
            {{ aliases.addError.value }}
          </NAlert>
          <NText depth="3">
            {{ t("applications.domain.aliasHint") }}
          </NText>
        </NSpace>
        <NSpin :show="aliases.loadingDomains.value">
          <NSpace
            v-if="aliasRows.length > 0"
            vertical
            :size="8"
          >
            <div
              v-for="row in aliasRows"
              :key="row.id"
              class="alias-row"
            >
              <span class="mono domain-name">{{ row.domain }}</span>
              <NTag v-if="row.disabled" size="small" type="warning">
                {{ t("applications.domain.disabled") }}
              </NTag>
              <NTag
                v-if="certOf(row.domain)"
                size="small"
                :type="certificateStatusTagType(certOf(row.domain)?.status)"
              >
                {{ certificateStatusLabel(certOf(row.domain)?.status) }}
              </NTag>
              <NButton
                v-else
                size="small"
                ghost
                :disabled="row.disabled"
                :loading="aliases.busyDomainId.value === row.id"
                @click="aliases.handleSecureDomain(row)"
              >
                {{ t("applications.domain.secure") }}
              </NButton>
              <span class="domain-actions">
                <NButton
                  size="small"
                  :disabled="row.disabled"
                  :loading="aliases.busyDomainId.value === row.id"
                  @click="aliases.handleSetPrimary(row)"
                >
                  {{ t("applications.domain.setPrimary") }}
                </NButton>
                <NPopconfirm
                  @positive-click="aliases.handleRemoveDomain(row)"
                >
                  <template #trigger>
                    <NButton
                      size="small"
                      type="error"
                      ghost
                      :loading="aliases.busyDomainId.value === row.id"
                    >
                      {{ t("applications.domain.remove") }}
                    </NButton>
                  </template>
                  {{ t("applications.domain.removeConfirm", { domain: row.domain }) }}
                </NPopconfirm>
              </span>
              <NAlert
                v-if="aliases.rowErrorId.value === row.id && aliases.rowError.value"
                type="error"
                :show-icon="true"
                class="row-error"
              >
                {{ aliases.rowError.value }}
              </NAlert>
            </div>
          </NSpace>
          <NText v-else depth="3" class="small">
            {{ t("applications.domain.aliasEmpty") }}
          </NText>
        </NSpin>
        <NText depth="3" class="small">
          {{ t("applications.domain.aliasNote") }}
        </NText>
      </NSpace>
    </NCard>
  </NSpace>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}

.alias-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 12px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.domain-name {
  min-width: 0;
  overflow-wrap: anywhere;
}

.domain-actions {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-left: auto;
}

.row-error {
  flex-basis: 100%;
}
</style>
