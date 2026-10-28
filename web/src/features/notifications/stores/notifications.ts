import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createChannel,
  deleteChannel,
  describeChannelError,
  isFeatureDisabled,
  listChannels,
  listDeliveries,
  listDeliveryFailures,
  retryDelivery as retryDeliveryRequest,
  testChannel,
  updateChannel,
} from "@/features/notifications/api/notifications";
import type {
  ChannelInput,
  DeliveryFailure,
  NotificationChannel,
  NotificationDelivery,
  TestResult,
} from "@/features/notifications/api/notifications";

export const useNotificationsStore = defineStore("notifications", () => {
  const channels = ref<NotificationChannel[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const errorRaw = ref<unknown>(null);
  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeChannelError(errorRaw.value),
  );
  const featureDisabled = ref(false);

  let listReadToken = 0;

  function clearList(): void {
    channels.value = [];
    loaded.value = false;
    errorRaw.value = null;
    featureDisabled.value = false;
  }

  function applyChannel(updated: NotificationChannel): void {
    const index = channels.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      channels.value = [...channels.value, updated];
      return;
    }
    channels.value[index] = updated;
  }

  async function fetchChannels(): Promise<void> {
    const token = ++listReadToken;
    const isCurrent = (): boolean => token === listReadToken;

    loading.value = true;
    errorRaw.value = null;
    try {
      const next = await listChannels();
      if (!isCurrent()) {
        return;
      }
      channels.value = next;
      featureDisabled.value = false;
      loaded.value = true;
    } catch (err) {
      if (!isCurrent()) {
        return;
      }
      if (isFeatureDisabled(err)) {
        featureDisabled.value = true;
        loaded.value = true;
        channels.value = [];
        return;
      }
      errorRaw.value = err;
      throw err;
    } finally {
      if (isCurrent()) {
        loading.value = false;
      }
    }
  }

  async function create(input: ChannelInput): Promise<NotificationChannel> {
    const channel = await createChannel(input);
    applyChannel(channel);
    return channel;
  }

  async function update(
    id: string,
    input: ChannelInput,
  ): Promise<NotificationChannel> {
    const channel = await updateChannel(id, input);
    applyChannel(channel);
    return channel;
  }

  async function remove(id: string): Promise<void> {
    await deleteChannel(id);
    channels.value = channels.value.filter((item) => item.id !== id);
  }

  function test(id: string): Promise<TestResult> {
    return testChannel(id);
  }

  function history(id: string): Promise<NotificationDelivery[]> {
    return listDeliveries(id);
  }

  function failures(): Promise<DeliveryFailure[]> {
    return listDeliveryFailures();
  }

  async function retryDelivery(id: string): Promise<void> {
    await retryDeliveryRequest(id);
  }

  function reset(): void {
    listReadToken += 1;
    clearList();
    loading.value = false;
  }

  return {
    channels,
    loading,
    loaded,
    error,
    featureDisabled,
    fetchChannels,
    create,
    update,
    remove,
    test,
    history,
    failures,
    retryDelivery,
    reset,
  };
});
