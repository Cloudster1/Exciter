import { permittedRoles, useAttributes, useFieldRules, type FixedField } from "@/api/attributes";
import type { Project } from "@/api/projects";
import { t } from "@/i18n";

/** Whether the reader changes a field of a requirement in its status, and if not, why (#206). */
export interface Access {
  edit: boolean;
  why: string;
}

type Status = { id: string; name: string } | null | undefined;

const open: Access = { edit: true, why: "" };

/** Reads whether the reader may change each field of a requirement in a status, a fixed one by its name and an
 * attribute by its id; the server holds the same rules, so this only says beforehand what it would refuse. */
export function useAccess(project: Project | undefined): (field: string, status: Status) => Access {
  const { data: rules } = useFieldRules(project?.key ?? "");
  const { data: attributes } = useAttributes(project?.key ?? "");
  return (field, status) => {
    if (!project) return open;
    const fixed = rules?.find((rule) => rule.field === field);
    const attribute = fixed ? undefined : attributes?.find((each) => each.id === field);
    const who = permittedRoles(fixed?.editableBy ?? attribute?.editableBy ?? [], status?.id, project.roles);
    if (who === null) return open;
    const label = fixed ? t.fieldRules.labels[field as FixedField] : (attribute?.name ?? field);
    const words = who.map((role) => t.fieldRules.roles[role] ?? role);
    return { edit: false, why: words.length === 0 ? t.fieldRules.nobody(label, status?.name ?? "") : t.fieldRules.only(words, label, status?.name ?? "") };
  };
}

/** The same for one requirement's status. */
export function useFieldAccess(project: Project, status: Status): (field: string) => Access {
  const access = useAccess(project);
  return (field) => access(field, status);
}
