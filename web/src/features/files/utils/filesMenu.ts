import type { DropdownOption } from "naive-ui";

import type { FilesEntry } from "@/features/files/schemas/files";
import { isExtractableArchive } from "@/features/files/utils/files";

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
  t: (_key: string) => string;
}

/** item builds one menu row with the menuitem role NDropdown needs. */
function item(label: string, key: string, disabled = false): FilesMenuItem {
  return { label, key, disabled, props: { role: "menuitem" } };
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
    options.push(item(t("files.menu.open"), "open"));
    options.push(item(t("files.menu.openTab"), "open-tab"));
  } else if (entry.kind === "file") {
    options.push(item(t("files.menu.edit"), "edit", !canReadContent));
    options.push(item(t("files.menu.download"), "download", !canReadContent));
  }
  // Only a regular file or a folder can be chmod-ed: the agent refuses a
  // symlink (it would follow to the target) and a special file.
  if (entry.kind === "file" || entry.kind === "directory") {
    options.push(item(t("files.menu.permissions"), "permissions", !canWrite));
  }
  if (entry.kind !== "special" && entry.kind !== "unspecified") {
    if (entry.kind === "file" && isExtractableArchive(entry)) {
      options.push(item(t("files.actions.extract"), "extract", !canWrite));
    }
    options.push(item(t("files.actions.rename"), "rename", !canWrite));
    options.push(item(t("files.actions.cut"), "cut", !canWrite));
    options.push(item(t("files.actions.copy"), "copy", !canWrite));
    // Paste is a folder action: on a folder row it pastes INTO that folder, so
    // it is offered only where a destination makes sense, never on a file row.
    if (entry.kind === "directory") {
      options.push(
        item(t("files.actions.paste"), "paste", !canWrite || !canPaste),
      );
    }
    options.push(item(t("common.actions.delete"), "delete", !canWrite));
  }
  return options;
}

/**
 * folderMenuOptions builds the empty-space / folder-level menu: create, paste,
 * upload and refresh. It never degrades to an empty menu — Refresh stays enabled
 * on a read-only root — while every write action follows canWrite. New entries
 * are appended here, so a later recycle-bin entry only adds one item.
 */
export function folderMenuOptions(ctx: FilesMenuContext): FilesMenuItem[] {
  const { t, canWrite, canPaste } = ctx;
  return [
    item(t("files.actions.newFile"), "new-file", !canWrite),
    item(t("files.actions.newFolder"), "new-folder", !canWrite),
    item(t("files.actions.paste"), "paste", !canWrite || !canPaste),
    item(t("files.actions.upload"), "upload", !canWrite),
    item(t("files.browser.refresh"), "refresh"),
  ];
}
