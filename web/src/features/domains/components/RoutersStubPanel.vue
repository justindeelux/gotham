<script setup lang="ts">
import { NCard, NEmpty } from "naive-ui";
import { RouterLink } from "vue-router";
</script>

<template>
  <NCard style="margin-top: 16px" :title="$t('domains.routers.title')">
    <NEmpty :description="$t('domains.routers.empty')">
      <template #extra>
        <p class="empty-hint">
          {{ $t("domains.routers.hint") }}
        </p>
        <RouterLink :to="{ name: 'projects' }">
          {{ $t("domains.routers.manageDomains") }}
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
