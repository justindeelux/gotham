import type { DropdownOption } from "naive-ui";
import {
  ArchiveRestore,
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
  Trash2,
  Upload,
} from "@lucide/vue";
import { h } from "vue";
import type { Component, VNode } from "vue";

import type { FilesEntry } from "@/features/files/schemas/files";
import { isExtractableArchive, isImageFile } from "@/features/files/utils/files";

/**
 * FilesMenuItem is one NDropdown option row: a label, the key the browser
 * dispatches on, an optional disabled state, and the explicit `role="menuitem"`
 * NDropdown's plain-div option body would otherwise omit. It is plain data so
 * the menu builders stay unit-testable without mounting a component.
 */
export type FilesMenuItem = DropdownOption;

/** FilesMenuContext is the gate set the menu builders read, plus a translator. */
export interface FilesMenuContext {
  canWrite: boolean;
  canReadContent: boolean;
  /** canPaste is true when a clipboard exists and the menu's destination is
   *  valid: the current directory for the folder menu, or the row's folder for
   *  a folder row (the store's pasteProblemAt is null). */
  canPaste: boolean;
  /** symlinkTargetPath is the root-relative path a symlink row's "Go to target"
   *  opens (JUS-132), or null when the target cannot be browsed; the action is
   *  offered only for a symlink with a browsable target. Ignored for other rows. */
  symlinkTargetPath?: string | null;
  t: (_key: string) => string;
}

/** item builds one menu row with the menuitem role NDropdown needs. */
function item(
  label: string,
  key: string,
  disabled = false,
  icon?: () => VNode,
): FilesMenuItem {
  return { label, key, disabled, icon, props: { role: "menuitem" } };
}

/** menuIcon renders one lucide icon for a dropdown option, matching the sort menu. */
export function menuIcon(icon: Component): () => VNode {
  return () => h(icon, { size: 14, "aria-hidden": "true" });
}

/**
 * rowMenuOptions builds the per-row (and right-click) actions menu. It carries
 * no plain "Move": cut + paste is the move, so the clipboard Copy/Cut pair plus
 * Paste is the only transfer surface. Paste is offered on a folder row only and
 * pastes INTO that folder; file rows have no destination, so no Paste. Disabled
 * states follow canWrite/canReadContent, so a read-only root greys the actions
 * rather than hiding them.
 */
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
  } else if (entry.kind === "file") {
    // Images open in the viewer (JUS-204), the row's default action, so the
    // menu leads with View exactly where folders lead with Open.
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
  // JUS-132: a symlink whose target lives inside the browsable root can jump
  // there. "" is the root's top level and stays valid; null (outside the root)
  // hides the action. The entry's own target must be present too.
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
  // Only a regular file or a folder can be chmod-ed: the agent refuses a
  // symlink (it would follow to the target) and a special file.
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
    // Paste is a folder action: on a folder row it pastes INTO that folder, so
    // it is offered only where a destination makes sense, never on a file row.
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

/**
 * folderMenuOptions builds the empty-space / folder-level menu: create, paste,
 * upload, the recycle bin and refresh. It never degrades to an empty menu —
 * Refresh stays enabled on a read-only root — while every write action follows
 * canWrite. The recycle bin is readable by any member (only restore/purge inside
 * it need owner/admin), so it is never disabled.
 */
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
    item(t("files.menu.recycleBin"), "recycle-bin", false, menuIcon(Recycle)),
    item(t("files.browser.refresh"), "refresh", false, menuIcon(RefreshCw)),
  ];
}
