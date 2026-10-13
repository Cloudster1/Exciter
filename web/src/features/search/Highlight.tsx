// biome-ignore-all lint/suspicious/noArrayIndexKey: a run has no identity but its place, and the runs of one answer never reorder
import { Fragment } from "react";
import type { Segment } from "@/api/search";

/** Runs of text as the server marked them: matches in mark, all of it as text and never as markup. */
export function Highlight({ segments }: { segments: Segment[] }) {
  return (
    <>
      {segments.map((segment, index) =>
        segment.match ? (
          <mark key={index} className="rounded-sm bg-warning-subtle px-0.5 text-ink">
            {segment.text}
          </mark>
        ) : (
          <Fragment key={index}>{segment.text}</Fragment>
        ),
      )}
    </>
  );
}
