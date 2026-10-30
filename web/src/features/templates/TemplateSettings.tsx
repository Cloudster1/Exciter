import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useAttributes } from "@/api/attributes";
import type { Project } from "@/api/projects";
import {
  templateFileHref,
  useDeleteTemplate,
  useExportTemplates,
  useUpdateTemplate,
  useUploadTemplate,
  type ExportTemplate,
  type TemplateField,
  type TemplateInclude,
} from "@/api/templates";
import { Icon } from "@/components/icons";
import { Button, ButtonLink, Card, Checkbox, Dialog, ErrorBanner, Field, IconButton, SectionTitle, Select, Skeleton, Tag } from "@/components/ui";
import { TEMPLATE_DESCRIPTION_MAX_LENGTH, TEMPLATE_MAX_BYTES, TEMPLATE_MAX_MB, TEMPLATE_NAME_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

const FIXED_FIELDS = ["type", "status", "owner", "priority", "labels", "revision", "rationale"] as const;

/** A field's name as the settings show it. */
export function fieldLabel(f: TemplateField): string {
  return f.field === "attribute" ? (f.attribute ?? "") : (t.templates.fieldNames[f.field] ?? f.field);
}

/** The project's Word templates and those other projects share; its administrators upload and shape its own. */
export function TemplateSettings({ project }: { project: Project }) {
  const { data: templates, error } = useExportTemplates(project.key);
  const canAdminister = project.can.administer && !project.archivedAt;
  const own = (templates ?? []).filter((tpl) => tpl.projectKey.toUpperCase() === project.key.toUpperCase());
  const others = (templates ?? []).filter((tpl) => tpl.projectKey.toUpperCase() !== project.key.toUpperCase());
  return (
    <section aria-labelledby="templates-title" className="space-y-4" data-templates>
      <div className="space-y-1">
        <SectionTitle id="templates-title">{t.templates.title}</SectionTitle>
        <p className="text-sm text-ink-muted">{t.templates.intro}</p>
      </div>
      {canAdminister ? <UploadForm project={project} /> : <p className="text-sm text-ink-muted">{t.templates.notAdmin}</p>}
      {error && <ErrorBanner>{t.templates.failed}</ErrorBanner>}
      {!templates && !error && <Skeleton />}
      {templates && own.length === 0 && <p className="text-sm text-ink-muted">{t.templates.none}</p>}
      {own.map((tpl) => (
        <TemplateCard key={tpl.id} template={tpl} project={project} />
      ))}
      {others.length > 0 && (
        <div className="space-y-3">
          <h3 className="text-base font-semibold text-ink">{t.templates.sharedByOthers}</h3>
          {others.map((tpl) => (
            <TemplateCard key={tpl.id} template={tpl} project={project} />
          ))}
        </div>
      )}
    </section>
  );
}

function UploadForm({ project }: { project: Project }) {
  const upload = useUploadTemplate(project.key);
  const [file, setFile] = useState<File | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [form, setForm] = useState(0);
  const tooLarge = Boolean(file && file.size > TEMPLATE_MAX_BYTES);
  const fields = upload.error instanceof ApiError ? upload.error.fields : {};
  const refusal = tooLarge
    ? t.templates.tooLarge(TEMPLATE_MAX_MB)
    : upload.error
      ? (fields.file ?? (fields.name ? undefined : upload.error.message))
      : undefined;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!file) return;
    upload.mutate(
      { file, name: name.trim() || file.name.replace(/\.(docx|dotx)$/i, ""), description },
      {
        onSuccess: () => {
          setFile(null);
          setName("");
          setDescription("");
          setForm((n) => n + 1);
        },
      },
    );
  }

  return (
    <Card className="p-4">
      <form key={form} onSubmit={submit} className="space-y-3" aria-label={t.templates.uploadTitle} data-template-upload>
        <h3 className="text-base font-semibold text-ink">{t.templates.uploadTitle}</h3>
        <div className="space-y-1">
          <label htmlFor="template-file" className="block text-sm font-medium text-ink">
            {t.templates.file}
          </label>
          <input
            id="template-file"
            type="file"
            accept=".docx,.dotx,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.openxmlformats-officedocument.wordprocessingml.template"
            aria-describedby="template-file-hint"
            className="block text-sm text-ink"
            onChange={(event) => setFile(event.target.files?.[0] ?? null)}
            data-template-file
          />
          <p id="template-file-hint" className="text-xs text-ink-muted">
            {t.templates.fileHint(TEMPLATE_MAX_MB)}
          </p>
        </div>
        <Field
          label={t.templates.name}
          id="template-name"
          value={name}
          maxLength={TEMPLATE_NAME_MAX_LENGTH}
          error={fields.name}
          onChange={(event) => setName(event.target.value)}
          data-template-name
        />
        <Field
          label={t.templates.description}
          id="template-description"
          rows={2}
          value={description}
          maxLength={TEMPLATE_DESCRIPTION_MAX_LENGTH}
          error={fields.description}
          onChange={(event) => setDescription(event.target.value)}
        />
        {refusal && <ErrorBanner>{refusal}</ErrorBanner>}
        <Button type="submit" icon={<Icon.Upload />} loading={upload.isPending} disabled={!file || tooLarge} data-action="upload-template">
          {t.templates.upload}
        </Button>
      </form>
    </Card>
  );
}

