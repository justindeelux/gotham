import { defineStore } from "pinia";
import { computed, ref } from "vue";
import type { Ref } from "vue";

import { activeLocale } from "@/shared/i18n";

import {
  createService,
  deleteService,
  deployService,
  describeServiceError,
  getService,
  listServiceDeploys,
  listServices,
  restartService,
  stopService,
  updateService,
} from "@/features/services/api/services";
import type {
  CreateServiceInput,
  DeployOutcome,
  Service,
  ServiceDeploy,
  UpdateServiceInput,
} from "@/features/services/api/services";

export interface ServiceHistory {
  deploys: ServiceDeploy[];
  loading: boolean;
  loaded: boolean;
  error: string | null;
  failure: unknown;
}

export const useServicesStore = defineStore("services", () => {
  const services = ref<Service[]>([]);
  const histories = ref<Record<string, ServiceHistory>>({});
  const loading = ref(false);
  const listFailure: Ref<unknown> = ref(null);
  const error = computed<string | null>(() => {
    if (listFailure.value === null) {
      return null;
    }
    void activeLocale.value;
    return describeServiceError(listFailure.value);
  });

  let historyReadToken = 0;
  const historyReadTokens = new Map<string, number>();

  function reset(): void {
    historyReadToken += 1;
    historyReadTokens.clear();
    services.value = [];
    histories.value = {};
    loading.value = false;
    listFailure.value = null;
  }

  function applyService(updated: Service): void {
    const index = services.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      services.value = [updated, ...services.value];
      return;
    }
    services.value[index] = { ...services.value[index], ...updated };
  }

  async function fetchServices(): Promise<void> {
    loading.value = true;
    listFailure.value = null;
    try {
      services.value = await listServices();
    } catch (err) {
      listFailure.value = err;
      throw err;
    } finally {
      loading.value = false;
    }
  }

  function serviceOf(id: string): Service | null {
    return services.value.find((item) => item.id === id) ?? null;
  }

  async function fetchService(id: string): Promise<Service> {
    const service = await getService(id);
    applyService(service);
    return service;
  }

  function historyOf(id: string): ServiceHistory | null {
    return histories.value[id] ?? null;
  }

  function deploysOf(id: string): ServiceDeploy[] {
    return histories.value[id]?.deploys ?? [];
  }

  function latestDeployOf(id: string): ServiceDeploy | null {
    return deploysOf(id)[0] ?? null;
  }

  async function fetchDeploys(id: string): Promise<void> {
    const token = ++historyReadToken;
    historyReadTokens.set(id, token);
    const isLatest = (): boolean => historyReadTokens.get(id) === token;

    const previous = histories.value[id];
    histories.value = {
      ...histories.value,
      [id]: {
        deploys: previous?.deploys ?? [],
        loading: true,
        loaded: previous?.loaded ?? false,
        error: null,
        failure: null,
      },
    };
    try {
      const deploys = await listServiceDeploys(id);
      if (!isLatest()) {
        return;
      }
      histories.value = {
        ...histories.value,
        [id]: { deploys, loading: false, loaded: true, error: null, failure: null },
      };
    } catch (err) {
      if (!isLatest()) {
        throw err;
      }
      const latest = histories.value[id] ?? previous;
      histories.value = {
        ...histories.value,
        [id]: {
          deploys: latest?.deploys ?? [],
          loading: false,
          loaded: latest?.loaded ?? false,
          error: describeServiceError(err),
          failure: err,
        },
      };
      throw err;
    }
  }

  async function fetchAllDeploys(): Promise<void> {
    await Promise.allSettled(
      services.value.map((service) => fetchDeploys(service.id)),
    );
  }

  async function create(input: CreateServiceInput): Promise<Service> {
    const created = await createService(input);
    applyService(created);
    return created;
  }

  async function update(
    id: string,
    input: UpdateServiceInput,
  ): Promise<Service> {
    const updated = await updateService(id, input);
    applyService(updated);
    return updated;
  }

  async function remove(id: string): Promise<string[]> {
    const warnings = await deleteService(id);
    services.value = services.value.filter((item) => item.id !== id);
    const remaining = { ...histories.value };
    delete remaining[id];
    histories.value = remaining;
    return warnings;
  }

  async function deploy(id: string): Promise<DeployOutcome> {
    const outcome = await deployService(id);
    applyService(outcome.service);
    await fetchDeploys(id).catch(() => undefined);
    return outcome;
  }

  async function stop(id: string): Promise<Service> {
    const updated = await stopService(id);
    applyService(updated);
    return updated;
  }

  async function restart(id: string): Promise<Service> {
    const updated = await restartService(id);
    applyService(updated);
    return updated;
  }

  return {
    services,
    histories,
    loading,
    error,
    reset,
    fetchServices,
    serviceOf,
    fetchService,
    historyOf,
    deploysOf,
    latestDeployOf,
    fetchDeploys,
    fetchAllDeploys,
    create,
    update,
    remove,
    deploy,
    stop,
    restart,
  };
});
