import { useTypes, type Attribute, type RequirementType } from "@/api/attributes";
import type { MatrixColumns } from "@/api/coverage";
import { useProjectLinkTypes } from "@/api/links";
import { useProjects, type Project } from "@/api/projects";
import type { Status } from "@/api/workflows";
import { Card, Chip, Segmented, Select } from "@/components/ui";
import { GridFilters } from "@/features/grid/GridFilters";
import { QueryInput } from "@/features/query/QueryInput";
import { t } from "@/i18n";
import { hasColumnFilter, ISSUE_RELATIONS, MATRIX_DIRECTIONS, MATRIX_GAPS, PAGE_RELATIONS, type MatrixGaps, type MatrixSetup } from "./address";

/** What a matrix lays out: the grid's narrowing for its rows, then its columns and the links that count. */
export function MatrixToolbar({
  project,
  setup,
  types,
  statuses,
  attributes,
  queryError,
  columnQueryError,
  onSetup,
}: {
  project: Project;
  setup: MatrixSetup;
  types: RequirementType[];
  statuses: Status[];
  attributes: Attribute[];
  queryError?: unknown;
  /** A refusal of the column filter's query, shown under it rather than under the rows'. */
  columnQueryError?: unknown;
  onSetup: (next: MatrixSetup) => void;
}) {
  const { data: linkTypes = [] } = useProjectLinkTypes(project.key);
  const columnProject = setup.columnProject || project.key;
  const { data: columnTypes = [] } = useTypes(columnProject);
  const filtered = hasColumnFilter(setup);
  const { data: projects = [] } = useProjects();
  const set = (patch: Partial<MatrixSetup>) => onSetup({ ...setup, ...patch });
  return (
    <div className="space-y-3" data-matrix-toolbar>
      <section aria-label={t.matrix.rows}>
        <GridFilters
          project={project}
          filter={setup.filter}
          types={types}
          statuses={statuses}
          attributes={attributes}
          queryError={queryError}
          onFilter={(filter) => set({ filter })}
        />
      </section>
      <Card className="flex flex-wrap items-start gap-x-6 gap-y-3 p-3">
        <Segmented<MatrixColumns>
          label={t.coverage.columns}
          value={setup.columns}
          onChange={(columns) => set({ columns, relations: [] })}
          size="sm"
          options={[
            { value: "requirements", label: t.coverage.columnRequirements, attrs: { "data-columns": "requirements" } },
            { value: "issues", label: t.coverage.columnIssues, attrs: { "data-columns": "issues" } },
            { value: "pages", label: t.matrix.columnPagesKind, attrs: { "data-columns": "pages" } },
          ]}
        />
        {setup.columns === "requirements" ? (
          <>
            <div className="min-w-44">
              <Select
                label={t.matrix.columnProject}
                value={setup.columnProject}
                onChange={(event) => set({ columnProject: event.target.value })}
                controlSize="sm"
                data-matrix-column-project
              >
                <option value="">{t.matrix.thisProject}</option>
                {projects
                  .filter((each) => each.key !== project.key)
                  .map((each) => (
                    <option key={each.id} value={each.key}>
                      {each.name}
                    </option>
                  ))}
              </Select>
            </div>
            <Segmented
              label={t.matrix.direction}
              value={setup.direction}
              onChange={(direction) => set({ direction })}
              size="sm"
              options={MATRIX_DIRECTIONS.map((d) => ({ value: d, label: t.matrix.directions[d], attrs: { "data-direction": d } }))}
            />
            <Choices
              label={t.matrix.linkTypes}
              hint={t.matrix.everyLinkType}
              name="link-type"
              options={linkTypes.filter((type) => type.enabled && !type.retired).map((type) => ({ value: type.key, label: type.name }))}
              chosen={setup.linkTypes}
              onChange={(linkTypes) => set({ linkTypes })}
            />
          </>
        ) : (
          <Choices
            label={t.matrix.relations}
            hint={t.matrix.everyRelation}
            name="relation"
            options={
              setup.columns === "pages"
                ? PAGE_RELATIONS.map((r) => ({ value: r, label: t.stator.links.headings[r] }))
                : ISSUE_RELATIONS.map((r) => ({ value: r, label: t.matrix.relationNames[r] }))
            }
            chosen={setup.relations}
            onChange={(relations) => set({ relations })}
          />
        )}
        <div className="min-w-52">
          <Select
            label={t.matrix.gaps}
            value={setup.gaps}
            onChange={(event) => set({ gaps: event.target.value as MatrixGaps })}
            hint={filtered ? undefined : t.matrix.gapsNeedFilter}
            controlSize="sm"
            data-matrix-gaps
          >
            {MATRIX_GAPS.map((gaps) => (
              <option key={gaps} value={gaps} disabled={(gaps === "columns" || gaps === "both") && !filtered}>
                {t.matrix.gapNames[gaps]}
              </option>
            ))}
          </Select>
        </div>
      </Card>
      {setup.columns === "requirements" && (
        <Card className="space-y-2 p-3" data-matrix-column-filter>
          <h2 className="text-xs font-medium text-ink-muted">{t.matrix.columnFilter}</h2>
          <QueryInput
            key={columnProject}
            projectKey={columnProject}
            value={setup.columnQuery}
            error={columnQueryError}
            name="matrix-columns"
            label={t.matrix.columnQuery}
            region={t.matrix.columnFilter}
            onSubmit={(columnQuery) => set({ columnQuery })}
          />
          <Choices
            label={t.matrix.columnTypes}
            hint={t.matrix.noColumnFilter}
            name="column-type"
            options={columnTypes.filter((type) => !type.retired).map((type) => ({ value: type.id, label: type.name }))}
            chosen={setup.columnTypes}
            onChange={(chosen) => set({ columnTypes: chosen })}
            quiet={filtered}
          />
        </Card>
      )}
    </div>
  );
}

/** Several of a few things picked by pressing them; none picked counts every one, as the hint says. */
function Choices({
  label,
  hint,
  name,
  options,
  chosen,
  onChange,
  quiet = false,
}: {
  label: string;
  hint: string;
  name: string;
  options: Array<{ value: string; label: string }>;
  chosen: string[];
  onChange: (next: string[]) => void;
  /** Keeps the hint back while something else already says what counts. */
  quiet?: boolean;
}) {
  const toggle = (value: string) => onChange(chosen.includes(value) ? chosen.filter((each) => each !== value) : [...chosen, value]);
  return (
    <div role="group" aria-label={label} className="min-w-64 flex-1 space-y-1">
      <span className="block text-xs font-medium text-ink-muted">{label}</span>
      <div className="flex flex-wrap gap-1.5">
        {options.map((option) => (
          <Chip key={option.value} pressed={chosen.includes(option.value)} onClick={() => toggle(option.value)} data-choice={`${name}:${option.value}`}>
            {option.label}
          </Chip>
        ))}
      </div>
      {chosen.length === 0 && !quiet && <p className="text-xs text-ink-subtle">{hint}</p>}
    </div>
  );
}
