import { t } from "@/i18n";
import { createContext, useContext, type CSSProperties, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { cx } from "./cx";

/** Where a page's head is drawn: the shell's strip over the content, else in place. */
export const ShellHeaderContext = createContext<HTMLElement | null>(null);

export interface Crumb {
  label: ReactNode;
  /** A link, rendered by the caller so the router's typing is kept. */
  render?: (label: ReactNode) => ReactNode;
}

/** Where the page sits: the sidebar's path minus the page itself. */
export function Breadcrumbs({ crumbs, className }: { crumbs: Crumb[]; className?: string }) {
  if (crumbs.length === 0) return null;
  return (
    <nav aria-label={t.common.breadcrumb} className={cx("text-sm text-ink-muted", className)}>
      <ol className="flex flex-wrap items-center gap-1.5">
        {crumbs.map((crumb, index) => (
          <li key={index} className="flex items-center gap-1.5">
            {index > 0 && <span aria-hidden="true">/</span>}
            {crumb.render ? crumb.render(crumb.label) : <span>{crumb.label}</span>}
          </li>
        ))}
      </ol>
    </nav>
  );
}

// The head of a page: a title, the facts about it, and what can be done from
// here. No room for a sentence about the page; the empty state says that.
export function PageHeader({
  crumb,
  crumbs,
  title,
  meta,
  actions,
  tabs,
  className,
  style,
}: {
  /** Where this page sits, drawn small above the title; crumbs is the structured form. */
  crumb?: ReactNode;
  crumbs?: Crumb[];
  title: ReactNode;
  /** Facts about the thing on the page: counts, a key, a type. */
  meta?: ReactNode;
  actions?: ReactNode;
  /** A row of tabs under the title, when the page has views. */
  tabs?: ReactNode;
  className?: string;
  /** Carries a page sheet's widths to a header drawn in the shell, outside the sheet. */
  style?: CSSProperties;
}) {
  const target = useContext(ShellHeaderContext);
  const header = (
    <header className={cx(target ? "pb-3" : "mb-5", className)} style={style}>
      <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
        <div className="min-w-0">
          {crumbs && crumbs.length > 0 ? (
            <Breadcrumbs crumbs={crumbs} className="mb-1" />
          ) : (
            crumb && <div className="mb-1 flex items-center gap-1.5 text-sm text-ink-muted">{crumb}</div>
          )}
          <h1 className="flex items-center gap-2 text-lg font-semibold tracking-tight text-ink">{title}</h1>
          {meta && <div className="mt-0.5 text-sm text-ink-muted">{meta}</div>}
        </div>
        {actions && <div className="flex flex-wrap items-center gap-2 sm:shrink-0">{actions}</div>}
      </div>
      {tabs && <div className="mt-3">{tabs}</div>}
    </header>
  );
  return target ? createPortal(header, target) : header;
}
