import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "@tanstack/react-router";
import { administers, useLogout, useMe } from "@/api/auth";
import { useMarkRead } from "@/api/notifications";
import { useProjects } from "@/api/projects";
import { useQuickSearch } from "@/api/search";
import { cx } from "@/components/ui";
import { useEscape, useFocusReturn } from "@/components/ui/overlay";
import { Icon, type IconName } from "@/components/icons";
import { NEW_PROJECT_PATH, QUICK_SEARCH_DEBOUNCE_MS, SEARCH_PATH } from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";
import { recentRequirements } from "@/lib/recent";
import { areaTitle, placesFor } from "./places";

interface Option {
  key: string;
  label: string;
  hint: string;
  icon: IconName;
  pick: () => void;
}

/** Options under one heading, read out as a list of their own. */
interface Group {
  id: "found" | "recent" | "projects" | "places" | "actions" | "search";
  options: Option[];
}

/** Whether every word typed starts a word of the label, so "aud" finds the audit log and "set mem" the members. */
function matches(label: string, typed: string): boolean {
  const words = label.toLowerCase().split(/\s+/);
  return typed
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((part) => words.some((word) => word.startsWith(part)));
}

/**
 * The Ctrl K palette, Cmd K on a Mac: requirements by key or title as the person types and the ones opened last
 * while nothing is, the projects, the places this build has, what can be done, and a way on to the full search,
 * each kind a group of its own.
 */
