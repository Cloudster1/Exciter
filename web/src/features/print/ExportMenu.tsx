import { useState } from "react";
import type { DocumentFormat } from "@/api/documents";
import { Button, Menu, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { FileExportDialog } from "./FileExport";

/** A menu that exports something as PDF, Word or an HTML site, through a template of projectKey's, and as ReqIF where
 * the exchange covers it. */
export function ExportMenu({
  title,
  href,
  projectKey,
  reqifHref,
  size = "md",
}: {
  title: string;
  href: (format: DocumentFormat) => string;
  projectKey?: string;
  reqifHref?: string;
  size?: "sm" | "md";
}) {
  const [format, setFormat] = useState<DocumentFormat>();
  const items: MenuItem[] = [
    { label: t.documents.pdf, icon: <Icon.Download />, onSelect: () => setFormat("pdf"), attrs: { "data-action": "export-pdf" } },
    { label: t.documents.docx, icon: <Icon.Download />, onSelect: () => setFormat("docx"), attrs: { "data-action": "export-docx" } },
    { label: t.documents["html.zip"], icon: <Icon.Download />, onSelect: () => setFormat("html.zip"), attrs: { "data-action": "export-html" } },
  ];
  if (reqifHref) {
    items.push({
      label: t.documents.reqif,
      icon: <Icon.Download />,
      onSelect: () => {
        window.location.assign(reqifHref);
      },
      attrs: { "data-action": "export-reqif-menu" },
    });
  }
  return (
    <>
      <Menu
        label={t.documents.exportMenu(title)}
        align="end"
        items={items}
        trigger={(props) => (
          <Button
            variant={size === "sm" ? "ghost" : "secondary"}
            size={size}
            icon={<Icon.Download />}
            onClick={props.toggle}
            aria-haspopup={props["aria-haspopup"]}
            aria-expanded={props["aria-expanded"]}
            aria-controls={props["aria-controls"]}
            aria-label={t.documents.exportMenu(title)}
            data-action="export-menu"
          >
            {t.documents.export}
          </Button>
        )}
      />
      {format && <FileExportDialog format={format} href={href(format)} title={title} projectKey={projectKey} onClose={() => setFormat(undefined)} />}
    </>
  );
}
