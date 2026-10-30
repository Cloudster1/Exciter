import { useRef, useState, type DragEvent } from "react";
import { attachmentUrl, formatSize, showsInPlace, useAttachments, useDeleteAttachment, useUploadAttachments } from "@/api/attachments";
import type { Requirement } from "@/api/requirements";
import { Button, ButtonLink, Card, ErrorBanner, IconButton, SectionTitle, Skeleton, Tag, cx, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { localDateFormat } from "@/lib/format";

const uploadedAt = localDateFormat({ dateStyle: "medium" });

function hasFiles(event: DragEvent): boolean {
  return Array.from(event.dataTransfer.types).includes("Files");
}

/** The files on a requirement, as Stator's panel lists a page's. Its editors attach more, by the picker or by
 * dropping them on the panel, and delete them. */
export function AttachmentPanel({ requirement }: { requirement: Requirement }) {
  const { data: files, error, refetch } = useAttachments(requirement.key);
  const { upload, pending, errors, clearErrors } = useUploadAttachments(requirement.key);
  const remove = useDeleteAttachment(requirement.key);
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const toast = useToast();
  const confirm = useConfirm();
  const editable = requirement.can.edit;

  function send(chosen: File[]) {
    clearErrors();
    remove.reset();
    for (const file of chosen) void upload(file).then((made) => made && toast.success(t.attachments.uploaded(made.fileName)));
  }

  const drop = editable
    ? {
        onDragEnter: (event: DragEvent) => {
          if (hasFiles(event)) setOver(true);
        },
        onDragOver: (event: DragEvent) => {
          if (!hasFiles(event)) return;
          event.preventDefault();
          event.dataTransfer.dropEffect = "copy";
        },
        onDragLeave: (event: DragEvent) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOver(false);
        },
        onDrop: (event: DragEvent) => {
          if (!hasFiles(event)) return;
          event.preventDefault();
          setOver(false);
          send([...event.dataTransfer.files]);
        },
      }
    : {};

  return (
    <Card
      className={cx("space-y-3 p-4", over && "border-dashed border-accent bg-accent-subtle")}
      data-attachments
      data-drop-active={over || undefined}
      {...drop}
    >
      <section aria-labelledby="attachments-title" className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <SectionTitle id="attachments-title">{files && files.length > 0 ? t.attachments.count(files.length) : t.attachments.title}</SectionTitle>
          {editable && (
            <>
              <input
                ref={input}
                type="file"
                multiple
                className="sr-only"
                tabIndex={-1}
                aria-hidden="true"
                onChange={(event) => {
                  const chosen = [...(event.target.files ?? [])];
                  event.target.value = "";
                  send(chosen);
                }}
                data-attachment-input
              />
              <Button size="sm" variant="secondary" icon={<Icon.Paperclip />} onClick={() => input.current?.click()} data-action="attach-files">
                {t.attachments.attach}
              </Button>
            </>
          )}
        </div>
        {over && <p className="text-sm text-ink">{t.attachments.dropHere}</p>}
        <div className="space-y-2" data-attachment-errors>
          {errors.map((message) => (
            <ErrorBanner key={message}>{message}</ErrorBanner>
          ))}
          {remove.error && <ErrorBanner>{remove.error.message}</ErrorBanner>}
          {error && <ErrorBanner onRetry={() => void refetch()}>{error.message}</ErrorBanner>}
        </div>
        {pending.length > 0 && (
          <ul className="space-y-1" data-uploads>
            {pending.map((name, i) => (
              <li key={`${name}:${i}`} className="text-sm text-ink-muted" data-upload={name}>
                {t.attachments.uploading(name)}
              </li>
            ))}
          </ul>
        )}
        {!files && !error && <Skeleton />}
        {files && files.length === 0 && pending.length === 0 && (
          <p className="text-sm text-ink-subtle">{editable ? t.attachments.emptyEditable : t.attachments.empty}</p>
        )}
        {files && files.length > 0 && (
          <ul className="divide-y divide-border rounded-control border border-border">
            {files.map((file) => {
              const inPlace = showsInPlace(file.contentType);
              return (
                <li key={file.id} className="flex flex-wrap items-center gap-3 px-3 py-2 text-sm" data-attachment={file.fileName} data-attachment-id={file.id}>
                  <Icon.File className="shrink-0 text-ink-muted" />
                  <a
                    href={attachmentUrl(file.id, inPlace)}
                    target={inPlace ? "_blank" : undefined}
                    rel="noreferrer"
                    download={inPlace ? undefined : file.fileName}
                    className="min-w-0 flex-1 truncate text-ink hover:text-accent"
                    aria-label={inPlace ? t.attachments.open(file.fileName) : t.attachments.download(file.fileName)}
                  >
                    {file.fileName}
                  </a>
                  {file.referenced && <Tag>{t.attachments.shown}</Tag>}
                  <span className="text-ink-muted tabular-nums">{formatSize(file.size)}</span>
                  <span className="hidden text-ink-muted sm:inline">
                    {t.attachments.uploadedBy(file.uploadedByName, uploadedAt.format(new Date(file.createdAt)))}
                  </span>
                  <ButtonLink
                    href={attachmentUrl(file.id)}
                    download={file.fileName}
                    variant="ghost"
                    size="sm"
                    icon={<Icon.Download />}
                    aria-label={t.attachments.download(file.fileName)}
                    title={t.attachments.download(file.fileName)}
                    data-action="download-attachment"
                  />
                  {editable && (
                    <IconButton
                      icon={<Icon.Trash />}
                      label={t.attachments.remove(file.fileName)}
                      size="sm"
                      disabled={remove.isPending && remove.variables === file.id}
                      onClick={async () => {
                        const noun = t.kit.confirm.things.named(t.kit.confirm.things.attachment, file.fileName);
                        if (!(await confirm({ noun, body: t.attachments.confirmRemove }))) return;
                        clearErrors();
                        remove.mutate(file.id, { onSuccess: () => toast.success(t.attachments.removed(file.fileName)) });
                      }}
                      data-action="delete-attachment"
                    />
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </Card>
  );
}
