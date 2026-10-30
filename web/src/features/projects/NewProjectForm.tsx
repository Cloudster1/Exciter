import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMyAccess } from "@/api/auth";
import { ApiError } from "@/api/client";
import { suggestKey, useCreateProject, useKeyCheck, useProjectTemplates } from "@/api/projects";
import { Button, ErrorBanner, Field, OptionCard, PageHeader, Page, Skeleton } from "@/components/ui";
import { KEY_SUGGEST_DEBOUNCE_MS, PROJECTS_PATH, PROJECT_DESCRIPTION_MAX_LENGTH, PROJECT_KEY_MAX_LENGTH, PROJECT_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";

/** Makes a project; the key follows the name, as a free key the server suggests, until somebody types one of their own. */
export function NewProjectForm() {
  const navigate = useNavigate();
  const access = useMyAccess();
  const mayCreate = access.data?.createProject ?? false;
  const create = useCreateProject();
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [description, setDescription] = useState("");
  const [template, setTemplate] = useState("");
  const { data: templates = [] } = useProjectTemplates();
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
    create.mutate(
      { name, key: shownKey, description, ...(template ? { template } : {}) },
      { onSuccess: (made) => void navigate({ to: "/p/$projectKey", params: { projectKey: made.key } }) },
    );
  }

  return (
    <Page width="narrow" data-new-project>
      <PageHeader title={t.projects.createTitle} />
      {access.isPending ? (
        <Skeleton />
      ) : !mayCreate ? (
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
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium text-ink">{t.projects.startFrom}</legend>
            <div role="radiogroup" aria-label={t.projects.startFrom} className="grid gap-3 sm:grid-cols-2" data-template-chooser>
              <OptionCard
                title={t.projects.blank}
                description={t.projects.blankDescription}
                checked={template === ""}
                onSelect={() => setTemplate("")}
                data-template=""
              />
              {templates.map((each) => {
                const named = t.projects.templates[each.key];
                return (
                  <OptionCard
                    key={each.key}
                    title={named?.name ?? each.name}
                    description={named?.description ?? each.description}
                    extra={<span className="text-xs text-ink-muted">{t.projects.templateMakes(each.types.join(", "), each.sections.join(", "))}</span>}
                    checked={template === each.key}
                    onSelect={() => setTemplate(each.key)}
                    data-template={each.key}
                  />
                );
              })}
            </div>
            {fields.template && <ErrorBanner>{fields.template}</ErrorBanner>}
          </fieldset>
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
    </Page>
  );
}
