import { useId, type ReactNode } from "react";
import { SectionTitle, Table, Td, Th } from "@/components/ui";
import { DocView } from "@/features/editor/DocView";
import type { Doc } from "@/features/editor/schema";
import { t } from "@/i18n";
import { iterationsOf, type TestCaseContent } from "./draft";

/** What a test case carries, read: its preconditions, its parameters by iteration, and its steps as a table. */
export function TestCaseView({ content, small = false }: { content: TestCaseContent; small?: boolean }) {
  const preconditions = content.preconditions as Doc | null;
  const iterations = iterationsOf(content);
  const id = useId();
  const ids = { preconditions: `${id}-preconditions`, parameters: `${id}-parameters`, steps: `${id}-steps` };
  // In a document a test case sits below its entry's heading, so its parts take a label rather than a heading.
  const heading = (at: string, text: string): ReactNode =>
    small ? (
      <p id={at} className="text-xs font-semibold uppercase text-ink-muted">
        {text}
      </p>
    ) : (
      <SectionTitle id={at}>{text}</SectionTitle>
    );
  return (
    <div className="space-y-4" data-test-case="">
      {preconditions && (
        <section aria-labelledby={ids.preconditions} className="space-y-1">
          {heading(ids.preconditions, t.testcases.preconditions)}
          <DocView doc={preconditions} />
        </section>
      )}
      {content.parameters.length > 0 && (
        <section aria-labelledby={ids.parameters} className="space-y-1">
          {heading(ids.parameters, t.testcases.parameters)}
          <Table aria-labelledby={ids.parameters} dense data-test-parameters="">
            <thead>
              <tr>
                <Th>{t.testcases.iteration}</Th>
                {content.parameters.map((p) => (
                  <Th key={p.name}>{p.name}</Th>
                ))}
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: iterations }, (_, n) => (
                <tr key={n}>
                  <Td>{n + 1}</Td>
                  {content.parameters.map((p) => (
                    <Td key={p.name}>{p.values[n]}</Td>
                  ))}
                </tr>
              ))}
            </tbody>
          </Table>
        </section>
      )}
      <section aria-labelledby={ids.steps} className="space-y-1">
        {heading(ids.steps, t.testcases.steps)}
        {content.steps.length === 0 ? (
          <p className="text-sm text-ink-muted">{t.testcases.noSteps}</p>
        ) : (
          <Table aria-labelledby={ids.steps} dense data-test-steps="">
            <thead>
              <tr>
                <Th className="w-12">#</Th>
                <Th>{t.testcases.action}</Th>
                <Th>{t.testcases.expected}</Th>
              </tr>
            </thead>
            <tbody>
              {content.steps.map((s, i) => (
                <tr key={i} data-step={i + 1}>
                  <Td className="align-top tabular-nums">{i + 1}</Td>
                  <Td className="align-top">
                    <DocView doc={s.action as Doc} />
                  </Td>
                  <Td className="align-top">{s.expected ? <DocView doc={s.expected as Doc} /> : null}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </section>
    </div>
  );
}
