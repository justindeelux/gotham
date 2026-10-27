import type { DropdownOption } from "naive-ui";
import {
  ArchiveRestore,
  ArrowRight,
  ChevronRight,
  ClipboardPaste,
  Copy,
  Download,
  ExternalLink,
  Eye,
  FilePlus,
  FolderOpen,
  FolderPlus,
  Lock,
  Pencil,
  Recycle,
  RefreshCw,
  Scissors,
  Terminal,
  Trash2,
  Upload,
} from "@lucide/vue";
import { h } from "vue";
import type { Component, VNode } from "vue";

import type { FilesEntry } from "@/features/files/schemas/files";
import { isExtractableArchive, isImageFile } from "@/features/files/utils/files";

export type FilesMenuItem = DropdownOption;

export interface FilesMenuContext {
  canWrite: boolean;
  canReadContent: boolean;
  canPaste: boolean;
  symlinkTargetPath?: string | null;
  canOpenTerminal: boolean;
  symlinkTargetDirectory?: boolean;
  canCopyToNode?: boolean;
  copyToNodeLabel?: string;
  canCopyToPane?: boolean;
  copyToPaneLabel?: string;
  copyToPaneReason?: string;
  t: (_key: string, _named?: Record<string, unknown>) => string;
}

function item(
  label: string,
  key: string,
  disabled = false,
  icon?: () => VNode,
  hint?: string,
): FilesMenuItem {
  return {
    label,
    key,
    disabled,
    icon,
    props: hint
      ? { role: "menuitem", title: hint }
      : { role: "menuitem" },
  };
}

export function menuIcon(icon: Component): () => VNode {
  return () => h(icon, { size: 14, "aria-hidden": "true" });
}

export function rowMenuOptions(
  entry: FilesEntry,
  ctx: FilesMenuContext,
): FilesMenuItem[] {
  const { t, canWrite, canReadContent, canPaste } = ctx;
  const options: FilesMenuItem[] = [];
  if (entry.kind === "directory") {
    options.push(
      item(t("files.menu.open"), "open", false, menuIcon(FolderOpen)),
    );
    options.push(
      item(t("files.menu.openTab"), "open-tab", false, menuIcon(ExternalLink)),
    );
    if (ctx.canOpenTerminal) {
      options.push(
        item(
          t("files.terminal.openIn", { name: entry.name }),
          "open-terminal",
          false,
          menuIcon(Terminal),
        ),
      );
    }
  } else if (entry.kind === "file") {
    if (isImageFile(entry)) {
      options.push(
        item(t("files.menu.view"), "view", !canReadContent, menuIcon(Eye)),
      );
    }
    options.push(
      item(t("files.menu.edit"), "edit", !canReadContent, menuIcon(Pencil)),
    );
    options.push(
      item(
        t("files.menu.download"),
        "download",
        !canReadContent,
        menuIcon(Download),
      ),
    );
  }
  if (
    entry.kind === "symlink" &&
    entry.symlink_target &&
    typeof ctx.symlinkTargetPath === "string"
  ) {
    options.push(
      item(
        t("files.menu.goTarget"),
        "go-target",
        false,
        menuIcon(ChevronRight),
      ),
    );
  }
  if (entry.kind === "symlink" && ctx.canOpenTerminal) {
    const knownDirectory = ctx.symlinkTargetDirectory === true;
    options.push(
      item(
        t("files.terminal.openIn", { name: entry.name }),
        "open-terminal",
        !knownDirectory,
        menuIcon(Terminal),
        knownDirectory
          ? undefined
          : t("files.terminal.symlinkTargetUnknown"),
      ),
    );
  }
  if (entry.kind === "file" || entry.kind === "directory") {
    options.push(
      item(
        t("files.menu.permissions"),
        "permissions",
        !canWrite,
        menuIcon(Lock),
      ),
    );
  }
  if (entry.kind !== "special" && entry.kind !== "unspecified") {
    if (entry.kind === "file" && isExtractableArchive(entry)) {
      options.push(
        item(
          t("files.actions.extract"),
          "extract",
          !canWrite,
          menuIcon(ArchiveRestore),
        ),
      );
    }
    options.push(
      item(t("files.actions.rename"), "rename", !canWrite, menuIcon(Pencil)),
    );
    options.push(
      item(t("files.actions.cut"), "cut", !canWrite, menuIcon(Scissors)),
    );
    options.push(
      item(t("files.actions.copy"), "copy", !canWrite, menuIcon(Copy)),
    );
    if (ctx.canCopyToNode !== undefined) {
      options.push(
        item(
          ctx.copyToNodeLabel ?? t("files.transfer.copyToNode"),
          "copy-to-node",
          !ctx.canCopyToNode,
          menuIcon(Copy),
        ),
      );
    }
    if (ctx.canCopyToPane !== undefined) {
      options.push(
        item(
          ctx.copyToPaneLabel ?? t("files.transfer.titlePane"),
          "copy-to-pane",
          !ctx.canCopyToPane,
          menuIcon(ArrowRight),
          ctx.copyToPaneReason,
        ),
      );
    }
    if (entry.kind === "directory") {
      options.push(
        item(
          t("files.actions.paste"),
          "paste",
          !canWrite || !canPaste,
          menuIcon(ClipboardPaste),
        ),
      );
    }
    options.push(
      item(
        t("common.actions.delete"),
        "delete",
        !canWrite,
        menuIcon(Trash2),
      ),
    );
  }
  return options;
}

export function folderMenuOptions(ctx: FilesMenuContext): FilesMenuItem[] {
  const { t, canWrite, canPaste } = ctx;
  return [
    item(t("files.actions.newFile"), "new-file", !canWrite, menuIcon(FilePlus)),
    item(
      t("files.actions.newFolder"),
      "new-folder",
      !canWrite,
      menuIcon(FolderPlus),
    ),
    item(
      t("files.actions.paste"),
      "paste",
      !canWrite || !canPaste,
      menuIcon(ClipboardPaste),
    ),
    item(t("files.actions.upload"), "upload", !canWrite, menuIcon(Upload)),
    ...(ctx.canOpenTerminal
      ? [
          item(
            t("files.terminal.openHere"),
            "open-terminal",
            false,
            menuIcon(Terminal),
          ),
        ]
      : []),
    item(t("files.menu.recycleBin"), "recycle-bin", false, menuIcon(Recycle)),
    item(t("files.browser.refresh"), "refresh", false, menuIcon(RefreshCw)),
  ];
}
