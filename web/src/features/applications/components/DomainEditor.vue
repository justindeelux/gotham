<script setup lang="ts">
import { NSpace } from "naive-ui";

import type { Application } from "@/features/applications/api/applications";
import ApplicationDomainPanel from "@/features/applications/components/ApplicationDomainPanel.vue";
import { provideDomainsRefresh } from "@/features/applications/composables/domainRefresh";

interface Props {
  application: Application;
}

const props = defineProps<Props>();

provideDomainsRefresh();
</script>

<template>
  <NSpace vertical :size="16">
    <ApplicationDomainPanel :application="props.application" />
  </NSpace>
</template>
