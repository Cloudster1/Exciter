import { Button } from "@/components/ui";
import { t } from "@/i18n";

/** Previous and next through a list read a window at a time, and where the window is; nothing when one window holds it. */
export function Pages({
  offset,
  shown,
  total,
  size,
  label,
  onOffset,
}: {
  offset: number;
  shown: number;
  total: number;
  size: number;
  label: string;
  onOffset: (n: number) => void;
}) {
  if (total <= size) return null;
  return (
    <nav aria-label={label} className="flex items-center justify-between gap-2">
      <Button variant="secondary" size="sm" disabled={offset === 0} onClick={() => onOffset(Math.max(0, offset - size))}>
        {t.coverage.previous}
      </Button>
      <span className="text-sm text-ink-subtle tabular-nums">{t.coverage.range(offset + 1, offset + shown, total)}</span>
      <Button variant="secondary" size="sm" disabled={offset + size >= total} onClick={() => onOffset(offset + size)}>
        {t.coverage.next}
      </Button>
    </nav>
  );
}
