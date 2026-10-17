import { useProjects } from "@/api/projects";
import { Chip, ErrorBanner } from "@/components/ui";
import { TOKEN_MAX_PROJECTS } from "@/config";
import { t } from "@/i18n";

/** The projects a token or a connected app is limited to; none picked is every project its person reaches. */
export function ProjectPicker({
  id,
  picked,
  onChange,
  some,
  error,
}: {
  id: string;
  picked: string[];
  onChange: (picked: string[]) => void;
  /** What a limited choice means, said of the token or of the app. */
  some: string;
  error?: string;
}) {
  const { data: choosable = [] } = useProjects();
  if (choosable.length === 0) return null;
  return (
    <>
      <fieldset aria-describedby={`${id}-hint`} data-token-projects-picker>
        <legend className="text-sm font-medium text-ink">{t.tokens.projects}</legend>
        <p id={`${id}-hint`} className="mb-2 text-xs text-ink-muted">
          {picked.length === 0 ? t.tokens.projectsAll : some}
        </p>
        <div className="flex flex-wrap gap-1.5">
          {choosable.map((project) => (
            <Chip
              key={project.key}
              pressed={picked.includes(project.key)}
              disabled={!picked.includes(project.key) && picked.length >= TOKEN_MAX_PROJECTS}
              data-project={project.key}
              onClick={() => onChange(picked.includes(project.key) ? picked.filter((k) => k !== project.key) : [...picked, project.key])}
            >
              {project.name}
              <span className="text-xs">{project.key}</span>
            </Chip>
          ))}
        </div>
      </fieldset>
      {error && <ErrorBanner>{error}</ErrorBanner>}
    </>
  );
}
