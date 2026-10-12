import type { StatusCategory } from "@/api/workflows";
import { Icon } from "@/components/icons";
import { cx } from "@/components/ui";
import { t } from "@/i18n";

/** How a status wears its category: the card's tint and border, the badge, the ring and the glyph. */
export const categoryLook: Record<StatusCategory, { card: string; badge: string; ring: string; glyph: typeof Icon.Check }> = {
  draft: { card: "bg-status-todo-subtle border-status-todo/40", badge: "bg-status-todo", ring: "border-status-todo", glyph: Icon.Edit },
  review: { card: "bg-status-progress-subtle border-status-progress/40", badge: "bg-status-progress", ring: "border-status-progress", glyph: Icon.Search },
  approved: { card: "bg-accent-subtle border-accent/40", badge: "bg-accent", ring: "border-accent", glyph: Icon.Check },
  done: { card: "bg-status-done-subtle border-status-done/40", badge: "bg-status-done", ring: "border-status-done", glyph: Icon.Shield },
  closed: { card: "bg-surface-sunken border-border-strong", badge: "bg-ink-subtle", ring: "border-ink-subtle", glyph: Icon.X },
};

/** A status by name, in its category's colour, with the category said in words for whoever cannot see it. */
export function StatusBadge({ name, category, className }: { name: string; category: StatusCategory; className?: string }) {
  const look = categoryLook[category];
  return (
    <span
      className={cx("inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium text-ink", look.card, className)}
      data-status={name}
      data-status-category={category}
      title={t.workflows.categories[category]}
    >
      <span aria-hidden className={cx("size-2 rounded-full", look.badge)} />
      {name}
      <span className="sr-only">{`, ${t.workflows.categories[category]}`}</span>
    </span>
  );
}
