<script setup lang="ts">
import { NRadio, NRadioGroup, NSpace, NText } from "naive-ui";

import { useCreateWizardState } from "@/features/applications/composables/useCreateAppWizard";

const wizard = useCreateWizardState();
const { form } = wizard;
</script>

<template>
  <NSpace vertical :size="12">
    <NText strong>Build pack</NText>
    <NText depth="3">
      Detection runs server-side at build time from the repo layout —
      the choice below is a hint, never a guess.
    </NText>
    <NRadioGroup v-model:value="form.buildPack">
      <NSpace vertical :size="8">
        <NRadio v-for="pack in wizard.buildPacks" :key="pack.label" :value="pack.value">
          <NText strong>{{ pack.label }}</NText>
          <br />
          <NText depth="3">{{ pack.hint }}</NText>
        </NRadio>
      </NSpace>
    </NRadioGroup>
  </NSpace>
</template>
