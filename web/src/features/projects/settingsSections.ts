import type { Project } from "@/api/projects";
import { t } from "@/i18n";

/** A project's settings, one route each, in the order of the suite's sections. */
export const PROJECT_SETTINGS_SECTIONS = [
  "details",
  "types",
  "attributes",
  "fields",
  "workflow",
  "approval",
  "links",
  "trace",
  "quality",
  "risks",
  "templates",
  "permissions",
  "import",
  "reqif",
  "word",
  "trash",
] as const;
export type ProjectSettingsSection = (typeof PROJECT_SETTINGS_SECTIONS)[number];

/** The sections only a project's administrators use, so nobody else is shown them. */
const ADMIN_ONLY: ReadonlySet<ProjectSettingsSection> = new Set(["permissions", "import", "reqif", "word"]);

export function sectionLabel(section: ProjectSettingsSection): string {
  const labels: Record<ProjectSettingsSection, string> = {
    details: t.projects.detailsTitle,
    types: t.types.tab,
    attributes: t.attributes.tab,
    fields: t.fieldRules.tab,
    workflow: t.workflows.tab,
    approval: t.review.rulesTab,
    links: t.linkTypes.tab,
    trace: t.chains.tab,
    quality: t.quality.tab,
    risks: t.risks.tab,
    templates: t.templates.tab,
    permissions: t.permissions.navLabel,
    import: t.imports.tab,
    reqif: t.reqif.tab,
    word: t.word.tab,
    trash: t.requirements.trashTitle,
  };
  return labels[section];
}

export function adminOnly(section: ProjectSettingsSection): boolean {
  return ADMIN_ONLY.has(section);
}

/** The sections somebody is shown: every one to the project's administrators, the rest to its other members. */
export function sectionsFor(project: Pick<Project, "can">): ProjectSettingsSection[] {
  return PROJECT_SETTINGS_SECTIONS.filter((section) => project.can.administer || !adminOnly(section));
}

export function isSection(value: unknown): value is ProjectSettingsSection {
  return typeof value === "string" && (PROJECT_SETTINGS_SECTIONS as readonly string[]).includes(value);
}

export function projectSettingsPath(projectKey: string, section: ProjectSettingsSection = "details"): string {
  return `/projects/${projectKey}/settings/${section}`;
}

/** Where /projects/<key>/settings sends somebody: to the section an old ?tab= names, else Details. */
export function settingsAddress(projectKey: string, tab: unknown, hash = ""): string {
  return `${projectSettingsPath(projectKey, isSection(tab) ? tab : "details")}${hash ? `#${hash}` : ""}`;
}

/** The section a path of the settings shows, from /projects/<key>/settings/<section>; Details for one it does not know. */
export function sectionOfPath(pathname: string): ProjectSettingsSection {
  const section = pathname.split("/")[4];
  if (section === "workflows") return "workflow";
  return isSection(section) ? section : "details";
}

/** Pages of a project that moved into its settings when they became sections of their own. */
const MOVED_PAGES: Record<string, ProjectSettingsSection> = { permissions: "permissions", trash: "trash" };

/**
 * Where an old /p/<key> address lives now, from what follows /p/, with its
 * search and hash kept. Permissions and the trash moved into the settings.
 */
export function movedProjectAddress(rest: string, searchStr = "", hash = ""): string {
  const after = `${searchStr}${hash ? `#${hash}` : ""}`;
  const [key = "", ...below] = rest.split("/");
  const path = below.join("/");
  const moved = Object.hasOwn(MOVED_PAGES, path) ? MOVED_PAGES[path] : undefined;
  if (moved) return `${projectSettingsPath(key, moved)}${after}`;
  return `/projects/${key}${path ? `/${path}` : ""}${after}`;
}
