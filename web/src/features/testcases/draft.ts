import type { components } from "@/api/schema";
import { isEmptyDoc, type Doc } from "@/features/editor/schema";

type Wire = components["schemas"];

/** What a test case carries besides a requirement's content (#250), as the API answers and takes it. */
export type TestCaseContent = Wire["TestCaseContent"];
export type TestStep = Wire["TestStep"];
export type TestParameter = Wire["TestParameter"];

/** A step being written; key keeps its editor while steps move. */
export interface StepDraft {
  key: string;
  action: Doc | null;
  expected: Doc | null;
}

/** A parameter being written, its values as typed: apart by semicolons. */
export interface ParameterDraft {
  key: string;
  name: string;
  values: string;
}

export interface TestCaseDraft {
  preconditions: Doc | null;
  steps: StepDraft[];
  parameters: ParameterDraft[];
}

let made = 0;
/** A key no other draft row holds, for React to keep each row's editor. */
export function draftKey(): string {
  made += 1;
  return `draft-${made}`;
}

/** The parameters' values, apart by semicolons, as one parameter's text field holds them. */
export const VALUE_SEPARATOR = ";";

export function draftOf(content: TestCaseContent | null | undefined): TestCaseDraft {
  return {
    preconditions: (content?.preconditions as Doc | null) ?? null,
    steps: (content?.steps ?? []).map((s) => ({ key: draftKey(), action: (s.action as Doc | null) ?? null, expected: (s.expected as Doc | null) ?? null })),
    parameters: (content?.parameters ?? []).map((p) => ({ key: draftKey(), name: p.name, values: p.values.join(`${VALUE_SEPARATOR} `) })),
  };
}

function docOrNull(doc: Doc | null): Doc | null {
  return doc && !isEmptyDoc(doc) ? doc : null;
}

/** The draft as the API takes it; steps keep their place, an empty one included, so the server names its problem. */
export function contentOf(draft: TestCaseDraft): TestCaseContent {
  return {
    preconditions: docOrNull(draft.preconditions),
    steps: draft.steps.map((s) => ({ action: s.action ?? { type: "doc", content: [] }, expected: docOrNull(s.expected) })),
    parameters: draft.parameters.map((p) => ({
      name: p.name.trim(),
      values: p.values
        .split(VALUE_SEPARATOR)
        .map((v) => v.trim())
        .filter((v) => v !== ""),
    })),
  };
}

/** Whether a draft holds anything a test case carries. */
export function holdsAnything(draft: TestCaseDraft): boolean {
  return Boolean(docOrNull(draft.preconditions)) || draft.steps.length > 0 || draft.parameters.length > 0;
}

/** How often the case runs: once per value its parameters line up, once without any. */
export function iterationsOf(content: TestCaseContent): number {
  return content.parameters.length === 0 ? 1 : (content.parameters[0]?.values.length ?? 1);
}
