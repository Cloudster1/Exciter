import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useBaselineActions, useBaselines } from "@/api/baselines";
import { ApiError } from "@/api/client";
import type { Project } from "@/api/projects";
import { useTree } from "@/api/tree";
import { Button, Card, EmptyState, ErrorBanner, Field, PageHeader, Select, Skeleton, Tag, useToast, Page } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BASELINE_DESCRIPTION_MAX_LENGTH, BASELINE_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { signingWords } from "./BaselineSigning";
import { localDateFormat } from "@/lib/format";

// A project's baselines (#37): listed newest first, and taken by its administrators.

const takenOn = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** Whether the reader may take, change and delete the project's baselines. */
export function keepsBaselines(project: Project): boolean {
  return project.can.administer && !project.archivedAt;
}

/** A project's baselines, newest first, with a way for its administrators to take one. */
export function BaselineList({ project }: { project: Project }) {
  const { data, error, refetch } = useBaselines(project.key);
  const [taking, setTaking] = useState(false);
  const toast = useToast();
  const mayKeep = keepsBaselines(project);
  return (
    <Page width="narrow" className="space-y-4" data-baselines={project.key}>
      <PageHeader
        title={t.baseline.title}
        meta={t.baseline.intro}
        actions={
          mayKeep &&
          !taking && (
            <Button icon={<Icon.Plus />} onClick={() => setTaking(true)} data-action="take-baseline">
              {t.baseline.take}
            </Button>
          )
        }
      />
      {!mayKeep && <p className="text-sm text-ink-muted">{t.baseline.readOnly}</p>}
      {taking && (
        <TakeBaseline
          project={project}
          onDone={(name) => {
            setTaking(false);
            if (name) toast.success(t.baseline.taken(name));
          }}
        />
      )}
      {error ? (
        <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>
      ) : !data ? (
        <Skeleton />
      ) : data.length === 0 ? (
        <EmptyState icon={<Icon.Archive />} title={t.baseline.none} />
      ) : (
        <ul className="flex flex-col divide-y divide-border" aria-label={t.baseline.title}>
          {data.map((b) => (
            <li key={b.id} className="flex flex-col gap-0.5 py-3" data-baseline={b.name}>
              <span className="flex flex-wrap items-center gap-2">
                <Link
                  to="/projects/$projectKey/baselines/$baselineId"
                  params={{ projectKey: project.key, baselineId: b.id }}
                  className="text-sm font-medium text-ink hover:text-accent hover:underline"
                >
                  {b.name}
                </Link>
                {b.signing && b.signing.signatures > 0 ? (
                  <Tag title={signingWords(b)} data-baseline-signed={b.signing.signed ? "signed" : "signing"}>
                    <Icon.Lock className="mr-1 size-3" aria-hidden="true" />
                    {b.signing.signed ? t.control.signedTag : t.control.signing(b.signing.signatures, b.signing.required)}
                  </Tag>
                ) : (
                  b.locked && (
                    <Tag title={t.baseline.lockedHint}>
                      <Icon.Lock className="mr-1 size-3" aria-hidden="true" />
                      {t.baseline.locked}
                    </Tag>
                  )
                )}
              </span>
              <span className="flex flex-wrap items-center gap-x-1.5 text-xs text-ink-subtle">
                <span>{b.root ? t.baseline.ofSection(b.root.key, b.root.title) : t.baseline.wholeProject}</span>
                <span aria-hidden="true">·</span>
                <span>{t.baseline.items(b.items)}</span>
                <span aria-hidden="true">·</span>
                <span>{t.baseline.takenBy(b.createdByName, takenOn.format(new Date(b.createdAt)))}</span>
              </span>
              {b.description && <span className="text-sm text-ink-muted">{b.description}</span>}
            </li>
          ))}
        </ul>
      )}
    </Page>
  );
}

function TakeBaseline({ project, onDone }: { project: Project; onDone: (name?: string) => void }) {
  const { take } = useBaselineActions(project.key);
  const { data: nodes } = useTree(project.key);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [rootKey, setRootKey] = useState("");
  const fields = take.error instanceof ApiError ? take.error.fields : {};
  const sections = (nodes ?? []).filter((node) => node.kind === "section");

  function submit(event: FormEvent) {
    event.preventDefault();
    take.mutate({ name: name.trim(), description: description.trim() || undefined, rootKey: rootKey || undefined }, { onSuccess: (b) => onDone(b.name) });
  }

  return (
    <Card className="p-4">
      <form onSubmit={submit} className="space-y-3" aria-label={t.baseline.takeTitle} data-take-baseline="">
        <Field
          label={t.baseline.name}
          value={name}
          placeholder={t.baseline.namePlaceholder}
          maxLength={BASELINE_NAME_MAX_LENGTH}
          error={fields.name}
          onChange={(event) => setName(event.target.value)}
          required
        />
        <Field
          label={t.baseline.description}
          rows={2}
          value={description}
          maxLength={BASELINE_DESCRIPTION_MAX_LENGTH}
          error={fields.description}
          onChange={(event) => setDescription(event.target.value)}
        />
        <Select label={t.baseline.scope} value={rootKey} onChange={(event) => setRootKey(event.target.value)} error={fields.rootKey}>
          <option value="">{t.baseline.wholeProject}</option>
          {sections.map((node) => (
            <option key={node.id} value={node.key}>
              {`${node.outline} ${node.title}`}
            </option>
          ))}
        </Select>
        {take.error && !fields.name && !fields.description && !fields.rootKey && <ErrorBanner>{take.error.message}</ErrorBanner>}
        <div className="flex gap-2">
          <Button type="submit" loading={take.isPending} disabled={!name.trim()} data-action="send-baseline">
            {t.baseline.takeSend}
          </Button>
          <Button type="button" variant="ghost" onClick={() => onDone()}>
            {t.baseline.cancel}
          </Button>
        </div>
      </form>
    </Card>
  );
}
