import type { FormRules } from "naive-ui";
import { z } from "zod";

import { ruleFrom } from "@/shared/validation/naiveAdapter";
import { parseWith } from "@/shared/validation/parse";

/**
 * Project and environment schemas (PE-4, Linear JUS-33). Response schemas
 * mirror the API contract in docs/plans/13-projects-environments.md section
 * 6 exactly; form schemas carry the contract's validation (names 1-64
 * chars). Forms render through `NForm` with the `*Rules` builders below
 * (one `ruleFrom` schema rule per field, so client errors show inline) plus
 * the `is*Valid` helpers for the submit disabled state; responses go
 * through `parseWith` at the axios boundary.
 */

/** resourceCountsSchema mirrors ResourceCounts in the contract. */
export const resourceCountsSchema = z.object({
  applications: z.number().int().nonnegative(),
  services: z.number().int().nonnegative(),
  databases: z.number().int().nonnegative(),
});

/** projectSchema mirrors Project in the contract. */
export const projectSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  description: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
  environment_count: z.number().int().nonnegative(),
  resource_counts: resourceCountsSchema,
});

/** environmentSchema mirrors Environment in the contract. */
export const environmentSchema = z.object({
  id: z.string().uuid(),
  project_id: z.string().uuid(),
  name: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
  resource_counts: resourceCountsSchema,
});

/** projectListEnvelopeSchema mirrors GET /projects. */
export const projectListEnvelopeSchema = z.object({
  projects: z.array(projectSchema),
});

/** projectEnvelopeSchema mirrors the project write routes (`{project}`). */
export const projectEnvelopeSchema = z.object({
  project: projectSchema,
});

/** projectDetailEnvelopeSchema mirrors GET /projects/{id}. */
export const projectDetailEnvelopeSchema = z.object({
  project: projectSchema,
  environments: z.array(environmentSchema),
});

/** createdProjectEnvelopeSchema mirrors POST /projects (201). */
export const createdProjectEnvelopeSchema = z.object({
  project: projectSchema,
  environments: z.array(environmentSchema),
});

/** environmentEnvelopeSchema mirrors the environment write routes. */
export const environmentEnvelopeSchema = z.object({
  environment: environmentSchema,
});
/** projectNameSchema gates the create/rename name: 1-64 chars after trim. */
export const projectNameSchema: z.ZodString = z
  .string()
  .trim()
  .min(1, "projects.validation.nameRequired")
  .max(64, "projects.validation.nameMaxLength");

/** environmentNameSchema gates the environment name: 1-64 chars after trim. */
export const environmentNameSchema: z.ZodString = z
  .string()
  .trim()
  .min(1, "projects.validation.nameRequired")
  .max(64, "projects.validation.nameMaxLength");

/** projectDescriptionSchema gates the optional description (max 500). */
export const projectDescriptionSchema: z.ZodString = z
  .string()
  .trim()
  .max(500, "projects.validation.descriptionMaxLength");

/** isProjectNameValid is the single source for the project submit gating. */
export function isProjectNameValid(value: unknown): boolean {
  return projectNameSchema.safeParse(value).success;
}

/** isEnvironmentNameValid is the single source for the environment gating. */
export function isEnvironmentNameValid(value: unknown): boolean {
  return environmentNameSchema.safeParse(value).success;
}

/** isProjectDescriptionValid gates the optional description (max 500). */
export function isProjectDescriptionValid(value: unknown): boolean {
  return projectDescriptionSchema.safeParse(value).success;
}

/**
 * projectNameRules builds the NForm rules for a lone project name field
 * (rename dialog): one schema-backed rule, so the message renders inline.
 */
export function projectNameRules(): FormRules {
  return {
    name: [
      { ...ruleFrom(projectNameSchema, { required: true }), trigger: ["input", "blur"] },
    ],
  };
}

/**
 * projectCreateRules builds the NForm rules for the create dialog
 * (name required, description optional).
 */
export function projectCreateRules(): FormRules {
  return {
    name: [
      { ...ruleFrom(projectNameSchema, { required: true }), trigger: ["input", "blur"] },
    ],
    description: [{ ...ruleFrom(projectDescriptionSchema), trigger: ["input", "blur"] }],
  };
}

/**
 * environmentNameRules builds the NForm rules for the environment
 * create/rename dialogs.
 */
export function environmentNameRules(): FormRules {
  return {
    name: [
      { ...ruleFrom(environmentNameSchema, { required: true }), trigger: ["input", "blur"] },
    ],
  };
}

/**
 * filterProjects matches the list search: case-insensitive substring on the
 * name (and description when present). An empty query matches everything.
 */
export function filterProjects<T extends { name: string; description: string }>(
  projects: T[],
  query: string,
): T[] {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return projects;
  }
  return projects.filter(
    (project) =>
      project.name.toLowerCase().includes(needle) ||
      project.description.toLowerCase().includes(needle),
  );
}

