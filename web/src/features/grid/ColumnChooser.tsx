import { useId, useState } from "react";
import type { Attribute } from "@/api/attributes";
import { Button, Checkbox } from "@/components/ui";
import { Icon } from "@/components/icons";
import { GRID_MAX_COLUMNS } from "@/config";
import { t } from "@/i18n";
import { SHOW_COLUMNS, SORT_COLUMNS } from "./filter";
import { columnName } from "./GridTable";

/** The columns the grid can show, ticked off in place; the order is the list's, whatever order they were ticked in. */
export function ColumnChooser({ columns, attributes, onChange }: { columns: string[]; attributes: Attribute[]; onChange: (next: string[]) => void }) {
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const offered = [...SHOW_COLUMNS, ...SORT_COLUMNS, ...attributes.filter((each) => !each.retired || columns.includes(each.id)).map((each) => each.id)];
  const toggle = (column: string, on: boolean) => {
    const chosen = on ? [...columns, column] : columns.filter((each) => each !== column);
    onChange(offered.filter((each) => chosen.includes(each)));
  };
  return (
    <div data-column-chooser>
      <Button
        variant="secondary"
        size="sm"
        icon={<Icon.Table />}
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen(!open)}
        data-action="choose-columns"
      >
        {t.grid.columns}
      </Button>
      {open && (
        <fieldset id={panelId} className="mt-2 grid gap-1.5 rounded-control border border-border bg-surface p-3 sm:grid-cols-3" data-column-options>
          <legend className="sr-only">{t.grid.columns}</legend>
          {offered.map((column) => (
            <Checkbox
              key={column}
              label={columnName(column, attributes)}
              checked={columns.includes(column)}
              disabled={!columns.includes(column) && columns.length >= GRID_MAX_COLUMNS}
              onChange={(e) => toggle(column, e.target.checked)}
              data-column-option={column}
            />
          ))}
        </fieldset>
      )}
    </div>
  );
}
