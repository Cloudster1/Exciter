import { useState } from "react";
import { auditExportHref, useAuditFacets, useAuditLog, type AuditEntry, type AuditFilter } from "@/api/audit";
import { ApiError } from "@/api/client";
import { Button, ButtonLink, EmptyState, ErrorBanner, Field, PageHeader, Select, Skeleton, Table, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import { Through } from "@/components/Through";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

// Adapted from Armature's audit page by way of Stator's: the same filters and
// CSV export, walked by the API's keyset cursor rather than by time alone.

const when = localDateFormat({ dateStyle: "medium", timeStyle: "medium" });

/** The words a reader knows a target by, as the entry kept them, else its id. */
export function targetName(entry: AuditEntry): string {
  const data = entry.data as Record<string, unknown>;
  for (const key of ["title", "name", "key", "group", "issuer", "baseUrl"]) {
    const value = data[key];
    if (typeof value === "string" && value) return value;
  }
  return entry.targetId ?? "";
}

/** What the entry's data says to a reader, without the ids and the target's own name. */
export function describe(entry: AuditEntry): string {
  const parts: string[] = [];
  for (const [key, value] of Object.entries(entry.data as Record<string, unknown>)) {
    if (/id$/i.test(key) || value === null || value === undefined || value === "") continue;
    if (Array.isArray(value)) {
      const plain = value.every((each) => typeof each !== "object" || each === null);
      parts.push(`${key}: ${plain ? value.join(", ") : value.length}`);
    } else if (typeof value !== "object") {
      parts.push(`${key}: ${String(value)}`);
    }
  }
  if (entry.ip) parts.push(t.audit.fromAddress(entry.ip));
  return parts.join("; ");
}

function typeLabel(type: string): string {
  return t.audit.targetTypes[type] ?? type;
}

/** The installation's audit log for its administrators: filters, a page of entries, and the export. */
export function AuditLog() {
  const [filter, setFilter] = useState<AuditFilter>({});
  const [targetLabel, setTargetLabel] = useState("");
  // Each page is reached by the cursor the one before handed out; going back pops it.
  const [cursors, setCursors] = useState<string[]>([]);
  const log = useAuditLog(filter, cursors[cursors.length - 1]);
  const facets = useAuditFacets();
  const entries = log.data?.entries ?? [];
  const next = log.data?.next ?? null;
  const problem = log.error instanceof ApiError ? log.error : null;
  const filtered = Object.values(filter).some(Boolean);

  function narrow(patch: Partial<AuditFilter>) {
    setFilter((current) => ({ ...current, ...patch }));
    setCursors([]);
  }

  function clear() {
    setFilter({});
    setTargetLabel("");
    setCursors([]);
  }

  const retention = facets.data?.retentionDays;
  return (
    <div className="mx-auto max-w-5xl" data-audit-log>
      <PageHeader
        title={t.audit.title}
        meta={retention === undefined ? undefined : retention > 0 ? t.audit.keptDays(retention) : t.audit.keptForever}
        actions={
          <ButtonLink href={auditExportHref(filter)} download size="sm" icon={<Icon.Download />} data-action="export-audit">
            {t.audit.export}
          </ButtonLink>
        }
      />
      <p className="mb-4 text-sm text-ink-muted">{t.audit.intro}</p>
      <section aria-label={t.audit.filters} className="mb-4 space-y-3" data-audit-filters>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <Select
            label={t.audit.action}
            id="audit-action"
            value={filter.action ?? ""}
            onChange={(e) => narrow({ action: (e.target.value || undefined) as AuditFilter["action"] })}
          >
            <option value="">{t.audit.everyAction}</option>
            {(facets.data?.actions ?? []).map((action) => (
              <option key={action} value={action}>
                {t.audit.actions[action]}
              </option>
            ))}
          </Select>
          <Select label={t.audit.actor} id="audit-actor" value={filter.actor ?? ""} onChange={(e) => narrow({ actor: e.target.value || undefined })}>
            <option value="">{t.audit.everybody}</option>
            {(facets.data?.actors ?? []).map((actor) => (
              <option key={actor.id} value={actor.id}>
                {actor.name || t.audit.formerMember}
              </option>
            ))}
          </Select>
          <Select
            label={t.audit.targetType}
            id="audit-target-type"
            value={filter.targetType ?? ""}
            onChange={(e) => narrow({ targetType: e.target.value || undefined })}
          >
            <option value="">{t.audit.everyTarget}</option>
            {(facets.data?.targetTypes ?? []).map((type) => (
              <option key={type} value={type}>
                {typeLabel(type)}
              </option>
            ))}
          </Select>
          <Field
            label={t.audit.from}
            id="audit-from"
            type="date"
            value={filter.from ?? ""}
            error={problem?.fields.from}
            onChange={(e) => narrow({ from: e.target.value || undefined })}
          />
          <Field
            label={t.audit.to}
            id="audit-to"
            type="date"
            value={filter.to ?? ""}
            error={problem?.fields.to}
            onChange={(e) => narrow({ to: e.target.value || undefined })}
          />
        </div>
        {(filter.target || filtered) && (
          <div className="flex flex-wrap items-center gap-2">
            {filter.target && (
              <span
                className="inline-flex items-center gap-1 rounded-full border border-border bg-surface-raised py-0.5 pr-1 pl-2.5 text-sm text-ink"
                data-audit-target-filter
              >
                {t.audit.onlyTarget(targetLabel || filter.target)}
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6! px-1!"
                  aria-label={t.audit.showEveryTarget}
                  onClick={() => {
                    setTargetLabel("");
                    narrow({ target: undefined });
                  }}
                >
                  <Icon.X />
                </Button>
              </span>
            )}
            <Button variant="ghost" size="sm" onClick={clear} data-action="clear-audit-filters">
              {t.audit.clearFilters}
            </Button>
          </div>
        )}
      </section>
      {log.error && !problem?.fields.from && !problem?.fields.to && <ErrorBanner onRetry={() => void log.refetch()}>{log.error.message}</ErrorBanner>}
      {log.isLoading ? (
        <Skeleton />
      ) : entries.length === 0 ? (
        <EmptyState
          icon={<Icon.Shield />}
          title={t.audit.empty}
          description={cursors.length > 0 ? t.audit.emptyLast : filtered ? t.audit.emptyFiltered : undefined}
        />
      ) : (
        <Table data-audit-table>
          <thead>
            <tr>
              <Th className="w-44">{t.audit.columnWhen}</Th>
              <Th>{t.audit.columnWhat}</Th>
              <Th className="w-40">{t.audit.columnWho}</Th>
              <Th>{t.audit.columnTarget}</Th>
              <Th>{t.audit.columnDetails}</Th>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry) => (
              <AuditRow
                key={entry.id}
                entry={entry}
                onActor={(actor) => narrow({ actor })}
                onTarget={(target, label) => {
                  setTargetLabel(label);
                  narrow({ target });
                }}
              />
            ))}
          </tbody>
        </Table>
      )}
      {(cursors.length > 0 || next) && (
        <nav aria-label={t.audit.pages} className="mt-3 flex items-center justify-end gap-2">
          <span className="mr-auto text-sm text-ink-muted" data-audit-page>
            {t.audit.page(cursors.length + 1)}
          </span>
          <Button variant="secondary" size="sm" disabled={cursors.length === 0} onClick={() => setCursors(cursors.slice(0, -1))} data-action="audit-newer">
            {t.audit.newer}
          </Button>
          <Button variant="secondary" size="sm" disabled={!next} onClick={() => next && setCursors([...cursors, next])} data-action="audit-older">
            {t.audit.older}
          </Button>
        </nav>
      )}
    </div>
  );
}

