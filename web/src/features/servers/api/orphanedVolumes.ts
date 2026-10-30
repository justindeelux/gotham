import { http } from "@/shared/api/http";

export interface OrphanedVolume {
  name: string;
  size_bytes: number;
  created_at?: string;
}

interface OrphanedVolumeListEnvelope {
  volumes: OrphanedVolume[];
}

export async function listOrphanedVolumes(
  serverId: string,
): Promise<OrphanedVolume[]> {
  const response = await http.get<OrphanedVolumeListEnvelope>(
    `/servers/${serverId}/orphaned-volumes`,
  );
  return response.data.volumes ?? [];
}

export async function removeOrphanedVolume(
  serverId: string,
  name: string,
): Promise<void> {
  await http.delete(
    `/servers/${serverId}/orphaned-volumes/${encodeURIComponent(name)}`,
  );
}
