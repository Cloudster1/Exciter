import { useMyTestExecutions } from "@/api/tests";
import { SectionTitle } from "@/components/ui";
import { t } from "@/i18n";
import { ExecutionTable } from "./TestsPage";

/** The open test executions the reader tests or manages, on the home page; nothing while there are none. */
export function MyExecutions() {
  const { data } = useMyTestExecutions();
  const open = (data ?? []).filter((e) => e.state === "open");
  if (open.length === 0) return null;
  return (
    <section aria-labelledby="home-executions" className="space-y-2" data-my-executions>
      <SectionTitle id="home-executions">{t.tests.mine}</SectionTitle>
      <ExecutionTable executions={open} label={t.tests.mine} />
    </section>
  );
}
