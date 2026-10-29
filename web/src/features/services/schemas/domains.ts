import { z } from "zod";

import { extractComposeServicePorts } from "@/shared/utils/compose";

const HOST_PATTERN = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/;
const COMPOSE_SERVICE_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_.-]*$/;

export const serviceDomainHostSchema = z
  .string()
  .min(1, "services.validation.domainHost")
  .max(253, "services.validation.domainHost")
  .regex(HOST_PATTERN, "services.validation.domainHost");

export const serviceDomainPortSchema = z
  .number({ error: "services.validation.domainPort" })
  .int("services.validation.domainPort")
  .min(1, "services.validation.domainPort")
  .max(65535, "services.validation.domainPort");

export const serviceDomainTargetSchema = z
  .string()
  .trim()
  .min(1, "services.validation.domainTarget")
  .regex(COMPOSE_SERVICE_PATTERN, "services.validation.domainTarget");

export const serviceDomainProtocolSchema = z.enum(["http", "https"]);

export interface DomainTarget {
  service: string;
  port: number;
}

/** Compose services with the container port the document publishes or exposes. */
export function serviceDomainTargets(composeYaml: string): DomainTarget[] {
  return Object.entries(extractComposeServicePorts(composeYaml)).map(
    ([service, port]) => ({ service, port }),
  );
}

/**
 * Pre-fill the target when there is exactly one routable container port, so
 * the common case only needs a host.
 */
export function defaultDomainTarget(composeYaml: string): DomainTarget | null {
  const routable = serviceDomainTargets(composeYaml).filter(
    (target) => target.port > 0,
  );
  return routable.length === 1 ? routable[0] : null;
}
