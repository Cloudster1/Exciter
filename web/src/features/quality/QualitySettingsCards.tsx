import { useState, type FormEvent } from "react";
import { useTypes } from "@/api/attributes";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import {
  RULE_IDS,
  SEVERITIES,
  WORD_RULES,
  useQualityRules,
  useQualitySettings,
  useSaveQualitySettings,
  type QualityRule,
  type QualityRuleSetting,
  type QualitySettings,
  type QualityWords,
  type Severity,
  type WordRule,
} from "@/api/quality";
import { Button, Card, Checkbox, ErrorBanner, Field, SectionTitle, SelectInput, Skeleton } from "@/components/ui";
import { QUALITY_MAX_WORDS, QUALITY_WORD_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

const listOf = (text: string) =>
  text
    .split(",")
    .map((w) => w.trim())
    .filter(Boolean);

/** Every rule as the project has it, in the settings' order; a rule the project never set starts at its defaults. */
export function ruleSettings(settings: QualitySettings, catalogue: QualityRule[]): QualityRuleSetting[] {
  return RULE_IDS.map((rule) => {
    const set = settings.rules.find((each) => each.rule === rule);
    const known = catalogue.find((each) => each.id === rule);
    return set ?? { rule, enabled: known?.defaultEnabled ?? true, severity: known?.defaultSeverity ?? "warning", typeIds: [] };
  });
}

/** Every word list, an empty one for a rule the project never gave words. */
function wordLists(settings: QualitySettings): Record<WordRule, QualityWords> {
  return Object.fromEntries(WORD_RULES.map((rule) => [rule, settings.words[rule] ?? { add: [], except: [] }])) as Record<WordRule, QualityWords>;
}

/** The writing rules a project checks (#207): each on or off, its severity and the types it applies to. */
export function QualityRulesCard({ project }: { project: Project }) {
  const { data: settings, error, refetch } = useQualitySettings(project.key);
  const { data: catalogue } = useQualityRules();
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!settings || !catalogue) return <Skeleton />;
  return <RulesForm key={settings.updatedAt ?? "default"} project={project} settings={settings} catalogue={catalogue} />;
}

function RulesForm({ project, settings, catalogue }: { project: Project; settings: QualitySettings; catalogue: QualityRule[] }) {
  const [rules, setRules] = useState(() => ruleSettings(settings, catalogue));
  const [notice, setNotice] = useState("");
  const { data: types } = useTypes(project.key);
  const save = useSaveQualitySettings(project.key);
  const may = project.can.administer && !project.archivedAt;
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const l = t.quality.rulesCard;
  const change = (rule: string, next: Partial<QualityRuleSetting>) => setRules(rules.map((each) => (each.rule === rule ? { ...each, ...next } : each)));
  function submit(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    save.mutate({ rules, words: wordLists(settings) }, { onSuccess: () => setNotice(l.saved) });
  }
  const offered = (types ?? []).filter((type) => !type.retired);
  return (
    <Card className="space-y-3 p-4" data-quality-rules>
      <SectionTitle>{l.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{l.intro}</p>
      {!may && <p className="text-sm text-ink-muted">{l.readOnly}</p>}
      <form onSubmit={submit} className="space-y-3" aria-label={l.title}>
        <ul className="flex flex-col divide-y divide-border">
          {rules.map((setting) => {
            const words = t.quality.rules[setting.rule];
            return (
              <li key={setting.rule} className="space-y-2 py-3" data-quality-rule={setting.rule}>
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <Checkbox
                      label={<span className="font-medium">{words.name}</span>}
                      aria-label={l.on(words.name)}
                      checked={setting.enabled}
                      disabled={!may}
                      onChange={(event) => change(setting.rule, { enabled: event.target.checked })}
                    />
                    <p className="mt-0.5 text-xs text-ink-muted">{words.about}</p>
                  </div>
                  <SelectInput
                    aria-label={l.severityOf(words.name)}
                    value={setting.severity}
                    disabled={!may}
                    controlSize="sm"
                    onChange={(event) => change(setting.rule, { severity: event.target.value as Severity })}
                  >
                    {SEVERITIES.map((severity) => (
                      <option key={severity} value={severity}>
                        {t.quality.severities[severity]}
                      </option>
                    ))}
                  </SelectInput>
                </div>
                {offered.length > 0 && (
                  <fieldset className="space-y-1">
                    <legend className="text-xs font-medium text-ink-muted">
                      <span aria-hidden="true">{l.types}</span>
                      <span className="sr-only">{l.typesOf(words.name)}</span>
                    </legend>
                    <div className="flex flex-wrap gap-x-4 gap-y-1">
                      {offered.map((type) => (
                        <Checkbox
                          key={type.id}
                          label={type.name}
                          checked={setting.typeIds.includes(type.id)}
                          disabled={!may}
                          onChange={(event) =>
                            change(setting.rule, {
                              typeIds: event.target.checked ? [...setting.typeIds, type.id] : setting.typeIds.filter((id) => id !== type.id),
                            })
                          }
                        />
                      ))}
                    </div>
                    <p className="text-xs text-ink-subtle">{l.everyType}</p>
                  </fieldset>
                )}
              </li>
            );
          })}
        </ul>
        {may && (
          <Button type="submit" loading={save.isPending} data-action="save-quality-rules">
            {l.save}
          </Button>
        )}
      </form>
      {save.error && <ErrorBanner>{Object.values(fields)[0] ?? save.error.message}</ErrorBanner>}
      <p role="status" className="text-sm text-ink-muted empty:hidden">
        {notice}
      </p>
    </Card>
  );
}

/** The words a project adds to the word rules and the built-in ones it excepts (#207). */
export function QualityWordsCard({ project }: { project: Project }) {
  const { data: settings, error, refetch } = useQualitySettings(project.key);
  const { data: catalogue } = useQualityRules();
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!settings || !catalogue) return <Skeleton />;
  return <WordsForm key={settings.updatedAt ?? "default"} project={project} settings={settings} catalogue={catalogue} />;
}

