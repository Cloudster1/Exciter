import { COUNT_MAX } from "@/config";
import { cx } from "./cx";

/**
 * How many of something wait, beside a place or on the bell, as every tool of
 * the suite draws it: capped where it would crowd, said in full to a screen
 * reader, and nothing while there is none.
 */
export function Count({ count, name, says, className }: { count: number; name: string; says: string; className?: string }) {
  if (count <= 0) return null;
  return (
    <span className={cx("rounded-full bg-accent-subtle px-1.5 text-2xs leading-4 font-medium text-accent tabular-nums", className)} data-count={name}>
      <span aria-hidden="true">{count > COUNT_MAX ? `${COUNT_MAX}+` : count}</span>
      <span className="sr-only">, {says}</span>
    </span>
  );
}
