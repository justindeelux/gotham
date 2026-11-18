import { http } from "@/shared/api/http";

export interface ApplicationContainer {
  id: string;
  name: string;
  service?: string;
  image: string;
  state: string;
  status: string;
  created?: string;
}

export type ApplicationContainerAction = "start" | "stop" | "restart";

interface ContainerListEnvelope {
  containers: ApplicationContainer[];
}

interface ContainerActionEnvelope {
  container_id: string;
}

export async function listApplicationContainers(
  appId: string,
): Promise<ApplicationContainer[]> {
  const response = await http.get<ContainerListEnvelope>(
    `/applications/${appId}/containers`,
  );
  return response.data.containers ?? [];
}

export async function applicationContainerAction(
  appId: string,
  containerId: string,
  action: ApplicationContainerAction,
): Promise<string> {
  const response = await http.post<ContainerActionEnvelope>(
    `/applications/${appId}/containers/${encodeURIComponent(containerId)}/${action}`,
    {},
  );
  return response.data.container_id;
}
