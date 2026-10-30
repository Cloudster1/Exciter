import { useState, type FormEvent } from "react";
import { useCodeTagWords, useSaveCodeTagWords } from "@/api/code";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { Button, Card, ErrorBanner, Field, SectionTitle, Skeleton, useToast } from "@/components/ui";
import { CODE_TAG_MAX_WORDS, CODE_TAG_WORD_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

const listOf = (text: string) =>
  text
    .split(",")
    .map((w) => w.trim())
    .filter(Boolean);

/** The words a project's code tags use for implements and verifies (#201); its administrators set them. */
export function CodeTagWordsCard({ project }: { project: Project }) {
  const { data, error, refetch } = useCodeTagWords(project.key);
  // Kept above the form, which starts again from the words it saved, so the word that it saved outlives it.
  const save = useSaveCodeTagWords(project.key);
  if (error) return <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>;
  if (!data) return <Skeleton />;
  return <WordsForm key={data.updatedAt ?? "default"} project={project} implementsWords={data.implements} verifiesWords={data.verifies} save={save} />;
}

interface FormProps {
  project: Project;
  implementsWords: string[];
  verifiesWords: string[];
  save: ReturnType<typeof useSaveCodeTagWords>;
}

function WordsForm({ project, implementsWords, verifiesWords, save }: FormProps) {
  const toast = useToast();
  const [implementing, setImplementing] = useState(implementsWords.join(", "));
  const [verifying, setVerifying] = useState(verifiesWords.join(", "));
  const may = project.can.administer && !project.archivedAt;
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const l = t.evidence.words;
  const most = CODE_TAG_MAX_WORDS * (CODE_TAG_WORD_MAX_LENGTH + 2);
  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate({ implements: listOf(implementing), verifies: listOf(verifying) }, { onSuccess: () => toast.success(l.saved) });
  }
  return (
    <Card className="space-y-3 p-4" data-code-tag-words>
      <SectionTitle>{l.title}</SectionTitle>
      <p className="text-sm text-ink-muted">{l.intro}</p>
      {!may && <p className="text-sm text-ink-muted">{l.readOnly}</p>}
      <form onSubmit={submit} className="space-y-3" aria-label={l.title}>
        <div className="flex flex-wrap gap-3">
          <div className="min-w-56 flex-1">
            <Field
              id="code-words-implements"
              label={l.implements}
              hint={l.hint}
              value={implementing}
              maxLength={most}
              disabled={!may}
              error={fields.implements}
              onChange={(event) => setImplementing(event.target.value)}
            />
          </div>
          <div className="min-w-56 flex-1">
            <Field
              id="code-words-verifies"
              label={l.verifies}
              hint={l.hint}
              value={verifying}
              maxLength={most}
              disabled={!may}
              error={fields.verifies}
              onChange={(event) => setVerifying(event.target.value)}
            />
          </div>
        </div>
        {may && (
          <Button type="submit" loading={save.isPending} data-action="save-code-tag-words">
            {l.save}
          </Button>
        )}
      </form>
      {save.error && Object.keys(fields).length === 0 && <ErrorBanner>{save.error.message}</ErrorBanner>}
    </Card>
  );
}
