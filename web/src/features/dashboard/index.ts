/**
 * Dashboard feature public surface. The auth store resets the user-scoped
 * recent-deployments cache on sign-out, so it is exported here (features may
 * only import another feature through its index).
 */
export {
  recentDeploymentsLimit,
  useRecentDeploymentsStore,
} from "./stores/deployments";
