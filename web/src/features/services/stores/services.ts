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
} from "@/features/services/api/services";
import type {
  CreateServiceInput,
  DeployOutcome,
  Service,
  ServiceDeploy,
  UpdateServiceInput,
} from "@/features/services/api/services";

/** Per-service deploy history: cached rows plus load and failure state. */
export interface ServiceHistory {
  deploys: ServiceDeploy[];
  loading: boolean;
  /** True only after a successful read; failures never count as loaded. */
  loaded: boolean;
  /** Redacted failure message of the last read; null when it succeeded. */
  error: string | null;
}

/**
 * Compose services: the list plus a per-service deploy history cache.
 *
 * History state is tracked per service so an unavailable read can never be
 * rendered as a confirmed-empty history: `loaded` is only set by a successful
 * response, a failure keeps the previously cached rows and records `error`,
 * and a service whose history was never read is distinguishable from one
 * whose history is genuinely empty.
 *
 * Containers are deliberately not cached here: reading them dials the node
 * agent (502 without one), so the detail page loads them on demand.
 */
export const useServicesStore = defineStore("services", () => {
  const services = ref<Service[]>([]);
  const histories = ref<Record<string, ServiceHistory>>({});
  const loading = ref(false);
  const error = ref<string | null>(null);

  /** Monotonic token of the newest history read per service. */
  let historyReadToken = 0;
  const historyReadTokens = new Map<string, number>();

  /**
   * reset drops the cached services and histories and invalidates in-flight
   * history reads, so the next sign-in never sees the previous account's
   * data. Called on sign-out (see the auth store).
   */
  function reset(): void {
    historyReadToken += 1;
    historyReadTokens.clear();
    services.value = [];
    histories.value = {};
    loading.value = false;
    error.value = null;
  }

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

  /**
   * historyOf returns one service's history state, or null when it was never
   * read. Callers must not render an empty result unless `loaded` is true.
   */
  function historyOf(id: string): ServiceHistory | null {
    return histories.value[id] ?? null;
  }

  /** deploysOf returns the cached deploy rows of one service. */
  function deploysOf(id: string): ServiceDeploy[] {
    return histories.value[id]?.deploys ?? [];
  }

  /** latestDeployOf returns the newest deploy attempt, if any. */
  function latestDeployOf(id: string): ServiceDeploy | null {
    return deploysOf(id)[0] ?? null;
  }

  /**
   * fetchDeploys refreshes one service's deploy history. A failure keeps the
   * previously cached rows (flagged stale through `error`) and rethrows for
   * the caller's own UI state.
   *
   * Concurrent reads for the same service are ordered by a per-service token:
   * only the newest read writes state, so a late failure cannot mark a history
   * unavailable that a newer read already loaded (and a late success cannot
   * overwrite a newer result).
   */
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
      },
    };
    try {
      const deploys = await listServiceDeploys(id);
      if (!isLatest()) {
        return;
      }
      histories.value = {
        ...histories.value,
        [id]: { deploys, loading: false, loaded: true, error: null },
      };
    } catch (err) {
      if (!isLatest()) {
        // An obsolete failure must not touch the newer read's state.
        throw err;
      }
      // Re-read at completion time: a newer call may already have replaced the
      // entry this read started from.
      const latest = histories.value[id] ?? previous;
      histories.value = {
        ...histories.value,
        [id]: {
          deploys: latest?.deploys ?? [],
          loading: false,
          loaded: latest?.loaded ?? false,
          error: describeServiceError(err),
        },
      };
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
    const remaining = { ...histories.value };
    delete remaining[id];
    histories.value = remaining;
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
