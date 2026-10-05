<script setup lang="ts">
import { NCard, NEmpty } from "naive-ui";
import { RouterLink } from "vue-router";
</script>

<template>
  <NCard style="margin-top: 16px" title="Router list — backend pending">
    <NEmpty description="The generated Traefik routers are not exposed by the API yet.">
      <template #extra>
        <p class="empty-hint">
          The control plane generates the file-provider configuration from
          application state (BE-6.1), but there is no read endpoint for the
          resulting routers. A live router table arrives in a later backend
          package; nothing is shown here rather than invented.
        </p>
        <RouterLink :to="{ name: 'projects' }">
          Manage application domains
        </RouterLink>
      </template>
    </NEmpty>
  </NCard>
</template>

<style scoped>
.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
  max-width: 60ch;
}
</style>
