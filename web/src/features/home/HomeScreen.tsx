import { useMe } from "@/api/auth";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";

/** Where the reader lands: what Exciter is for, until there are projects to show. */
export function HomeScreen() {
  const { data: me } = useMe();
  return (
    <div className="mx-auto max-w-3xl" data-home>
      <PageHeader title={t.home.title} meta={me?.organization?.name} />
      <EmptyState icon={<Icon.Home />} title={t.home.emptyTitle} description={t.home.emptyBody} />
    </div>
  );
}
