import type { ReactNode } from "react";
import { administers, useMe } from "@/api/auth";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";

/** A settings page for the organization's owners and administrators; anybody else is told whose it is. */
export function AdminsOnly({ title, refusal, children }: { title: string; refusal: string; children: ReactNode }) {
  const { data: me } = useMe();
  if (administers(me?.role)) return children;
  return (
    <div className="mx-auto max-w-3xl" data-admins-only>
      <PageHeader title={title} />
      <EmptyState icon={<Icon.Lock />} title={title} description={refusal} />
    </div>
  );
}
