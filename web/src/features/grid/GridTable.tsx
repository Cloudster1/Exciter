import { useState, type KeyboardEvent } from "react";
import { Link } from "@tanstack/react-router";
import { showsOn, type Attribute } from "@/api/attributes";
import { PRIORITIES, useUpdateRequirement, type Priority, type RequirementChanges, type RequirementRow } from "@/api/requirements";
import type { Sort } from "@/api/search";
import { Button, Checkbox, IconButton, Input, SelectInput, Table, Td, Th, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";
import { REQUIREMENT_TITLE_MAX_LENGTH } from "@/config";
import { PickBox, type Picked, type Selection } from "@/features/armature/CreateIssues";
import { textOf, type DocNode } from "@/features/editor/schema";
import { LabelLink } from "@/features/labels/RequirementLabels";
import { usePeopleNames } from "@/features/types/AttributeFields";
import { TypeIcon } from "@/features/types/TypeIcon";
import { StatusBadge } from "@/features/workflows/StatusBadge";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { SORT_COLUMNS } from "./filter";
import { SuspectBadge } from "@/features/links/Suspect";

const changedOn = localDateFormat({ dateStyle: "medium" });

/** What the table says when an edit lands or is refused, so the grid can tell the reader. */
export interface EditResult {
  saved?: string;
  error?: string;
}

/** A column's heading: the field's own name, or the attribute's. */
export function columnName(column: string, attributes: Attribute[]): string {
  return t.grid.columnNames[column] ?? attributes.find((each) => each.id === column)?.name ?? column;
}

/** The grid's rows in the columns chosen, each heading a way to order by it, and the cells an editor may change in place. */
export function GridTable({
  projectKey,
  rows,
  columns,
  sort,
  attributes,
  selection,
  onSort,
  onEdit,
}: {
  projectKey: string;
  rows: RequirementRow[];
  columns: string[];
  sort: Sort;
  attributes: Attribute[];
  /** Rows are picked for Armature issues when there is one. */
  selection?: Selection;
  onSort: (next: Sort) => void;
  onEdit: (result: EditResult) => void;
}) {
  const sortable = (column: string) => (SORT_COLUMNS as readonly string[]).includes(column) || attributes.some((each) => each.id === column);
  // A heading goes from A to Z, to Z to A, and back to the outline.
  const next = (column: string): Sort => (sort.column !== column ? { column, desc: false } : sort.desc ? { column: "", desc: false } : { column, desc: true });
  const pickable: Picked[] = rows
    .filter((row) => row.kind === "requirement" && row.can.link && !row.trashed)
    .map((row) => ({ key: row.key, title: row.title, outline: row.outline }));
  const allPicked = pickable.length > 0 && pickable.every((each) => selection?.has(each.key));
  return (
    <Table aria-label={t.grid.tableLabel} dense data-grid-table>
      <thead>
        <tr>
          {selection && (
            <Th>
              <Checkbox
                label={<span className="sr-only">{t.armature.create.selectShown}</span>}
                checked={allPicked}
                disabled={pickable.length === 0}
                onChange={(event) => selection.set(pickable, event.target.checked)}
                data-pick-shown
              />
            </Th>
          )}
          {columns.map((column) => {
            const name = columnName(column, attributes);
            const sorted = sort.column === column ? (sort.desc ? "descending" : "ascending") : undefined;
            return (
              <Th key={column} aria-sort={sorted} data-grid-column={column}>
                {sortable(column) ? (
                  <Button
                    variant="link"
                    onClick={() => onSort(next(column))}
                    className="text-2xs font-medium tracking-wide text-ink-subtle uppercase hover:text-ink"
                    iconRight={sorted ? <Icon.ChevronDown className={sorted === "ascending" ? "size-3 rotate-180" : "size-3"} /> : undefined}
                    aria-label={`${t.grid.sortBy(name)}${sorted ? `, ${sorted === "ascending" ? t.grid.sortedUp : t.grid.sortedDown}` : ""}`}
                    data-action="sort-column"
                  >
                    {name}
                  </Button>
                ) : (
                  name
                )}
              </Th>
            );
          })}
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.id} data-grid-row={row.key}>
            {selection && (
              <Td>
                {pickable.some((each) => each.key === row.key) && (
                  <PickBox selection={selection} item={{ key: row.key, title: row.title, outline: row.outline }} />
                )}
              </Td>
            )}
            {columns.map((column) => (
              <Td key={column}>
                <Cell projectKey={projectKey} row={row} column={column} attributes={attributes} onEdit={onEdit} />
              </Td>
            ))}
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

function Cell({
  projectKey,
  row,
  column,
  attributes,
  onEdit,
}: {
  projectKey: string;
  row: RequirementRow;
  column: string;
  attributes: Attribute[];
  onEdit: (result: EditResult) => void;
}) {
  const update = useUpdateRequirement(row.key);
  const save = (body: RequirementChanges) =>
    update.mutate(body, { onSuccess: () => onEdit({ saved: t.grid.saved(row.key) }), onError: (error) => onEdit({ error: error.message }) });
  const requirement = row.kind === "requirement";
  switch (column) {
    case "outline":
      return <span className="text-ink-muted tabular-nums">{row.outline}</span>;
    case "key":
      return (
        <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
          <span className="font-mono text-xs text-ink-muted">{row.key}</span>
          {row.suspectLinks > 0 && <SuspectBadge count={row.suspectLinks} />}
        </span>
      );
    case "title":
      return <TitleCell projectKey={projectKey} row={row} onSave={(title) => save({ title, baseRevision: row.revision })} />;
    case "type":
      return row.type ? (
        <span className="inline-flex items-center gap-1 text-ink-muted">
          <TypeIcon icon={row.type.icon} colour={row.type.colour} />
          {row.type.name}
        </span>
      ) : row.kind === "section" ? (
        <Tag>{t.requirements.kinds.section}</Tag>
      ) : null;
    case "status":
      return row.status ? <StatusBadge name={row.status.name} category={row.status.category} /> : null;
    case "owner":
      return <span className="text-ink-muted">{row.owner?.name ?? ""}</span>;
    case "priority":
      return row.can.edit && requirement ? (
        <SelectInput
          aria-label={t.grid.editPriority(row.key)}
          value={row.priority ?? ""}
          disabled={update.isPending}
          onChange={(e) => save({ priority: e.target.value === "" ? null : (e.target.value as Priority) })}
          controlSize="sm"
          className="w-auto"
          data-grid-edit="priority"
        >
          <option value="">{t.requirements.noPriority}</option>
          {PRIORITIES.map((priority) => (
            <option key={priority} value={priority}>
              {t.requirements.priorities[priority]}
            </option>
          ))}
        </SelectInput>
      ) : (
        <span className="text-ink-muted">{row.priority ? t.requirements.priorities[row.priority] : ""}</span>
      );
    case "updated":
      return <span className="text-ink-muted">{changedOn.format(new Date(row.updatedAt))}</span>;
    case "labels":
      return (
        <span className="flex flex-wrap gap-1">
          {row.labels.map((name) => (
            <LabelLink key={name} name={name} projectKey={projectKey} />
          ))}
        </span>
      );
  }
  const attribute = attributes.find((each) => each.id === column);
  if (!attribute) return null;
  const editable = row.can.edit && requirement && !attribute.retired && showsOn(attribute, row.type?.id);
  return (
    <ValueCell
      attribute={attribute}
      row={row}
      editable={editable}
      busy={update.isPending}
      onSave={(value) => save({ attributes: { [attribute.id]: value }, baseRevision: row.revision })}
    />
  );
}

function TitleCell({ projectKey, row, onSave }: { projectKey: string; row: RequirementRow; onSave: (title: string) => void }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(row.title);
  if (editing) {
    const done = (keep: boolean) => {
      setEditing(false);
      if (keep && draft.trim() && draft.trim() !== row.title) onSave(draft.trim());
    };
    return (
      <Input
        aria-label={t.grid.editTitle(row.key)}
        value={draft}
        maxLength={REQUIREMENT_TITLE_MAX_LENGTH}
        // biome-ignore lint/a11y/noAutofocus: the reader asked to edit this cell, so the box takes the focus they gave the button
        autoFocus
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => done(true)}
        onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
          if (e.key === "Enter") done(true);
          if (e.key === "Escape") {
            e.stopPropagation();
            setDraft(row.title);
            setEditing(false);
          }
        }}
        controlSize="sm"
        data-grid-edit="title"
      />
    );
  }
  return (
    <span className="inline-flex items-center gap-1">
      <Link to="/p/$projectKey/r/$reqKey" params={{ projectKey, reqKey: row.key }} className="font-medium text-ink hover:text-accent hover:underline">
        {row.title}
      </Link>
      {row.can.edit && (
        <IconButton
          icon={<Icon.Edit className="size-3" />}
          label={t.grid.editTitle(row.key)}
          size="xs"
          onClick={() => {
            setDraft(row.title);
            setEditing(true);
          }}
          data-action="edit-title"
        />
      )}
    </span>
  );
}

