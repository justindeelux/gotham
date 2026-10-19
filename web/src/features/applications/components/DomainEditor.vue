<script setup lang="ts">
import { NSpace } from "naive-ui";

import type { Application } from "@/features/applications/api/applications";
import ApplicationDomainPanel from "@/features/applications/components/ApplicationDomainPanel.vue";
import { provideDomainsRefresh } from "@/features/applications/composables/domainRefresh";

/**
 * Domain and certificate editor of one application (the Domains tab of the
 * application detail page). The base domain is written through the
 * applications API; each row's certificate configuration through the proxy
 * SSL API via its own modal (JUS-236).
 */

interface Props {
  application: Application;
}

const props = defineProps<Props>();

/**
 * domainsTick is bumped by the alias panel after every mutation so the
 * certificate panel reloads its attached-domain list without a page reload.
 */
provideDomainsRefresh();
</script>

<template>
  <NSpace vertical :size="16">
    <ApplicationDomainPanel :application="props.application" />
  </NSpace>
</template>
