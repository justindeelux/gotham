<script setup lang="ts">
import { useEventListener } from "@vueuse/core";
import {
  NAlert,
  NButton,
  NFormItem,
  NInput,
  NRadio,
  NRadioGroup,
  NSelect,
  NSpace,
  NText,
  NTooltip,
} from "naive-ui";
import { onMounted, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";

const wizard = useCreateWizardState();
const { form } = wizard;

const { t } = useI18n();

onMounted(() => wizard.subscribeNodeTools());
watch(() => form.serverId, () => wizard.subscribeNodeTools());
useEventListener(window, "focus", () => wizard.subscribeNodeTools());
</script>

<template>
  <NSpace vertical :size="12">
    <NText strong>{{ t("applications.wizard.buildPackTitle") }}</NText>
    <template v-if="wizard.isCompose.value">
      <NText depth="3">
        {{ t("applications.wizard.composeNoBuildPack") }}
      </NText>
    </template>
    <template v-else-if="wizard.isDockerfile.value">
      <NText depth="3">
        {{ t("applications.wizard.buildPackDockerfileNote") }}
      </NText>
    </template>
    <template v-else>
      <NText depth="3">
        {{ t("applications.wizard.buildPackIntro") }}
      </NText>
      <NText v-if="wizard.scan.loading" depth="3" class="app-scan-state">
        {{ t("applications.wizard.scanChecking") }}
      </NText>
      <NAlert v-else-if="wizard.scan.failed" type="warning" :show-icon="true">
        {{ t("applications.wizard.scanFailed") }}
      </NAlert>
      <NText v-else-if="wizard.scan.markerFile !== ''" depth="3" class="app-scan-state">
        {{ t("applications.wizard.scanFound", { file: wizard.scan.markerFile }) }}
      </NText>
      <NText v-else-if="wizard.scan.ran" depth="3" class="app-scan-state">
        {{ t("applications.wizard.scanEmpty") }}
      </NText>
      <NAlert v-if="wizard.nodeToolsLoading.value" type="info" :show-icon="true">
        {{ t("applications.wizard.packChecking") }}
      </NAlert>
      <NAlert v-else-if="wizard.nodeToolsError.value" type="warning" :show-icon="true">
        <NSpace align="center" :size="12" wrap>
          <span>{{ t("applications.wizard.packCheckError") }}</span>
          <NButton size="small" @click="() => void wizard.retryNodeTools()">
            {{ t("applications.wizard.packRetry") }}
          </NButton>
        </NSpace>
      </NAlert>
      <NAlert v-if="wizard.noBuildEngine.value" type="warning" :show-icon="true">
        <NSpace vertical :size="8">
          <span>{{ t("applications.wizard.packNoEngine") }}</span>
          <NButton size="small" tertiary @click="wizard.installBuildPack('railpack')">
            {{ t("applications.wizard.packInstallRailpack") }}
          </NButton>
        </NSpace>
      </NAlert>
      <NRadioGroup v-model:value="form.buildPack" @update:value="wizard.markBuildPackTouched">
        <NSpace vertical :size="8">
          <div
            v-for="pack in wizard.buildPackChoices.value"
            :key="pack.value"
            class="pack-row"
          >
            <NRadio :value="pack.value" :disabled="!pack.selectable">
              <NText strong>{{ pack.label }}</NText>
              <br />
              <NText depth="3">{{ pack.hint }}</NText>
            </NRadio>
            <NTooltip v-if="pack.missing" trigger="hover" placement="top">
              <template #trigger>
                <span class="pack-reason" role="note" tabindex="0">
                  {{
                    pack.installing
                      ? t("applications.wizard.packInstalling", { tool: pack.missing })
                      : t("applications.wizard.packMissing", { tool: pack.missing })
                  }}
                </span>
              </template>
              {{ t("applications.wizard.packTooltip") }}
            </NTooltip>
            <NButton
              v-if="pack.missing"
              size="tiny"
              tertiary
              :loading="pack.installing"
              :disabled="pack.installing"
              :aria-label="t('applications.wizard.packInstallNamed', { pack: pack.label })"
              @click="wizard.installBuildPack(pack.value)"
            >
              {{ t("applications.wizard.packInstall") }}
            </NButton>
          </div>
        </NSpace>
      </NRadioGroup>
      <div v-if="wizard.isComposePack.value" class="app-compose-pack-fields">
        <NFormItem :label="t('applications.wizard.composeFile')">
          <NSelect
            v-if="wizard.scanComposeFileOptions.value.length > 0"
            v-model:value="form.composeFile"
            :options="wizard.scanComposeFileOptions.value"
            filterable
            tag
            class="app-compose-pack-mono"
          />
          <NInput
            v-else
            v-model:value="form.composeFile"
            class="app-compose-pack-mono"
            placeholder="docker-compose.yml"
          />
          <span class="app-compose-pack-hint">{{ t("applications.wizard.composeFileHint") }}</span>
        </NFormItem>
        <NFormItem :label="t('applications.wizard.composeService')">
          <NSelect
            v-if="wizard.scannedComposeServiceOptions.value.length > 0"
            v-model:value="form.composeService"
            :options="wizard.scannedComposeServiceOptions.value"
            :placeholder="t('applications.wizard.composeServicePlaceholder')"
            filterable
            tag
            class="app-compose-pack-mono"
          />
          <NInput
            v-else
            v-model:value="form.composeService"
            class="app-compose-pack-mono"
            :placeholder="t('applications.wizard.composeServiceInputPlaceholder')"
          />
          <span class="app-compose-pack-hint">
            {{
              wizard.scannedComposeServiceOptions.value.length > 0
                ? t("applications.wizard.composeServicesScanned", {
                    file: wizard.scan.composeFile,
                  })
                : t("applications.wizard.composeServiceHint")
            }}
          </span>
        </NFormItem>
      </div>
    </template>
  </NSpace>
</template>

<style scoped>
.pack-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.pack-reason {
  font-size: var(--text-xs);
  color: var(--warn);
  border-bottom: 1px dotted var(--warn);
  cursor: help;
}

.app-scan-state {
  font-size: var(--text-sm);
}

.app-compose-pack-fields {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding-left: var(--space-4);
}

.app-compose-pack-mono {
  font-family: var(--font-mono);
}

.app-compose-pack-hint {
  font-size: var(--text-xs);
  color: var(--meta);
}
</style>
