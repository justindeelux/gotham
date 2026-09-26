<script setup lang="ts">
import { NConfigProvider, NMessageProvider } from "naive-ui";

import AppLayout from "./layouts/AppLayout.vue";
</script>

<template>
  <NConfigProvider>
    <NMessageProvider>
      <AppLayout />
    </NMessageProvider>
  </NConfigProvider>
</template>
