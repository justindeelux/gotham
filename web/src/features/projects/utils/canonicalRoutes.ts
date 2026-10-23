
export interface RouteScope {
  projectId: string;
  environmentId: string;
}

export function resolveEnvironmentScope(
  response: { projectId: string; environmentId: string },
  route: RouteScope,
): RouteScope | null {
  if (
    response.projectId === route.projectId &&
    response.environmentId === route.environmentId
  ) {
    return null;
  }
  return { projectId: response.projectId, environmentId: response.environmentId };
}
