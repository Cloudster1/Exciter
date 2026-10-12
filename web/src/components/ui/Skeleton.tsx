import { useEffect, useState } from "react";
import { SKELETON_DELAY_MS } from "@/config";
import { cx } from "./cx";

// A skeleton stands where the content will, so the page does not jump; it
// waits before showing so a fast load never flashes grey.
export function Skeleton({ lines = 3, rows, className }: { lines?: number; rows?: number; className?: string }) {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => setShown(true), SKELETON_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, []);
  if (!shown) return <div aria-hidden="true" className={className} data-skeleton="pending" />;
  const count = rows ?? lines;
  return (
    <div aria-hidden="true" data-skeleton className={cx("animate-pulse space-y-2", className)}>
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className={cx("rounded bg-surface-raised", rows ? "h-9" : "h-3.5", !rows && i === count - 1 && "w-2/3")} />
      ))}
    </div>
  );
}