/** One attribute's value, shown as a reader knows it, and for an editor changed in place where its kind allows. */
function ValueCell({
  attribute,
  row,
  editable,
  busy,
  onSave,
}: {
  attribute: Attribute;
  row: RequirementRow;
  editable: boolean;
  busy: boolean;
  onSave: (value: unknown) => void;
}) {
  const { data: names } = usePeopleNames();
  const value = row.attributes[attribute.id];
  const label = t.grid.editValue(attribute.name, row.key);
  if (editable && (attribute.kind === "select" || attribute.kind === "boolean")) {
    const current = value === undefined || value === null ? "" : String(value);
    return (
      <SelectInput
        aria-label={label}
        value={current}
        disabled={busy}
        onChange={(e) => onSave(e.target.value === "" ? null : attribute.kind === "boolean" ? e.target.value === "true" : e.target.value)}
        controlSize="sm"
        className="w-auto"
        data-grid-edit={attribute.name}
      >
        <option value="">{t.grid.none}</option>
        {attribute.kind === "boolean" ? (
          <>
            <option value="true">{t.grid.yes}</option>
            <option value="false">{t.grid.no}</option>
          </>
        ) : (
          attribute.options
            .filter((option) => !option.retired || option.id === value)
            .map((option) => (
              <option key={option.id} value={option.id}>
                {option.name}
              </option>
            ))
        )}
      </SelectInput>
    );
  }
  if (editable && (attribute.kind === "text" || attribute.kind === "number" || attribute.kind === "date")) {
    const current = value === undefined || value === null ? "" : String(value);
    const commit = (raw: string) => {
      const trimmed = raw.trim();
      if (trimmed === current) return;
      onSave(trimmed === "" ? null : attribute.kind === "number" ? Number(trimmed) : trimmed);
    };
    return (
      <Input
        key={`${row.revision}:${current}`}
        aria-label={label}
        defaultValue={current}
        type={attribute.kind === "text" ? "text" : attribute.kind}
        step={attribute.kind === "number" ? "any" : undefined}
        disabled={busy}
        onBlur={(e) => commit(e.target.value)}
        onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => e.key === "Enter" && commit(e.currentTarget.value)}
        controlSize="sm"
        className="min-w-24"
        data-grid-edit={attribute.name}
      />
    );
  }
  if (value === undefined || value === null) return null;
  const option = (id: unknown) => attribute.options.find((each) => each.id === id)?.name ?? String(id);
  const shown =
    attribute.kind === "select"
      ? option(value)
      : attribute.kind === "multiSelect"
        ? (value as string[]).map(option).join(", ")
        : attribute.kind === "boolean"
          ? value
            ? t.grid.yes
            : t.grid.no
          : attribute.kind === "user"
            ? (names?.get(value as string) ?? "")
            : attribute.kind === "richText"
              ? textOf(value as DocNode)
              : String(value);
  return <span className="line-clamp-2 text-ink-muted">{shown}</span>;
}
