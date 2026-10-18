import { useMemo } from "react";
import type { Project } from "@/api/projects";
import { useQualityCheck, type Draft, type QualityCheck } from "@/api/quality";
import type { TermMark } from "@/features/editor/terms";
import { findingMarks } from "./FindingsList";

/** The glossary terms a check found in the statement, as the editor marks them (#210). */
export function termMarks(terms: QualityCheck["terms"]): TermMark[] {
  return terms
    .filter((term) => term.field === "statement")
    .map((term) => ({
      termId: term.termId,
      name: term.name,
      preview: term.definitionPreview,
      explicit: term.explicit,
      block: term.block,
      from: term.from,
      to: term.to,
    }));
}

/** A requirement being written, checked against the writing rules as typing pauses (#207). `marks` and `terms` are
 * undefined while the latest answer is of an older draft, so the editor keeps the marks it has, moved with the text. */
export function useDraftQuality(project: Project, draft: Draft, enabled: boolean) {
  const on = enabled && project.can.edit && !project.archivedAt;
  const { check, current, error } = useQualityCheck(project.key, draft, on);
  const marks = useMemo(() => (current && check ? findingMarks(check.findings) : undefined), [current, check]);
  const terms = useMemo(() => (current && check ? termMarks(check.terms ?? []) : undefined), [current, check]);
  return { on, check, current, failed: Boolean(error), marks, terms };
}
