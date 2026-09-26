<script setup lang="ts">
import { NCard, NEmpty, NSpace, NText } from "naive-ui";
</script>

<template>
  <NSpace vertical :size="16">
    <NCard title="Dashboard">
      <NEmpty description="No data yet">
        <template #extra>
          <NText depth="3">
            Your applications, databases, and services will appear here once you
            create them.
          </NText>
        </template>
      </NEmpty>
    </NCard>
  </NSpace>
</template>
