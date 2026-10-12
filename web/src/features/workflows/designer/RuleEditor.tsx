import { useState } from "react";
import type { RuleKind, RuleType } from "@/api/workflows";
import { Icon } from "@/components/icons";
import { Button, Checkbox, Field, IconButton, Select } from "@/components/ui";
import { t } from "@/i18n";
import { describeRule, optionLabel, roleWord, ruleLabel, type DraftRule } from "./graph";

const KINDS: RuleKind[] = ["condition", "validator", "postfunction"];

/** The rules on one transition, grouped the way the engine asks them: whether the move is offered, whether
 * what comes with it passes, and what happens afterwards; a rule is picked from the server's list, never typed. */
export function RuleEditor({ rules, catalogue, onChange }: { rules: DraftRule[]; catalogue: RuleType[]; onChange: (rules: DraftRule[]) => void }) {
  const [picked, setPicked] = useState("");
  const [config, setConfig] = useState<Record<string, unknown>>({});
  const chosen = catalogue.find((type) => type.type === picked);
  const words = t.workflows.ruleKinds;

  const complete =
    chosen !== undefined &&
    chosen.options.every((option) => {
      if (!option.required) return true;
      const value = config[option.name];
      return Array.isArray(value) ? value.length > 0 : typeof value === "string" && value.trim() !== "";
    });

  function add() {
    if (!chosen) return;
    onChange([...rules, { kind: chosen.kind, type: chosen.type, config }]);
    setPicked("");
    setConfig({});
  }

  return (
    <div className="space-y-3" data-rule-editor>
      {KINDS.map((kind) => {
        const ofKind = rules.map((rule, index) => ({ rule, index })).filter(({ rule }) => rule.kind === kind);
        return (
          <div key={kind}>
            <p className="text-sm font-medium text-ink">{words[kind].title}</p>
            <p className="text-xs text-ink-subtle">{words[kind].hint}</p>
            {ofKind.length > 0 && (
              <ul className="mt-1.5 space-y-1">
                {ofKind.map(({ rule, index }) => {
                  const text = describeRule(rule, catalogue);
                  return (
                    <li
                      key={`${rule.type}-${index}`}
                      data-rule={rule.type}
                      className="flex items-center justify-between gap-2 rounded-control bg-surface-raised px-2 py-1 text-sm text-ink"
                    >
                      <span className="min-w-0 truncate" title={text}>
                        {text}
                      </span>
                      <IconButton
                        size="xs"
                        icon={<Icon.X />}
                        label={t.workflows.removeRule(text)}
                        onClick={() => onChange(rules.filter((_, at) => at !== index))}
                      />
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        );
      })}

      <div className="space-y-2 border-t border-border pt-3">
        <Select
          label={t.workflows.addRule}
          id="field-rule"
          value={picked}
          onChange={(event) => {
            setPicked(event.target.value);
            setConfig({});
          }}
        >
          <option value="">{t.workflows.chooseRule}</option>
          {KINDS.map((kind) => (
            <optgroup key={kind} label={words[kind].title}>
              {catalogue
                .filter((type) => type.kind === kind)
                .map((type) => (
                  <option key={type.type} value={type.type}>
                    {ruleLabel(type.type, catalogue)}
                  </option>
                ))}
            </optgroup>
          ))}
        </Select>
        {chosen && <p className="text-xs text-ink-muted">{(t.workflows.ruleHints as Record<string, string | undefined>)[chosen.type] ?? chosen.description}</p>}

        {chosen?.options.map((option) =>
          option.kind === "roles" ? (
            <fieldset key={option.name} className="space-y-1">
              <legend className="text-sm font-medium text-ink-muted">{optionLabel(option.name, option.label)}</legend>
              {option.choices.map((choice) => {
                const current = Array.isArray(config[option.name]) ? (config[option.name] as string[]) : [];
                return (
                  <Checkbox
                    key={choice}
                    label={roleWord(choice)}
                    className="flex"
                    checked={current.includes(choice)}
                    onChange={(event) =>
                      setConfig({ ...config, [option.name]: event.target.checked ? [...current, choice] : current.filter((each) => each !== choice) })
                    }
                  />
                );
              })}
            </fieldset>
          ) : (
            <Field
              key={option.name}
              label={optionLabel(option.name, option.label)}
              id={`field-rule-${option.name}`}
              value={typeof config[option.name] === "string" ? (config[option.name] as string) : ""}
              onChange={(event) => setConfig({ ...config, [option.name]: event.target.value })}
            />
          ),
        )}

        <Button size="sm" variant="secondary" disabled={!complete} onClick={add} data-action="add-rule">
          {t.workflows.addRuleButton}
        </Button>
      </div>
    </div>
  );
}
