<script setup lang="ts">
import { NRadio, NRadioGroup, NSpace, NText } from "naive-ui";
import { useI18n } from "vue-i18n";

import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";

const wizard = useCreateWizardState();
const { form } = wizard;

const { t } = useI18n();
</script>

<template>
  <NSpace vertical :size="12">
    <NText strong>{{ t("applications.wizard.buildPackTitle") }}</NText>
    <template v-if="wizard.isComposePaste.value">
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
      <NRadioGroup v-model:value="form.buildPack">
        <NSpace vertical :size="8">
          <NRadio v-for="pack in wizard.buildPacks.value" :key="pack.value" :value="pack.value">
            <NText strong>{{ pack.label }}</NText>
            <br />
            <NText depth="3">{{ pack.hint }}</NText>
          </NRadio>
        </NSpace>
      </NRadioGroup>
    </template>
  </NSpace>
</template>
