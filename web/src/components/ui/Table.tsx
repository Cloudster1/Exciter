import type { HTMLAttributes, TdHTMLAttributes, ThHTMLAttributes } from "react";
import { cx } from "./cx";

// Tables, for lists with more than one thing worth reading per row: a card per
// row hides the columns, a table shows them and lets the eye run down one.
export function Table({ children, className, dense, sticky, ...rest }: HTMLAttributes<HTMLTableElement> & { dense?: boolean; sticky?: boolean }) {
  return (
    // Positioned, so a visually hidden header inside is clipped by the scroll
    // container instead of widening the whole page on a narrow screen.
    <div className={cx("relative overflow-x-auto rounded-overlay border border-border bg-surface", sticky && "max-h-full overflow-y-auto")}>
      <table
        {...rest}
        data-dense={dense || undefined}
        className={cx("w-full border-collapse text-sm", sticky && "[&_thead_th]:sticky [&_thead_th]:top-0 [&_thead_th]:bg-surface", className)}
      >
        {children}
      </table>
    </div>
  );
}

export function Th({ children, className, ...rest }: ThHTMLAttributes<HTMLTableCellElement>) {
  return (
    <th {...rest} className={cx("border-b border-border px-3 py-2 text-left text-2xs font-medium tracking-wide text-ink-subtle uppercase", className)}>
      {children}
    </th>
  );
}

export function Td({ children, className, ...rest }: TdHTMLAttributes<HTMLTableCellElement>) {
  return (
    <td
      {...rest}
      className={cx("border-b border-border px-3 py-2 align-middle tabular-nums [tr:last-child_&]:border-b-0 [table[data-dense]_&]:py-1.5", className)}
    >
      {children}
    </td>
  );
}