/** One entry as a row; the person and the target narrow the log to themselves when followed. */
function AuditRow({ entry, onActor, onTarget }: { entry: AuditEntry; onActor: (actor: string) => void; onTarget: (target: string, label: string) => void }) {
  const name = targetName(entry);
  const actor = entry.actorId ? entry.actorName || t.audit.formerMember : t.audit.system;
  const { actorId, targetId } = entry;
  return (
    <tr data-audit-row={entry.action}>
      <Td className="text-sm whitespace-nowrap text-ink-muted">
        <time dateTime={entry.createdAt}>{when.format(new Date(entry.createdAt))}</time>
      </Td>
      <Td className="text-sm text-ink">
        {t.audit.actions[entry.action]}
        <span className="block font-mono text-2xs text-ink-subtle">{entry.action}</span>
      </Td>
      <Td className="text-sm" data-audit-actor>
        {actorId ? (
          <button
            type="button"
            className="text-left text-ink underline-offset-2 hover:underline"
            aria-label={t.audit.filterByActor(actor)}
            onClick={() => onActor(actorId)}
          >
            {actor}
          </button>
        ) : (
          <span className="text-ink-subtle">{actor}</span>
        )}
        <Through via={entry.via} className="block" />
      </Td>
      <Td className="text-sm">
        <span className="block text-2xs text-ink-subtle">{typeLabel(entry.targetType)}</span>
        {targetId ? (
          <button
            type="button"
            className="max-w-48 truncate text-left text-ink underline-offset-2 hover:underline"
            aria-label={t.audit.filterByTarget(name)}
            onClick={() => onTarget(targetId, name)}
            data-action="filter-target"
          >
            {name}
          </button>
        ) : (
          name && <span className="text-ink">{name}</span>
        )}
      </Td>
      <Td className="text-xs break-words text-ink-muted">{describe(entry)}</Td>
    </tr>
  );
}
