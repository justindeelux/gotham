import { defineStore } from "pinia";
import { ref } from "vue";

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
} from "../api/services";
import type {
  CreateServiceInput,
  DeployOutcome,
  Service,
  ServiceDeploy,
  UpdateServiceInput,
} from "../api/services";

/**
 * Compose services: the list plus a per-service deploy history cache.
 *
 * Containers are deliberately not cached here: reading them dials the node
 * agent (502 without one), so the detail page loads them on demand.
 */
export const useServicesStore = defineStore("services", () => {
  const services = ref<Service[]>([]);
  const deploys = ref<Record<string, ServiceDeploy[]>>({});
  const loading = ref(false);
  const error = ref<string | null>(null);
  const deploysError = ref<string | null>(null);

  /** applyService merges one service into the in-memory list in place. */
  function applyService(updated: Service): void {
    const index = services.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      services.value = [updated, ...services.value];
      return;
    }
    services.value[index] = { ...services.value[index], ...updated };
  }

  /** fetchServices loads the caller's services, newest first. */
  async function fetchServices(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      services.value = await listServices();
    } catch (err) {
      error.value = describeServiceError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** serviceOf returns one service from the cache, if loaded. */
  function serviceOf(id: string): Service | null {
    return services.value.find((item) => item.id === id) ?? null;
  }

  /** fetchService loads one service (with its stored document) and caches it. */
  async function fetchService(id: string): Promise<Service> {
    const service = await getService(id);
    applyService(service);
    return service;
  }

  /** deploysOf returns the cached deploy history of one service. */
  function deploysOf(id: string): ServiceDeploy[] {
    return deploys.value[id] ?? [];
  }

  /** latestDeployOf returns the newest deploy attempt, if any. */
  function latestDeployOf(id: string): ServiceDeploy | null {
    return deploysOf(id)[0] ?? null;
  }

  /** fetchDeploys refreshes one service's deploy history. */
  async function fetchDeploys(id: string): Promise<void> {
    deploysError.value = null;
    try {
      deploys.value = { ...deploys.value, [id]: await listServiceDeploys(id) };
    } catch (err) {
      deploysError.value = describeServiceError(err);
      throw err;
    }
  }

  /** fetchAllDeploys refreshes the history of every listed service. */
  async function fetchAllDeploys(): Promise<void> {
    await Promise.allSettled(
      services.value.map((service) => fetchDeploys(service.id)),
    );
  }

  /** create stores a service and prepends it to the list. */
  async function create(input: CreateServiceInput): Promise<Service> {
    const created = await createService(input);
    applyService(created);
    return created;
  }

  /** update patches the mutable fields and refreshes the cached row. */
  async function update(
    id: string,
    input: UpdateServiceInput,
  ): Promise<Service> {
    const updated = await updateService(id, input);
    applyService(updated);
    return updated;
  }

  /** remove soft-deletes a service and drops it from the list. */
  async function remove(id: string): Promise<void> {
    await deleteService(id);
    services.value = services.value.filter((item) => item.id !== id);
    const remaining = { ...deploys.value };
    delete remaining[id];
    deploys.value = remaining;
  }

  /** deploy starts the project and records the attempt. */
  async function deploy(id: string): Promise<DeployOutcome> {
    const outcome = await deployService(id);
    applyService(outcome.service);
    await fetchDeploys(id).catch(() => undefined);
    return outcome;
  }

  /** stop takes the project down (named volumes stay). */
  async function stop(id: string): Promise<Service> {
    const updated = await stopService(id);
    applyService(updated);
    return updated;
  }

  /** restart restarts a running project in place, or starts a stopped one. */
  async function restart(id: string): Promise<Service> {
    const updated = await restartService(id);
    applyService(updated);
    return updated;
  }

  return {
    services,
    deploys,
    loading,
    error,
    deploysError,
    fetchServices,
    serviceOf,
    fetchService,
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
