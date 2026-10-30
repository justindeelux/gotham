import { parseWith } from "@/shared/validation/parse";
import { http } from "@/shared/api/http";

import {
  cleanupExclusionEnvelopeSchema,
  cleanupExclusionListSchema,
  cleanupNextRunSchema,
  cleanupRunDetailSchema,
  cleanupRunListSchema,
  cleanupScheduleEnvelopeSchema,
  cleanupScheduleListSchema,
  type CleanupExclusion,
  type CleanupRun,
  type CleanupRunDetail,
  type CleanupSchedule,
  type ScheduleForm,
} from "../schemas/center";

export interface CleanupRunQuery {
  serverId?: string;
  limit?: number;
  before?: string;
}

export interface ExclusionInput {
  server_id?: string;
  category: string;
  match: string;
  note?: string;
}

export async function listSchedules(): Promise<CleanupSchedule[]> {
  const response = await http.get<unknown>("/cleanup/schedules");
  const parsed = parseWith(cleanupScheduleListSchema, response.data, {
    context: "cleanup schedules",
  });
  return parsed.schedules ?? [];
}

export async function createSchedule(input: ScheduleForm): Promise<CleanupSchedule> {
  const response = await http.post<unknown>("/cleanup/schedules", input);
  const parsed = parseWith(cleanupScheduleEnvelopeSchema, response.data, {
    context: "cleanup schedule",
  });
  return parsed.schedule;
}

export async function updateSchedule(
  id: string,
  input: ScheduleForm,
): Promise<CleanupSchedule> {
  const response = await http.put<unknown>(`/cleanup/schedules/${id}`, input);
  const parsed = parseWith(cleanupScheduleEnvelopeSchema, response.data, {
    context: "cleanup schedule",
  });
  return parsed.schedule;
}

export async function deleteSchedule(id: string): Promise<void> {
  await http.delete(`/cleanup/schedules/${id}`);
}

export async function runScheduleNow(id: string): Promise<void> {
  await http.post(`/cleanup/schedules/${id}/run`);
}

export async function nextCronRun(expression: string): Promise<string> {
  const response = await http.get<unknown>("/cleanup/cron/next", {
    params: { expr: expression },
  });
  const parsed = parseWith(cleanupNextRunSchema, response.data, {
    context: "cleanup cron",
  });
  return parsed.next_run_at;
}

export async function listRuns(query: CleanupRunQuery = {}): Promise<CleanupRun[]> {
  const response = await http.get<unknown>("/cleanup/runs", {
    params: {
      server_id: query.serverId || undefined,
      limit: query.limit,
      before: query.before || undefined,
    },
  });
  const parsed = parseWith(cleanupRunListSchema, response.data, {
    context: "cleanup history",
  });
  return parsed.runs ?? [];
}

export async function getRun(id: string): Promise<CleanupRunDetail> {
  const response = await http.get<unknown>(`/cleanup/runs/${id}`);
  const parsed = parseWith(cleanupRunDetailSchema, response.data, {
    context: "cleanup run",
  });
  return {
    ...parsed.run,
    categories: parsed.run.categories ?? {},
    items: parsed.run.items ?? [],
  };
}

export async function listExclusions(): Promise<CleanupExclusion[]> {
  const response = await http.get<unknown>("/cleanup/exclusions");
  const parsed = parseWith(cleanupExclusionListSchema, response.data, {
    context: "cleanup exclusions",
  });
  return parsed.exclusions ?? [];
}

export async function createExclusion(input: ExclusionInput): Promise<CleanupExclusion> {
  const response = await http.post<unknown>("/cleanup/exclusions", input);
  const parsed = parseWith(cleanupExclusionEnvelopeSchema, response.data, {
    context: "cleanup exclusion",
  });
  return parsed.exclusion;
}

export async function deleteExclusion(id: string): Promise<void> {
  await http.delete(`/cleanup/exclusions/${id}`);
}
