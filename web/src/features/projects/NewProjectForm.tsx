import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { administers, useMe } from "@/api/auth";
import { ApiError } from "@/api/client";
import { suggestKey, useCreateProject, useKeyCheck } from "@/api/projects";
import { Button, ErrorBanner, Field, PageHeader } from "@/components/ui";
import { KEY_SUGGEST_DEBOUNCE_MS, PROJECTS_PATH, PROJECT_DESCRIPTION_MAX_LENGTH, PROJECT_KEY_MAX_LENGTH, PROJECT_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";

/** Makes a project; the key follows the name, as a free key the server suggests, until somebody types one of their own. */
export function NewProjectForm() {
  const navigate = useNavigate();
  const { data: me } = useMe();
  const mayCreate = administers(me?.organization?.role);
  const create = useCreateProject();
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [description, setDescription] = useState("");
  const settledName = useDebounced(name, KEY_SUGGEST_DEBOUNCE_MS);
  const settledKey = useDebounced(key, KEY_SUGGEST_DEBOUNCE_MS);
  const check = useKeyCheck(keyTouched ? { key: settledKey } : { name: settledName });
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const suggested = !keyTouched && settledName === name ? check.data?.suggestion : undefined;
  const shownKey = keyTouched ? key : suggested || suggestKey(name, PROJECT_KEY_MAX_LENGTH);
  const typedCheck = keyTouched && check.data?.key === key.trim().toUpperCase() && check.data.valid ? check.data : undefined;
  const keyHint = typedCheck ? (typedCheck.available ? t.projects.keyFree(typedCheck.key) : t.projects.keyTaken(typedCheck.key)) : t.projects.keyHint;

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate({ name, key: shownKey, description }, { onSuccess: (made) => void navigate({ to: "/p/$projectKey", params: { projectKey: made.key } }) });
  }

  return (
    <div className="mx-auto max-w-2xl" data-new-project>
      <PageHeader title={t.projects.createTitle} />
      {!mayCreate ? (
        <p className="text-sm text-ink-muted">{t.projects.notAdmin}</p>
      ) : (
        <form onSubmit={submit} className="space-y-4" noValidate>
          {create.error && !Object.keys(fields).length && <ErrorBanner>{create.error.message}</ErrorBanner>}
          <Field
            label={t.projects.name}
            hint={t.projects.nameHint}
            value={name}
            maxLength={PROJECT_NAME_MAX_LENGTH}
            onChange={(event) => setName(event.target.value)}
            error={fields.name}
            autoFocus
            required
          />
          <Field
            label={t.projects.key}
            hint={keyHint}
            value={shownKey}
            maxLength={PROJECT_KEY_MAX_LENGTH}
            className="font-mono uppercase"
            onChange={(event) => {
              setKeyTouched(true);
              setKey(event.target.value.toUpperCase());
            }}
            error={fields.key}
            required
          />
          <Field
            label={t.projects.description}
            hint={t.projects.descriptionHint}
            value={description}
            maxLength={PROJECT_DESCRIPTION_MAX_LENGTH}
            rows={3}
            onChange={(event) => setDescription(event.target.value)}
            error={fields.description}
          />
          <div className="flex gap-2">
            <Button type="submit" loading={create.isPending} data-action="create-project">
              {t.projects.submit}
            </Button>
            <Button type="button" variant="secondary" onClick={() => navigate({ to: PROJECTS_PATH })}>
              {t.projects.cancel}
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}
