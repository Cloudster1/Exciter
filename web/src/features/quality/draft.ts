import { useMemo } from "react";
import type { Project } from "@/api/projects";
import { useQualityCheck, type Draft } from "@/api/quality";
import { findingMarks } from "./FindingsList";

/** A requirement being written, checked against the writing rules as typing pauses (#207). `marks` is undefined
 * while the latest answer is of an older draft, so the editor keeps the marks it has, moved with the text. */
export function useDraftQuality(project: Project, draft: Draft, enabled: boolean) {
  const on = enabled && project.can.edit && !project.archivedAt;
  const { check, current, error } = useQualityCheck(project.key, draft, on);
  const marks = useMemo(() => (current && check ? findingMarks(check.findings) : undefined), [current, check]);
  return { on, check, current, failed: Boolean(error), marks };
}
