// backupsModel.ts — S62 T10: the pure helpers the Backups panel renders with.
// The model computes presentation only (byte sizes, the restore command
// string): every fact — state, age, size, schema, restorability — arrives
// from the daemon and is rendered as sent. Nothing here decides what a
// backup IS.

import type { RpcMethods } from "../../../proto/contract";

/** One row of `backup.list`, the daemon's own view of a copied file. */
export type BackupView = RpcMethods["backup.list"]["result"]["backups"][number];

/** The `backup.status` answer — the header's every word comes from here. */
export type BackupStatus = RpcMethods["backup.status"]["result"];

/** The `backup.prune` plan row: a file, a keep flag, the daemon's reason. */
export type PrunePlanRow = RpcMethods["backup.prune"]["result"]["plan"][number];

/**
 * Formats a byte count the way the CLI does: powers of 1024, the unit named
 * once, the number never faked into more precision than it has. A size is a
 * fact the daemon measured; this only gives it a human shape.
 */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "?";
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let value = bytes;
  let unit = -1;
  do {
    value /= 1024;
    unit += 1;
  } while (value >= 1024 && unit < units.length - 1);
  const shown = value >= 100 ? Math.round(value).toString() : value.toFixed(1).replace(/\.0$/, "");
  return `${shown} ${units[unit]}`;
}

/** The command the operator runs to restore one named file — hint only. */
export function restoreCommand(file: string): string {
  return `aeon backup restore ${file}`;
}

/** The schema line for one row: how much was applied and at which migration it stopped. */
export function schemaLine(view: BackupView): string {
  return `${view.applied} applied · last ${view.lastMigration}`;
}

/** The files the prune plan marks for removal, in plan order. */
export function planRemovals(plan: PrunePlanRow[]): PrunePlanRow[] {
  return plan.filter((row) => !row.keep);
}
