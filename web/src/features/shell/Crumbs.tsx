import type { ReactNode } from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { useMe } from "@/api/auth";
import { Breadcrumbs, type Crumb } from "@/components/ui";
import { crumbsFor } from "./places";

/** Where the reader is, at the top of the page's sticky strip: the installation, then the places the page lies
 *  under, each a link, and the page itself when it is a place. */
export function PlaceCrumbs() {
  const { data: me } = useMe();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  if (!me) return null;
  const crumbs: Crumb[] = [
    { label: me.installation.name, render: (label) => <Link to="/">{label}</Link> },
    ...crumbsFor(pathname).map(({ label, path }) => ({
      label,
      render: path ? (shown: ReactNode) => <Link to={path}>{shown}</Link> : (shown: ReactNode) => <span aria-current="page">{shown}</span>,
    })),
  ];
  return <Breadcrumbs crumbs={crumbs} className="hidden min-w-0 truncate pb-2 sm:block [&_a]:hover:text-ink" />;
}