/**
 * parseProjectList validates a GET /projects payload. Warn-only like every
 * other envelope: server/client skew never breaks a render.
 */
export function parseProjectList(
  data: unknown,
): z.infer<typeof projectListEnvelopeSchema> {
  return parseWith(projectListEnvelopeSchema, data, {
    context: "ProjectListEnvelope",
  });
}

/**
 * parseProjectDetail validates a GET /projects/{id} payload (warn-only).
 */
export function parseProjectDetail(
  data: unknown,
): z.infer<typeof projectDetailEnvelopeSchema> {
  return parseWith(projectDetailEnvelopeSchema, data, {
    context: "ProjectDetailEnvelope",
  });
}

/**
 * parseCreatedProject validates a POST /projects payload (warn-only).
 */
export function parseCreatedProject(
  data: unknown,
): z.infer<typeof createdProjectEnvelopeSchema> {
  return parseWith(createdProjectEnvelopeSchema, data, {
    context: "CreatedProjectEnvelope",
  });
}

/**
 * parseProject validates a PATCH /projects/{id} payload (warn-only).
 */
export function parseProject(
  data: unknown,
): z.infer<typeof projectEnvelopeSchema> {
  return parseWith(projectEnvelopeSchema, data, {
    context: "ProjectEnvelope",
  });
}

/**
 * parseEnvironment validates an environment write payload (warn-only).
 */
export function parseEnvironment(
  data: unknown,
): z.infer<typeof environmentEnvelopeSchema> {
  return parseWith(environmentEnvelopeSchema, data, {
    context: "EnvironmentEnvelope",
  });
}

/**
 * environmentResourceApplicationSchema mirrors one application of
 * GET /environments/{id}/resources: the deploy list item plus the grouping
 * fields and the node name (see environmentResourceApplication in
 * internal/projects/routes.go).
 */
export const environmentResourceApplicationSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  environment_id: z.string().uuid(),
  environment_name: z.string(),
  project_id: z.string().uuid(),
  project_name: z.string(),
  provider: z.string(),
  repo: z.string(),
  clone_url: z.string(),
  branch: z.string(),
  build_pack: z.string(),
  base_domain: z.string(),
  base_domain_disabled: z.boolean(),
  port: z.number().int(),
  host_port: z.number().int(),
  server_id: z.string(),
  server_name: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
  is_preview: z.boolean(),
  preview_of: z.string().optional(),
});

/** serviceDomainRouteSchema mirrors one ServiceDomainRoute of a service. */
export const serviceDomainRouteSchema = z.object({
  service: z.string(),
  domain: z.string(),
  port: z.number().int(),
});

/**
 * environmentResourceServiceSchema mirrors one service of
 * GET /environments/{id}/resources: the services response shape
 * (see ServiceResponse in internal/services/response.go), so the
 * environment page renders service cards unchanged.
 */
export const environmentResourceServiceSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  status: z.string(),
  server_id: z.string(),
  server_name: z.string(),
  environment_id: z.string().uuid(),
  environment_name: z.string(),
  project_id: z.string().uuid(),
  project_name: z.string(),
  compose_project: z.string(),
  compose_yaml: z.string().optional(),
  env: z.record(z.string()),
  domains: z.array(serviceDomainRouteSchema),
  created_at: z.string(),
  updated_at: z.string(),
});

/**
 * environmentResourceDatabaseSchema mirrors one database of
 * GET /environments/{id}/resources: the databases list item plus the
 * grouping fields and the node name (see environmentResourceDatabase in
 * internal/projects/routes.go).
 */
export const environmentResourceDatabaseSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  environment_id: z.string().uuid(),
  environment_name: z.string(),
  project_id: z.string().uuid(),
  project_name: z.string(),
  engine: z.string(),
  version: z.string().optional(),
  status: z.string(),
  server_id: z.string(),
  server_name: z.string(),
  public_port: z.number().int(),
  volume: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
});

/** environmentResourcesEnvelopeSchema mirrors GET /environments/{id}/resources. */
export const environmentResourcesEnvelopeSchema = z.object({
  environment: environmentSchema,
  project: projectSchema,
  applications: z.array(environmentResourceApplicationSchema),
  services: z.array(environmentResourceServiceSchema),
  databases: z.array(environmentResourceDatabaseSchema),
});

/**
 * parseEnvironmentResources validates a GET /environments/{id}/resources
 * payload (warn-only).
 */
export function parseEnvironmentResources(
  data: unknown,
): z.infer<typeof environmentResourcesEnvelopeSchema> {
  return parseWith(environmentResourcesEnvelopeSchema, data, {
    context: "EnvironmentResourcesEnvelope",
  });
}