function TemplateCard({ template: tpl, project }: { template: ExportTemplate; project: Project }) {
  const editable = tpl.can.edit && !project.archivedAt;
  // Every template holds {{content}}; the line names what it fills in.
  const filled = tpl.placeholders.filter((p) => p !== "content");
  return (
    <Card className="space-y-3 p-4" data-template={tpl.name}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="space-y-1">
          <p className="font-semibold text-ink">
            {tpl.name}
            {tpl.shared && <Tag className="ml-2">{t.templates.sharedTag}</Tag>}
          </p>
          {tpl.description && <p className="text-sm text-ink-muted">{tpl.description}</p>}
          <p className="text-xs text-ink-muted">
            {tpl.projectKey.toUpperCase() !== project.key.toUpperCase() && `${t.templates.from(tpl.projectKey)} · `}
            {tpl.fileName}
            {filled.length > 0 && ` · ${t.templates.uses(filled.map((p) => `{{${p}}}`).join(", "))}`}
          </p>
        </div>
        <ButtonLink size="sm" variant="ghost" icon={<Icon.Download />} href={templateFileHref(tpl.id)} download aria-label={t.templates.download(tpl.name)}>
          {tpl.fileName}
        </ButtonLink>
      </div>
      {editable ? (
        <TemplateForm template={tpl} project={project} />
      ) : (
        tpl.fields.length > 0 && <p className="text-sm text-ink">{tpl.fields.map(fieldLabel).join(", ")}</p>
      )}
    </Card>
  );
}

