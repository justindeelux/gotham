import {
  ArrowRight,
  Copy,
  Download,
  FolderOpen,
  Lock,
  Pencil,
  Trash2,
} from "@lucide/vue";
import type { DropdownOption } from "naive-ui";
import { h } from "vue";
import type { Component, VNode } from "vue";

import type { ContainerFileEntry } from "@/features/applications/api/containerFiles";

export interface ContainerFileMenuContext {
  canWrite: boolean;
  canExec: boolean;
  execReason: string;
  t: (_key: string) => string;
}

function icon(component: Component): () => VNode {
  return () => h(component, { size: 14, "aria-hidden": "true" });
}

function item(
  label: string,
  key: string,
  disabled: boolean,
  mark: () => VNode,
  hint?: string,
): DropdownOption {
  return {
    label,
    key,
    disabled,
    icon: mark,
    props: hint
      ? { role: "menuitem", title: hint }
      : { role: "menuitem" },
  };
}

// The container file manager follows the server file manager menu: the same
// actions, labels and icons, minus everything that needs a files root.
export function containerFileMenuOptions(
  entry: ContainerFileEntry,
  ctx: ContainerFileMenuContext,
): DropdownOption[] {
  const { t, canWrite, canExec, execReason } = ctx;
  const options: DropdownOption[] = [];
  if (entry.kind === "directory") {
    options.push(
      item(t("applications.files.actions.open"), "open", false, icon(FolderOpen)),
    );
  }
  if (entry.kind === "file" || entry.kind === "symlink") {
    options.push(
      item(t("applications.files.actions.edit"), "edit", false, icon(Pencil)),
    );
  }
  if (entry.kind !== "special") {
    options.push(
      item(
        t("applications.files.actions.download"),
        "download",
        false,
        icon(Download),
      ),
    );
  }
  if (!canWrite) {
    return options;
  }
  const execItem = (
    label: string,
    key: string,
    mark: Component,
  ): DropdownOption =>
    item(label, key, !canExec, icon(mark), canExec ? undefined : execReason);
  options.push(
    execItem(t("applications.files.actions.rename"), "rename", Pencil),
    execItem(t("applications.files.actions.copy"), "copy", Copy),
    execItem(t("applications.files.actions.move"), "move", ArrowRight),
    execItem(t("applications.files.actions.chmod"), "chmod", Lock),
    execItem(t("applications.files.actions.delete"), "delete", Trash2),
  );
  return options;
}
