import type { FormRules } from "naive-ui";
import { z } from "zod";

import { ruleFrom } from "@/shared/validation/naiveAdapter";
import { parseWith } from "@/shared/validation/parse";

export const resourceCountsSchema = z.object({
  applications: z.number().int().nonnegative(),
  services: z.number().int().nonnegative(),
  databases: z.number().int().nonnegative(),
});

export const projectSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  description: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
  environment_count: z.number().int().nonnegative(),
  resource_counts: resourceCountsSchema,
});

export const environmentSchema = z.object({
  id: z.string().uuid(),
  project_id: z.string().uuid(),
  name: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
  resource_counts: resourceCountsSchema,
});

export const projectListEnvelopeSchema = z.object({
  projects: z.array(projectSchema),
});

export const projectEnvelopeSchema = z.object({
  project: projectSchema,
});

export const projectDetailEnvelopeSchema = z.object({
  project: projectSchema,
  environments: z.array(environmentSchema),
});

export const createdProjectEnvelopeSchema = z.object({
  project: projectSchema,
  environments: z.array(environmentSchema),
});

export const environmentEnvelopeSchema = z.object({
  environment: environmentSchema,
});
export const projectNameSchema: z.ZodString = z
  .string()
  .trim()
  .min(1, "projects.validation.nameRequired")
  .max(64, "projects.validation.nameMaxLength");

export const environmentNameSchema: z.ZodString = z
  .string()
  .trim()
  .min(1, "projects.validation.nameRequired")
  .max(64, "projects.validation.nameMaxLength");

export const projectDescriptionSchema: z.ZodString = z
  .string()
  .trim()
  .max(500, "projects.validation.descriptionMaxLength");

export function isProjectNameValid(value: unknown): boolean {
  return projectNameSchema.safeParse(value).success;
}

export function isEnvironmentNameValid(value: unknown): boolean {
  return environmentNameSchema.safeParse(value).success;
}

export function isProjectDescriptionValid(value: unknown): boolean {
  return projectDescriptionSchema.safeParse(value).success;
}

export function projectNameRules(): FormRules {
  return {
    name: [
      { ...ruleFrom(projectNameSchema, { required: true }), trigger: ["input", "blur"] },
    ],
  };
}

export function projectCreateRules(): FormRules {
  return {
    name: [
      { ...ruleFrom(projectNameSchema, { required: true }), trigger: ["input", "blur"] },
    ],
    description: [{ ...ruleFrom(projectDescriptionSchema), trigger: ["input", "blur"] }],
  };
}

export function environmentNameRules(): FormRules {
  return {
    name: [
      { ...ruleFrom(environmentNameSchema, { required: true }), trigger: ["input", "blur"] },
    ],
  };
}

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

export function parseProjectList(
  data: unknown,
): z.infer<typeof projectListEnvelopeSchema> {
  return parseWith(projectListEnvelopeSchema, data, {
    context: "ProjectListEnvelope",
  });
}

export function parseProjectDetail(
  data: unknown,
): z.infer<typeof projectDetailEnvelopeSchema> {
  return parseWith(projectDetailEnvelopeSchema, data, {
    context: "ProjectDetailEnvelope",
  });
}

export function parseCreatedProject(
  data: unknown,
): z.infer<typeof createdProjectEnvelopeSchema> {
  return parseWith(createdProjectEnvelopeSchema, data, {
    context: "CreatedProjectEnvelope",
  });
}

export function parseProject(
  data: unknown,
): z.infer<typeof projectEnvelopeSchema> {
  return parseWith(projectEnvelopeSchema, data, {
    context: "ProjectEnvelope",
  });
}

export function parseEnvironment(
  data: unknown,
): z.infer<typeof environmentEnvelopeSchema> {
  return parseWith(environmentEnvelopeSchema, data, {
    context: "EnvironmentEnvelope",
  });
}

export const runtimeReplicaHealthSchema = z.object({
  service: z.string().optional(),
  replica: z.string().optional(),
  container_id: z.string().optional(),
  deployment_id: z.string().optional(),
  state: z.string(),
  health: z.string(),
  reason: z.string().optional(),
  coverage: z.string(),
  observed_at: z.string().optional(),
  last_restart_at: z.string().optional(),
  last_oom_at: z.string().optional(),
});

export const runtimeHealthSchema = z
  .object({
    state: z.string(),
    coverage: z.string(),
    reason: z.string().optional(),
    observed_at: z.string().optional(),
    enabled: z.boolean(),
    grace_seconds: z.number().int(),
    incident_id: z.string().optional(),
    affected: z.array(runtimeReplicaHealthSchema).default([]),
  })
  .nullish();

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
  source_type: z.string().optional(),
  branch: z.string(),
  build_pack: z.string(),
  primary_domain: z.string(),
  server_id: z.string(),
  server_name: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
  is_preview: z.boolean(),
  preview_of: z.string().optional(),
  runtime_health: runtimeHealthSchema,
});

export const serviceDomainRouteSchema = z.object({
  service: z.string(),
  domain: z.string(),
  port: z.number().int(),
});

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
  env: z.record(z.string(), z.string()),
  domains: z.array(serviceDomainRouteSchema),
  created_at: z.string(),
  updated_at: z.string(),
});

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
  runtime_health: runtimeHealthSchema,
});

export const environmentResourcesEnvelopeSchema = z.object({
  environment: environmentSchema,
  project: projectSchema,
  applications: z.array(environmentResourceApplicationSchema),
  services: z.array(environmentResourceServiceSchema),
  databases: z.array(environmentResourceDatabaseSchema),
});

export function parseEnvironmentResources(
  data: unknown,
): z.infer<typeof environmentResourcesEnvelopeSchema> {
  return parseWith(environmentResourcesEnvelopeSchema, data, {
    context: "EnvironmentResourcesEnvelope",
  });
}
