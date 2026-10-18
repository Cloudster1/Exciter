import type { Bucket, WidgetData } from "@/api/dashboards";
import type { View } from "@/api/search";
import { fromAddress, toAddress, type GridAddress } from "@/features/grid/filter";
import { t } from "@/i18n";

// Where a widget's bucket opens: the grid with the widget's source and the bucket's condition joined, so it keeps
// the same requirements the widget counted.

/** A query's own order, which a condition joined after it would leave stranded in the middle. */
const ORDER = /\s+ORDER\s+BY\s+.*$/is;

/** Joins a condition to a query in the query language. */
export function joinQuery(query: string, cond: string): string {
  const source = query.replace(ORDER, "").trim();
  if (!cond) return query;
  return source ? `(${source}) AND ${cond}` : cond;
}

/** The grid's address for a bucket of a widget's data; a view's source keeps the view's filter and order. */
export function bucketAddress(data: WidgetData, cond: string, view?: View): GridAddress {
  if (data.viewId && view) {
    const { filter, sort } = fromAddress(toAddress(view.filter, view.sort));
    return toAddress({ ...filter, query: joinQuery(filter.query, cond) }, sort, { view: view.id });
  }
  const query = joinQuery(data.query, cond);
  return query ? { query } : {};
}

/** A bucket as a reader names it: its value, None for none, Other for the tail counted as one. */
export function bucketLabel(bucket: Bucket, words: Record<string, string> = {}): string {
  if (bucket.label === "" && bucket.query === "") return t.dashboard.other;
  if (bucket.label === "") return t.dashboard.noneValue;
  return words[bucket.label] ?? bucket.label;
}