export function QuickSwitcher({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const { data: me } = useMe();
  const ref = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const listId = useId();
  const [text, setText] = useState("");
  const [active, setActive] = useState(0);
  const [recent] = useState(recentRequirements);
  const projects = useProjects();
  const actions = useActions();
  useEscape(true, onClose);
  useFocusReturn(true, ref, true);

  const typed = text.trim();
  const words = useDebounced(typed, QUICK_SEARCH_DEBOUNCE_MS);
  const quick = useQuickSearch(typed ? words : "");
  const toRequirement = (projectKey: string, reqKey: string) => () => void navigate({ to: "/p/$projectKey/r/$reqKey", params: { projectKey, reqKey } });
  // The server matched these already; an answer to an earlier word, kept on show while the newer one loads, is not
  // offered for it.
  const found: Option[] =
    typed && words === typed && !quick.isPlaceholderData
      ? (quick.data ?? []).map((hit) => ({
          key: `requirement:${hit.requirement.id}`,
          label: `${hit.requirement.key} ${hit.requirement.title}`,
          hint: [hit.projectName, hit.outline].filter(Boolean).join(" "),
          icon: "Lines",
          pick: toRequirement(hit.requirement.projectKey, hit.requirement.key),
        }))
      : [];
  const opened: Option[] = typed
    ? []
    : recent.map((each) => ({
        key: `recent:${each.id}`,
        label: `${each.key} ${each.title}`,
        hint: each.projectKey,
        icon: "Lines",
        pick: toRequirement(each.projectKey, each.key),
      }));
  const projectOptions: Option[] = typed
    ? (projects.data ?? [])
        .filter((project) => matches(project.name, typed) || project.key.toLowerCase().startsWith(typed.toLowerCase()))
        .map((project) => ({
          key: `project:${project.key}`,
          label: project.name,
          hint: project.key,
          icon: "Folder",
          pick: () => void navigate({ to: "/p/$projectKey", params: { projectKey: project.key } }),
        }))
    : [];
  const places: Option[] = placesFor(me)
    .map((place) => ({
      key: `place:${place.path}`,
      label: place.label,
      hint: place.area === "work" ? "" : areaTitle(place.area),
      icon: place.icon,
      pick: () => void navigate({ to: place.path }),
    }))
    .filter((option) => matches(`${option.hint} ${option.label}`, typed));
  const searchEverything: Option[] = typed
    ? [
        {
          key: "search",
          label: t.search.searchFor(typed),
          hint: t.nav.search,
          icon: "Search",
          pick: () => void navigate({ to: SEARCH_PATH, search: { q: typed } }),
        },
      ]
    : [];
  const groups: Group[] = [
    { id: typed ? "found" : "recent", options: typed ? found : opened },
    { id: "projects", options: projectOptions },
    { id: "places", options: places },
    { id: "actions", options: typed ? actions.filter((action) => matches(action.label, typed)) : [] },
    { id: "search", options: searchEverything },
  ];
  const shown = groups.filter((group) => group.options.length > 0);
  // The arrows go through the groups in the order they are drawn.
  const options = shown.flatMap((group) => group.options);
  const current = Math.min(active, options.length - 1);
  const optionId = (index: number) => `${listId}-${index}`;
  const groupId = (group: Group) => `${listId}-${group.id}`;

  // The active option is followed from the keyboard, so the list scrolls itself rather than taking focus.
  useEffect(() => {
    document.getElementById(`${listId}-${current}`)?.scrollIntoView?.({ block: "nearest" });
  }, [listId, current]);

  const pick = (option: Option) => {
    onClose();
    option.pick();
  };

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    const last = options.length - 1;
    const moves: Record<string, number> = { ArrowDown: Math.min(current + 1, last), ArrowUp: Math.max(current - 1, 0), Home: 0, End: last };
    if (event.key in moves) {
      event.preventDefault();
      setActive(moves[event.key]!);
    } else if (event.key === "Enter") {
      event.preventDefault();
      const option = options[current];
      if (option) pick(option);
    }
  }

  const counted: Record<Group["id"], (count: number) => string> = {
    found: t.switcher.requirementsCount,
    recent: t.switcher.requirementsCount,
    projects: t.switcher.projectsCount,
    places: t.switcher.optionsCount,
    actions: t.switcher.actionsCount,
    search: t.switcher.optionsCount,
  };
  let index = 0;
  return createPortal(
    // biome-ignore lint/a11y: the scrim is for the pointer; the keyboard closes the switcher with Escape
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-ink/30 p-4 pt-[10vh]" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={t.switcher.title}
        className="w-full max-w-xl overflow-hidden rounded-overlay border border-border bg-surface-overlay shadow-2"
        data-quick-switcher
      >
        <div className="flex items-center gap-2 border-b border-border px-3">
          <Icon.Search className="shrink-0 text-ink-subtle" />
          <input
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setActive(0);
            }}
            onKeyDown={onKeyDown}
            role="combobox"
            aria-label={t.switcher.label}
            aria-autocomplete="list"
            aria-expanded={options.length > 0}
            aria-controls={shown.length > 0 ? shown.map(groupId).join(" ") : undefined}
            aria-activedescendant={options.length > 0 ? optionId(current) : undefined}
            placeholder={t.switcher.placeholder}
            autoComplete="off"
            spellCheck={false}
            className="h-11 min-w-0 flex-1 bg-transparent text-sm text-ink placeholder:text-ink-subtle focus:outline-none"
            data-quick-switcher-input
          />
          <kbd className="hidden font-mono text-2xs text-ink-subtle shell:inline">{t.switcher.escape}</kbd>
        </div>
        <div className="p-1">
          {options.length > 0 ? (
            <div ref={listRef} className="max-h-[50vh] overflow-y-auto">
              {shown.map((group) => (
                <div key={group.id}>
                  <p id={`${groupId(group)}-heading`} className="px-2 pt-2 pb-1 text-2xs font-medium tracking-wide text-ink-subtle uppercase">
                    {t.switcher.groups[group.id]}
                  </p>
                  <div id={groupId(group)} role="listbox" aria-labelledby={`${groupId(group)}-heading`} data-quick-switcher-group={group.id}>
                    {group.options.map((option) => {
                      const at = index++;
                      const Glyph = Icon[option.icon];
                      return (
                        <div
                          key={option.key}
                          id={optionId(at)}
                          role="option"
                          aria-selected={at === current}
                          tabIndex={-1}
                          onMouseMove={() => at !== current && setActive(at)}
                          // Picked on press, so focus never leaves the input that names the active option.
                          onMouseDown={(e) => {
                            e.preventDefault();
                            pick(option);
                          }}
                          className={cx("flex cursor-pointer items-center gap-2 rounded-control px-2 py-1.5 text-sm", at === current && "bg-accent-subtle")}
                          data-quick-switcher-option={option.key}
                        >
                          <Glyph className="shrink-0 text-ink-muted" />
                          <span className="min-w-0 flex-1 truncate text-ink">{option.label}</span>
                          <span className="max-w-[45%] shrink-0 truncate text-2xs text-ink-muted">{option.hint}</span>
                        </div>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p className="px-3 py-6 text-center text-sm text-ink-subtle" data-quick-switcher-empty>
              {t.switcher.noMatch}
            </p>
          )}
          {quick.isError && typed && (
            <p className="px-3 py-2 text-sm text-danger" data-quick-switcher-error>
              {t.search.quickFailed}
            </p>
          )}
        </div>
        <p role="status" className="sr-only">
          {shown.length > 0 ? shown.map((group) => counted[group.id](group.options.length)).join(", ") : t.switcher.optionsCount(0)}
        </p>
      </div>
    </div>,
    document.body,
  );
}

/** What can be done from anywhere, for whoever may do it. */
function useActions(): Option[] {
  const navigate = useNavigate();
  const { data: me } = useMe();
  const markRead = useMarkRead();
  const logout = useLogout();
  return [
    ...(administers(me?.role)
      ? [
          {
            key: "action:new-project",
            label: t.projects.create,
            hint: t.switcher.groups.actions,
            icon: "Plus" as const,
            pick: () => void navigate({ to: NEW_PROJECT_PATH }),
          },
        ]
      : []),
    { key: "action:mark-all-read", label: t.switcher.markAllRead, hint: t.switcher.groups.actions, icon: "Check", pick: () => markRead.mutate({ all: true }) },
    {
      key: "action:sign-out",
      label: t.account.signOut,
      hint: t.switcher.groups.actions,
      icon: "External",
      // Wherever the request ends, the session is no longer this browser's to use.
      pick: () => logout.mutate(undefined, { onSettled: () => void navigate({ to: "/login" }) }),
    },
  ];
}
