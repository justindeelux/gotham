import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import type { ScheduleState, UpdateCheck, UpdateSchedule } from "@/features/updates/schemas/updates";
import { checkSchema, scheduleStateSchema } from "@/features/updates/schemas/updates";

/**
 * Typed client for the self-update routes (internal/updates/routes.go):
 *
 *   GET  /updates/check     any caller; resolves the newest release
 *   POST /updates/apply     platform admin; checks and applies
 *   GET  /updates/schedule  any caller; schedule + runtime status
 *   PUT  /updates/schedule  platform admin; validates, persists, hot-reloads
 *
 * FEATURE_UPDATES=false unmounts every route (404).
 */

/** The apply response body. */
export interface ApplyResult {
  applied: boolean;
  version?: string;
  message?: string;
  staged: boolean;
  restart: boolean;
}

export async function getUpdateCheck(): Promise<UpdateCheck> {
  const response = await http.get<UpdateCheck>("/updates/check");
  return parseWith(checkSchema, response.data, { context: "UpdateCheck" });
}

export async function applyUpdate(): Promise<ApplyResult> {
  const response = await http.post<ApplyResult>("/updates/apply");
  return response.data;
}

export async function getUpdateSchedule(): Promise<ScheduleState> {
  const response = await http.get<ScheduleState>("/updates/schedule");
  return parseWith(scheduleStateSchema, response.data, { context: "ScheduleState" });
}

export async function saveUpdateSchedule(schedule: UpdateSchedule): Promise<ScheduleState> {
  const response = await http.put<ScheduleState>("/updates/schedule", schedule);
  return parseWith(scheduleStateSchema, response.data, { context: "ScheduleState" });
}