function WordsForm({ project, settings, catalogue }: { project: Project; settings: QualitySettings; catalogue: QualityRule[] }) {
  const [lists, setLists] = useState(() =>
    Object.fromEntries(Object.entries(wordLists(settings)).map(([rule, words]) => [rule, { add: words.add.join(", "), except: words.except.join(", ") }])),
  );
  const [notice, setNotice] = useState("");
  const save = useSaveQualitySettings(project.key);
  const may = project.can.administer && !project.archivedAt;
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const l = t.quality.wordsCard;
  const most = QUALITY_MAX_WORDS * (QUALITY_WORD_MAX_LENGTH + 2);
  function submit(event: FormEvent) {
    event.preventDefault();
    setNotice("");
    const words = Object.fromEntries(WORD_RULES.map((rule) => [rule, { add: listOf(lists[rule]?.add ?? ""), except: listOf(lists[rule]?.except ?? "") }]));
    save.mutate({ rules: ruleSettings(settings, catalogue), words }, { onSuccess: () => setNotice(l.saved) });
  }
  // A refusal names the list as words.<rule>.add or words.<rule>.except.
  const problem = (rule: string, list: "add" | "except") => fields[`words.${rule}.${list}`] ?? fields[`words.${rule}`];
  return (
    <Card className="space-y-3 p-4" data-quality-words>
      <SectionTitle>{l.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{l.intro}</p>
      {!may && <p className="text-sm text-ink-muted">{l.readOnly}</p>}
      <form onSubmit={submit} className="space-y-3" aria-label={l.title}>
        {WORD_RULES.map((rule) => {
          const name = t.quality.rules[rule].name;
          return (
            <div key={rule} className="flex flex-wrap gap-3" data-quality-words-rule={rule}>
              {(["add", "except"] as const).map((list) => (
                <div key={list} className="min-w-56 flex-1">
                  <Field
                    id={`quality-words-${rule}-${list}`}
                    label={l[list](name)}
                    hint={l.hint}
                    value={lists[rule]?.[list] ?? ""}
                    maxLength={most}
                    disabled={!may}
                    error={problem(rule, list)}
                    onChange={(event) =>
                      setLists({ ...lists, [rule]: { add: lists[rule]?.add ?? "", except: lists[rule]?.except ?? "", [list]: event.target.value } })
                    }
                  />
                </div>
              ))}
            </div>
          );
        })}
        {may && (
          <Button type="submit" loading={save.isPending} data-action="save-quality-words">
            {l.save}
          </Button>
        )}
      </form>
      {save.error && Object.keys(fields).length === 0 && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <p role="status" className="text-sm text-ink-muted empty:hidden">
        {notice}
      </p>
    </Card>
  );
}
