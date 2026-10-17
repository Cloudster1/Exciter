import type { FieldPermit } from "@/api/attributes";
import type { Status } from "@/api/workflows";
import { Button, Checkbox, IconButton, Select } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

const ROLES = ["viewer", "reviewer", "editor", "admin"] as const;

/** The status a field is required from, or none (#206). */
export function RequiredFrom({
  id,
  statuses,
  value,
  onChange,
  error,
}: {
  id: string;
  statuses: Status[];
  value: string | null;
  onChange: (value: string | null) => void;
  error?: string;
}) {
  return (
    <Select
      label={t.fieldRules.requiredFrom}
      id={id}
      hint={t.fieldRules.requiredFromHint}
      value={value ?? ""}
      error={error}
      onChange={(event) => onChange(event.target.value || null)}
      data-field="required-from"
    >
      <option value="">{t.fieldRules.never}</option>
      {statuses.map((status) => (
        <option key={status.id} value={status.id}>
          {status.name}
        </option>
      ))}
    </Select>
  );
}

/** Who changes a field: lines of roles and the statuses they change it in, none for whoever edits (#206). */
export function PermitsEditor({
  id,
  statuses,
  permits,
  onChange,
  error,
}: {
  id: string;
  statuses: Status[];
  permits: FieldPermit[];
  onChange: (permits: FieldPermit[]) => void;
  error?: string;
}) {
  const set = (at: number, permit: FieldPermit) => onChange(permits.map((each, i) => (i === at ? permit : each)));
  const toggle = (list: string[], value: string, on: boolean) => (on ? [...list, value] : list.filter((each) => each !== value));
  return (
    <fieldset className="space-y-2" aria-describedby={`${id}-hint`} data-permits-editor>
      <legend className="text-sm font-medium text-ink-muted">{t.fieldRules.editableBy}</legend>
      <p id={`${id}-hint`} className="text-xs text-ink-subtle">
        {t.fieldRules.editableByHint}
      </p>
      {permits.map((permit, at) => (
        <div key={at} className="space-y-1 rounded-control border border-border p-2" data-permit={at + 1}>
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-semibold text-ink-muted">{t.fieldRules.permit(at + 1)}</span>
            <IconButton size="xs" icon={<Icon.X />} label={t.fieldRules.removePermit(at + 1)} onClick={() => onChange(permits.filter((_, i) => i !== at))} />
          </div>
          <fieldset className="flex flex-wrap gap-x-4 gap-y-1">
            <legend className="sr-only">{`${t.fieldRules.permit(at + 1)}: ${t.fieldRules.rolesLegend}`}</legend>
            {ROLES.map((role) => (
              <Checkbox
                key={role}
                label={t.permissions.roles[role] ?? role}
                checked={permit.roles.includes(role)}
                onChange={(event) => set(at, { ...permit, roles: toggle(permit.roles, role, event.target.checked) })}
              />
            ))}
          </fieldset>
          <fieldset className="flex flex-wrap gap-x-4 gap-y-1">
            <legend className="text-xs text-ink-subtle">{t.fieldRules.statusesLegend}</legend>
            {statuses.map((status) => (
              <Checkbox
                key={status.id}
                label={status.name}
                checked={permit.statuses.includes(status.id)}
                onChange={(event) => set(at, { ...permit, statuses: toggle(permit.statuses, status.id, event.target.checked) })}
              />
            ))}
          </fieldset>
        </div>
      ))}
      <Button size="sm" variant="secondary" icon={<Icon.Plus />} onClick={() => onChange([...permits, { roles: [], statuses: [] }])} data-action="add-permit">
        {t.fieldRules.addPermit}
      </Button>
      {error && <p className="text-sm text-danger">{error}</p>}
    </fieldset>
  );
}
