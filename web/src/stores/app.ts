import { defineStore } from "pinia";

interface AppState {
  sidebarCollapsed: boolean;
}

export const useAppStore = defineStore("app", {
  state: (): AppState => ({
    sidebarCollapsed: false,
  }),
  actions: {
    setSidebarCollapsed(collapsed: boolean): void {
      this.sidebarCollapsed = collapsed;
    },
    toggleSidebar(): void {
      this.sidebarCollapsed = !this.sidebarCollapsed;
    },
  },
});
