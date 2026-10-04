<script setup lang="ts">
import { NSpace } from "naive-ui";

import type { Application } from "@/features/applications/api/applications";
import ApplicationDomainPanel from "@/features/applications/components/ApplicationDomainPanel.vue";
import CertificatePanel from "@/features/applications/components/CertificatePanel.vue";

/**
 * Domain and certificate editor of one application (the Domains tab of the
 * application detail page). The base domain is written through the
 * applications API; the certificate configuration through the proxy SSL API.
 * The certificate's recorded domain always comes from the application's
 * current base_domain, so a domain change is flagged until the configuration
 * is saved again.
 */

interface Props {
  application: Application;
}

const props = defineProps<Props>();
</script>

<template>
  <NSpace vertical :size="16">
    <ApplicationDomainPanel :application="props.application" />
    <CertificatePanel :application="props.application" />
  </NSpace>
</template>
