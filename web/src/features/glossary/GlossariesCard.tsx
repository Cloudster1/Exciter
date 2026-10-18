import { useState } from "react";
import { ApiError } from "@/api/client";
import { useGlossaries, useSaveGlossaries, type Glossaries } from "@/api/glossary";
import { useProjects, type Project } from "@/api/projects";
import { Button, Card, ErrorBanner, IconButton, SectionTitle, SelectInput, Skeleton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { GLOSSARY_MAX_LIBRARIES } from "@/config";
import { t } from "@/i18n";

/** The library projects whose glossaries mark a project's requirements (#210), in the order they win a word. */
export function GlossariesCard({ project }: { project: Project }) {
  const { data: glossaries, error, refetch } = useGlossaries(project.key);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!glossaries) return <Skeleton />;
  // Not keyed by what was saved: the form already holds it, and its status line must outlive the save to be heard.
  return <GlossariesForm project={project} glossaries={glossaries} />;
}

function GlossariesForm({ project, glossaries }: { project: Project; glossaries: Glossaries }) {
  const l = t.glossary.settings;
  const [chosen, setChosen] = useState(() => [...glossaries.libraries].sort((a, b) => a.position - b.position).map((each) => each.projectKey));
  const [adding, setAdding] = useState("");
  const [notice, setNotice] = useState("");
  const may = project.can.administer && !project.archivedAt;
  const { data: projects } = useProjects(false, may);
  const save = useSaveGlossaries(project.key);
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const libraries = (projects ?? []).filter((each) => each.settings.library && each.key !== project.key);
  const offered = libraries.filter((each) => !chosen.includes(each.key));
  const nameOf = (key: string) => libraries.find((each) => each.key === key)?.name ?? glossaries.libraries.find((each) => each.projectKey === key)?.name ?? key;
  const termsOf = (key: string) => glossaries.libraries.find((each) => each.projectKey === key)?.terms;
  const full = chosen.length >= GLOSSARY_MAX_LIBRARIES;

  function change(next: string[]) {
    setNotice("");
    setChosen(next);
  }
  function move(index: number, by: number) {
    const next = [...chosen];
    const [taken] = next.splice(index, 1);
    next.splice(index + by, 0, taken!);
    change(next);
  }

  return (
    <Card className="space-y-3 p-4" data-glossaries-card>
      <section aria-labelledby="glossaries-title" className="space-y-3">
        <SectionTitle id="glossaries-title">{l.title}</SectionTitle>
        <p className="text-sm text-ink-muted">{l.intro}</p>
        {!may && <p className="text-sm text-ink-muted">{l.readOnly}</p>}
        {chosen.length === 0 ? (
          <p className="text-sm text-ink-subtle">{l.none}</p>
        ) : (
          <ol className="flex flex-col divide-y divide-border" aria-label={l.list}>
            {chosen.map((key, i) => {
              const name = nameOf(key);
              const terms = termsOf(key);
              const problem = fields[`libraries[${i}]`];
              return (
                <li key={key} className="space-y-1 py-2" data-glossary-library={key}>
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="w-5 text-sm text-ink-subtle tabular-nums">{i + 1}.</span>
                    <span className="min-w-0 flex-1 text-sm text-ink">
                      {name} <span className="font-mono text-xs text-ink-muted">{key}</span>
                      {terms !== undefined && <span className="ml-2 text-xs text-ink-subtle">{l.terms(terms)}</span>}
                    </span>
                    {may && (
                      <div className="flex gap-0.5">
                        <IconButton icon={<Icon.ChevronUp />} label={l.up(name)} size="sm" disabled={i === 0} onClick={() => move(i, -1)} />
                        <IconButton icon={<Icon.ChevronDown />} label={l.down(name)} size="sm" disabled={i === chosen.length - 1} onClick={() => move(i, 1)} />
                        <IconButton icon={<Icon.X />} label={l.remove(name)} size="sm" onClick={() => change(chosen.filter((each) => each !== key))} />
                      </div>
                    )}
                  </div>
                  {problem && <p className="text-sm text-danger">{problem}</p>}
                </li>
              );
            })}
          </ol>
        )}
        {may && (
          <div className="flex flex-wrap items-end gap-2">
            {libraries.length === 0 && projects ? (
              <p className="text-sm text-ink-subtle">{l.noLibraries}</p>
            ) : (
              <>
                <div className="space-y-1">
                  <label htmlFor="glossaries-add" className="block text-sm font-medium text-ink-muted">
                    {l.add}
                  </label>
                  <SelectInput id="glossaries-add" value={adding} disabled={full || offered.length === 0} onChange={(event) => setAdding(event.target.value)}>
                    <option value="">{l.choose}</option>
                    {offered.map((each) => (
                      <option key={each.key} value={each.key}>
                        {each.name} ({each.key})
                      </option>
                    ))}
                  </SelectInput>
                </div>
                <Button
                  variant="secondary"
                  disabled={!adding || full}
                  onClick={() => {
                    change([...chosen, adding]);
                    setAdding("");
                  }}
                  data-action="add-glossary"
                >
                  {l.addButton}
                </Button>
              </>
            )}
          </div>
        )}
        {full && may && <p className="text-xs text-ink-subtle">{l.tooMany(GLOSSARY_MAX_LIBRARIES)}</p>}
        {may && (
          <Button loading={save.isPending} onClick={() => save.mutate(chosen, { onSuccess: () => setNotice(l.saved) })} data-action="save-glossaries">
            {l.save}
          </Button>
        )}
        {save.error && !Object.keys(fields).some((name) => name.startsWith("libraries[")) && (
          <ErrorBanner>{fields.libraries ?? save.error.message}</ErrorBanner>
        )}
        <p role="status" className="text-sm text-ink-muted empty:hidden">
          {notice}
        </p>
      </section>
    </Card>
  );
}
