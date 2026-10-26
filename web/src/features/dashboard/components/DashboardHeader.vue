<script setup lang="ts">
import { NButton } from "naive-ui";
import { RouterLink } from "vue-router";
</script>

<template>
  <div class="page-head">
    <div>
      <p class="eyebrow">{{ $t("dashboard.header.eyebrow") }}</p>
      <h1>{{ $t("dashboard.header.title") }}</h1>
      <p class="page-desc">
        {{ $t("dashboard.header.description") }}
      </p>
    </div>
    <div class="page-actions">
      <RouterLink :to="{ name: 'servers' }" custom>
        <template #default="{ navigate }">
          <NButton quaternary @click="navigate">{{ $t("dashboard.header.viewServers") }}</NButton>
        </template>
      </RouterLink>
      <RouterLink :to="{ name: 'servers', query: { add: '1' } }" custom>
        <template #default="{ navigate }">
          <NButton type="primary" @click="navigate">{{ $t("dashboard.header.addServer") }}</NButton>
        </template>
      </RouterLink>
    </div>
  </div>
</template>
