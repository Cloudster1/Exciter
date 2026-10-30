import type { ReactNode } from "react";
import { administers, useMe } from "@/api/auth";
import { EmptyState, PageHeader, Page } from "@/components/ui";
import { Icon } from "@/components/icons";

/** A settings page for the installation's owners and administrators; anybody else is told whose it is. */
export function AdminsOnly({ title, refusal, children }: { title: string; refusal: string; children: ReactNode }) {
  const { data: me } = useMe();
  if (administers(me?.role)) return children;
  return (
    <Page width="narrow" data-admins-only>
      <PageHeader title={title} />
      <EmptyState icon={<Icon.Lock />} title={title} description={refusal} />
    </Page>
  );
}
