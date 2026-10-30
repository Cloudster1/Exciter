import { Button } from "./Button";
import { t } from "@/i18n";

/** A noun as a count says it: once for one, in the plural for any other number. */
export interface Counted {
  one: string;
  many: string;
}

/** How many a whole list holds, not just the page on screen. */
export function countOf(total: number, noun: Counted): string {
  return `${total} ${total === 1 ? noun.one : noun.many}`;
}

function range(offset: number, shown: number, total: number): string {
  return t.kit.pager.range(total === 0 ? 0 : offset + 1, offset + shown, total);
}

/** The whole count while it fits on the page, and which of it is shown once it does not. */
export function shownOf(total: number, offset: number, shown: number, noun: Counted): string {
  if (offset === 0 && shown >= total) return countOf(total, noun);
  return `${range(offset, shown, total)} ${noun.many}`;
}

/** A page past the first, as an address names it; the first page is the address without one. */
export function isPageOffset(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value > 0;
}

/** The start of the last page, for a page left past the end when the list shrank. */
export function clampOffset(offset: number, total: number, pageSize: number): number {
  if (offset < total) return offset;
  return Math.max(0, Math.floor((total - 1) / pageSize) * pageSize);
}

interface PagerProps {
  total: number;
  offset: number;
  pageSize: number;
  onChange: (offset: number) => void;
  /** Names the list for a screen reader, as "Version pages" or "Member pages". */
  label: string;
}

/** Steps through a list longer than one page. */
export function Pager({ total, offset, pageSize, onChange, label }: PagerProps) {
  if (total <= pageSize && offset === 0) return null;
  return (
    <nav aria-label={label} className="mt-3 flex items-center justify-end gap-2 text-sm text-ink-muted">
      <span className="tabular-nums" data-page-range>
        {range(offset, Math.max(0, Math.min(pageSize, total - offset)), total)}
      </span>
      <Button variant="ghost" size="sm" aria-label={t.kit.pager.previousPage} disabled={offset === 0} onClick={() => onChange(Math.max(0, offset - pageSize))}>
        {t.kit.pager.previous}
      </Button>
      <Button variant="ghost" size="sm" aria-label={t.kit.pager.nextPage} disabled={offset + pageSize >= total} onClick={() => onChange(offset + pageSize)}>
        {t.kit.pager.next}
      </Button>
    </nav>
  );
}
