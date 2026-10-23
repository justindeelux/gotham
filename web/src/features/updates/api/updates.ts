import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import type { ScheduleState, UpdateChangelog, UpdateCheck, UpdateSchedule } from "@/features/updates/schemas/updates";
import { changelogSchema, checkSchema, scheduleStateSchema } from "@/features/updates/schemas/updates";

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

export async function getUpdateChangelog(): Promise<UpdateChangelog> {
  const response = await http.get<UpdateChangelog>("/updates/changelog");
  return parseWith(changelogSchema, response.data, { context: "UpdateChangelog" });
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
