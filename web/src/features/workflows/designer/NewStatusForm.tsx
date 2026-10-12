import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { STATUS_CATEGORIES, useStatusActions, type Status, type StatusCategory } from "@/api/workflows";
import { Button, ErrorBanner, Field, Select } from "@/components/ui";
import { STATUS_DESCRIPTION_MAX_LENGTH, STATUS_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

/** Makes a status the project does not have yet, right where it is wanted; its category says what counting
 * and coverage make of a requirement in it. */
export function NewStatusForm({
  projectKey,
  onCreated,
  heading = t.workflows.coinHeading,
  submitLabel = t.workflows.coinSubmit,
}: {
  projectKey: string;
  onCreated: (status: Status) => void;
  heading?: string;
  submitLabel?: string;
}) {
  const { create } = useStatusActions(projectKey);
  const [name, setName] = useState("");
  const [category, setCategory] = useState<StatusCategory>("review");
  const [description, setDescription] = useState("");
  const fields = create.error instanceof ApiError ? create.error.fields : {};

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { name: name.trim(), category, description: description.trim() },
      {
        onSuccess: (status) => {
          setName("");
          setDescription("");
          onCreated(status);
        },
      },
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-2" noValidate data-new-status>
      <span className="block text-sm font-medium text-ink-muted">{heading}</span>
      {create.error && !Object.keys(fields).length && <ErrorBanner>{create.error.message}</ErrorBanner>}
      <Field
        id="field-new-status-name"
        label={t.workflows.statusName}
        value={name}
        maxLength={STATUS_NAME_MAX_LENGTH}
        onChange={(event) => setName(event.target.value)}
        error={fields.name}
      />
      <Select
        id="field-new-status-category"
        label={t.workflows.countsAs}
        value={category}
        onChange={(event) => setCategory(event.target.value as StatusCategory)}
        error={fields.category}
      >
        {STATUS_CATEGORIES.map((each) => (
          <option key={each} value={each}>
            {t.workflows.categories[each]}
          </option>
        ))}
      </Select>
      <Field
        id="field-new-status-description"
        label={t.workflows.description}
        value={description}
        maxLength={STATUS_DESCRIPTION_MAX_LENGTH}
        onChange={(event) => setDescription(event.target.value)}
        error={fields.description}
      />
      <Button type="submit" size="sm" variant="secondary" loading={create.isPending} disabled={!name.trim()} data-action="create-status">
        {submitLabel}
      </Button>
    </form>
  );
}
