import type { ProviderRepo } from "@/features/applications/api/providers";

export function cloneUrlFor(repo: ProviderRepo): string {
  const ssh = repo.ssh_url?.trim() ?? "";
  if (repo.private) {
    return ssh;
  }
  return repo.clone_url;
}

export function suggestAppName(repoName: string): string {
  return repoName.toLowerCase().replace(/[^a-z0-9-]+/g, "-").slice(0, 31);
}