function TemplateForm({ template: tpl, project }: { template: ExportTemplate; project: Project }) {
  const update = useUpdateTemplate(tpl.id);
  const [name, setName] = useState(tpl.name);
  const [description, setDescription] = useState(tpl.description);
  const [fields, setFields] = useState<TemplateField[]>(tpl.fields);
  const [include, setInclude] = useState<TemplateInclude>(tpl.include);
  const [shared, setShared] = useState(tpl.shared);
  const [deleting, setDeleting] = useState(false);
  const errors = update.error instanceof ApiError ? update.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    update.mutate({ name, description, fields, include, shared });
  }

  return (
    <form onSubmit={submit} className="space-y-4" aria-label={tpl.name}>
      <Field
        label={t.templates.name}
        id={`template-${tpl.id}-name`}
        value={name}
        maxLength={TEMPLATE_NAME_MAX_LENGTH}
        error={errors.name}
        onChange={(e) => setName(e.target.value)}
      />
      <Field
        label={t.templates.description}
        id={`template-${tpl.id}-description`}
        rows={2}
        value={description}
        maxLength={TEMPLATE_DESCRIPTION_MAX_LENGTH}
        error={errors.description}
        onChange={(e) => setDescription(e.target.value)}
      />
      <FieldList id={tpl.id} project={project} fields={fields} onChange={setFields} error={errors.fields} />
      <fieldset className="space-y-2">
        <legend className="text-sm font-medium text-ink">{t.templates.include}</legend>
        <Checkbox
          label={t.templates.links}
          checked={include.links}
          onChange={(e) => setInclude({ ...include, links: e.target.checked })}
          data-include="links"
        />
        <Checkbox
          label={t.templates.comments}
          checked={include.comments}
          onChange={(e) => setInclude({ ...include, comments: e.target.checked })}
          data-include="comments"
        />
        <Checkbox
          label={t.templates.changeMarks}
          checked={include.changeMarks}
          onChange={(e) => setInclude({ ...include, changeMarks: e.target.checked })}
          data-include="changeMarks"
        />
      </fieldset>
      <Checkbox label={t.templates.share} checked={shared} onChange={(e) => setShared(e.target.checked)} data-template-share />
      {update.error && !errors.name && !errors.description && !errors.fields && <ErrorBanner>{update.error.message}</ErrorBanner>}
      {update.isSuccess && (
        <p role="status" className="text-sm text-ink-muted">
          {t.templates.saved}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" loading={update.isPending} data-action="save-template">
          {t.templates.save}
        </Button>
        <Button type="button" variant="danger" icon={<Icon.Trash />} onClick={() => setDeleting(true)} data-action="delete-template">
          {t.templates.delete}
        </Button>
      </div>
      {deleting && <DeleteDialog template={tpl} onClose={() => setDeleting(false)} />}
    </form>
  );
}

function FieldList({
  id,
  project,
  fields,
  onChange,
  error,
}: {
  id: string;
  project: Project;
  fields: TemplateField[];
  onChange: (fields: TemplateField[]) => void;
  error?: string;
}) {
  const { data: attributes = [] } = useAttributes(project.key);
  const [pick, setPick] = useState("");
  const chosen = new Set(fields.map((f) => (f.field === "attribute" ? `attribute:${f.attribute?.toLowerCase()}` : f.field)));
  const fixed = FIXED_FIELDS.filter((f) => !chosen.has(f));
  const named = attributes.filter((a) => !a.retired && !chosen.has(`attribute:${a.name.toLowerCase()}`));

  function move(i: number, by: number) {
    const next = [...fields];
    const [f] = next.splice(i, 1);
    next.splice(i + by, 0, f!);
    onChange(next);
  }

  function add() {
    if (!pick) return;
    const [field, attribute] = pick.split(":", 2) as [TemplateField["field"], string | undefined];
    onChange([...fields, attribute ? { field, attribute } : { field }]);
    setPick("");
  }

  return (
    <fieldset className="space-y-2" aria-describedby={`template-${id}-fields-hint`}>
      <legend className="text-sm font-medium text-ink">{t.templates.fields}</legend>
      <p id={`template-${id}-fields-hint`} className="text-xs text-ink-muted">
        {t.templates.fieldsHint}
      </p>
      {fields.length > 0 && (
        <ol className="space-y-1" data-template-fields>
          {fields.map((f, i) => {
            const label = fieldLabel(f);
            return (
              <li key={`${f.field}-${f.attribute ?? ""}`} className="flex items-center gap-1 text-sm text-ink" data-template-field={label}>
                <span className="flex-1">{label}</span>
                <IconButton size="sm" icon={<Icon.ChevronUp />} label={t.templates.moveUp(label)} disabled={i === 0} onClick={() => move(i, -1)} />
                <IconButton
                  size="sm"
                  icon={<Icon.ChevronDown />}
                  label={t.templates.moveDown(label)}
                  disabled={i === fields.length - 1}
                  onClick={() => move(i, 1)}
                />
                <IconButton size="sm" icon={<Icon.X />} label={t.templates.remove(label)} onClick={() => onChange(fields.filter((_, j) => j !== i))} />
              </li>
            );
          })}
        </ol>
      )}
      <div className="flex items-end gap-2">
        <Select label={t.templates.addField} id={`template-${id}-add`} value={pick} error={error} onChange={(e) => setPick(e.target.value)} controlSize="sm">
          <option value="">{t.templates.pick}</option>
          {fixed.map((f) => (
            <option key={f} value={f}>
              {t.templates.fieldNames[f]}
            </option>
          ))}
          {named.length > 0 && (
            <optgroup label={t.templates.attributes}>
              {named.map((a) => (
                <option key={a.id} value={`attribute:${a.name}`}>
                  {a.name}
                </option>
              ))}
            </optgroup>
          )}
        </Select>
        <Button type="button" size="sm" variant="secondary" icon={<Icon.Plus />} disabled={!pick} onClick={add} data-action="add-template-field">
          {t.templates.add}
        </Button>
      </div>
    </fieldset>
  );
}

function DeleteDialog({ template: tpl, onClose }: { template: ExportTemplate; onClose: () => void }) {
  const remove = useDeleteTemplate(tpl.id);
  return (
    <Dialog
      open
      title={t.templates.deleteTitle(tpl.name)}
      onClose={onClose}
      footer={
        <>
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.templates.cancel}
          </Button>
          <Button
            type="button"
            variant="danger"
            loading={remove.isPending}
            onClick={() => remove.mutate(undefined, { onSuccess: onClose })}
            data-action="confirm-delete-template"
          >
            {t.templates.delete}
          </Button>
        </>
      }
    >
      <p className="text-sm text-ink">{t.templates.deleteBody}</p>
      {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
    </Dialog>
  );
}
