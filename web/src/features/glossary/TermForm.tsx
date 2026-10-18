import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { TERM_STATUSES, type Term, type TermStatus } from "@/api/glossary";
import { Button, ErrorBanner, Field, Labelled, Select } from "@/components/ui";
import { TERM_COMMENT_MAX_LENGTH, TERM_MAX_AVOID, TERM_MAX_SYNONYMS, TERM_NAME_MAX_LENGTH } from "@/config";
import { Editor } from "@/features/editor/Editor";
import { emptyDoc, type Doc } from "@/features/editor/schema";
import { t } from "@/i18n";
import { WordsInput } from "./WordsInput";

/** What a term says, as the form sends it. */
export interface TermValues {
  name: string;
  definition: Doc;
  synonyms: string[];
  avoid: string[];
  status: TermStatus;
  comment: string;
}

/** A term written or changed: its name, its definition in the shared editor, its synonyms, the words not to use
 * instead and its status; a change also takes a comment. */
export function TermForm({
  term,
  submitLabel,
  pending,
  error,
  onSubmit,
  onCancel,
}: {
  term?: Term;
  submitLabel: string;
  pending: boolean;
  error: Error | null;
  onSubmit: (values: TermValues) => void;
  onCancel: () => void;
}) {
  const l = t.glossary.form;
  const [name, setName] = useState(term?.name ?? "");
  const [definition, setDefinition] = useState<Doc | null>((term?.definition as Doc | undefined) ?? null);
  const [synonyms, setSynonyms] = useState<string[]>(term?.synonyms ?? []);
  const [avoid, setAvoid] = useState<string[]>(term?.avoid ?? []);
  const [status, setStatus] = useState<TermStatus>(term?.status ?? "proposed");
  const [comment, setComment] = useState("");
  const [missing, setMissing] = useState(false);
  const fields = error instanceof ApiError ? error.fields : {};
  const general = error && Object.keys(fields).length === 0;
  const prefix = term ? `term-${term.id}` : "new-term";

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) {
      setMissing(true);
      return;
    }
    onSubmit({ name: name.trim(), definition: definition ?? emptyDoc, synonyms, avoid, status, comment: comment.trim() });
  }

  return (
    <form onSubmit={submit} className="space-y-4" noValidate data-term-form>
      {general && <ErrorBanner>{error.message}</ErrorBanner>}
      <Field
        id={`${prefix}-name`}
        label={l.name}
        hint={l.nameHint}
        value={name}
        maxLength={TERM_NAME_MAX_LENGTH}
        error={fields.name ?? (missing && !name.trim() ? l.nameMissing : undefined)}
        onChange={(event) => setName(event.target.value)}
        required
        autoFocus
      />
      <Labelled id={`${prefix}-definition`} label={l.definition} error={fields.definition}>
        <Editor id={`${prefix}-definition`} value={definition} onChange={setDefinition} aria-label={l.definition} />
      </Labelled>
      <WordsInput
        label={l.synonyms}
        hint={l.synonymsHint}
        words={synonyms}
        onChange={setSynonyms}
        most={TERM_MAX_SYNONYMS}
        error={fields.synonyms}
        data-term-synonyms=""
      />
      <WordsInput label={l.avoid} hint={l.avoidHint} words={avoid} onChange={setAvoid} most={TERM_MAX_AVOID} error={fields.avoid} data-term-avoid="" />
      <Select id={`${prefix}-status`} label={l.status} value={status} error={fields.status} onChange={(event) => setStatus(event.target.value as TermStatus)}>
        {TERM_STATUSES.map((each) => (
          <option key={each} value={each}>
            {t.glossary.statuses[each]}
          </option>
        ))}
      </Select>
      {term && (
        <Field
          id={`${prefix}-comment`}
          label={l.comment}
          value={comment}
          maxLength={TERM_COMMENT_MAX_LENGTH}
          error={fields.comment}
          onChange={(event) => setComment(event.target.value)}
        />
      )}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onCancel}>
          {l.cancel}
        </Button>
        <Button type="submit" loading={pending} data-action="save-term">
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}
