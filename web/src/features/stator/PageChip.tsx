import type { StatorPage, StatorPages } from "@/api/stator";
import { STATOR_ACCOUNT_PATH } from "@/config";
import { t } from "@/i18n";

// The issue chip's counterpart for a page: its title, space, path and version as the viewer may read them, or what
// everybody may know of it without a token.

/** What a chip can show this viewer: the page itself, or its space with why there is no more. */
export type PageChipState = { kind: "page"; page: StatorPage } | { kind: "connect" } | { kind: "loading" } | { kind: "unreachable" } | { kind: "hidden" };

/** Decides a chip's state from what the lookup found for its page. */
export function pageChipState(id: string, lookup: StatorPages, connected: boolean): PageChipState {
  if (!connected) return { kind: "connect" };
  switch (lookup.status) {
    case "not_configured":
    case "not_connected":
    case "rejected":
      return { kind: "connect" };
    case "unreachable":
      return { kind: "unreachable" };
  }
  if (!lookup.pages.has(id)) return { kind: "loading" };
  const page = lookup.pages.get(id);
  return page ? { kind: "page", page } : { kind: "hidden" };
}

/** A page a requirement is traced to, drawn from the lookup, falling back on the space and the stored version. */
export function PageChip({
  pageId,
  href,
  spaceKey,
  version,
  state,
}: {
  pageId: string;
  href: string;
  spaceKey: string;
  version: number;
  state: PageChipState;
}) {
  const c = t.stator.chip;
  if (state.kind === "page") {
    const page = state.page;
    return (
      <a
        href={page.url}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex min-w-0 max-w-full flex-wrap items-baseline gap-x-2 text-ink hover:underline"
        data-stator-page={pageId}
        data-state="page"
      >
        <span className="shrink-0 font-mono text-xs text-ink-muted">
          <span className="sr-only">{c.space} </span>
          {page.spaceKey}
        </span>
        <span className="min-w-0 truncate font-medium">{page.title}</span>
        {page.path.length > 0 && <span className="min-w-0 truncate text-xs text-ink-muted">{page.path.join(" / ")}</span>}
        <span className="shrink-0 text-xs text-ink-muted">{c.version(page.version)}</span>
      </a>
    );
  }
  return (
    <span className="inline-flex flex-wrap items-baseline gap-2" data-stator-page={pageId} data-state={state.kind}>
      <a href={href} target="_blank" rel="noopener noreferrer" className="text-ink hover:underline">
        <span className="font-mono text-xs">{spaceKey}</span> {c.page}
      </a>
      {version > 0 && <span className="text-xs text-ink-muted">{c.version(version)}</span>}
      {state.kind === "connect" && (
        <a href={STATOR_ACCOUNT_PATH} className="text-xs text-ink-muted underline" data-action="connect-stator-hint">
          {c.connect}
        </a>
      )}
      {state.kind === "hidden" && <span className="text-xs text-ink-muted">{c.notAvailable}</span>}
      {state.kind === "unreachable" && <span className="text-xs text-ink-muted">{c.unreachable}</span>}
    </span>
  );
}
