import { useState } from "react";
import { Link } from "@tanstack/react-router";
import type { Project } from "@/api/projects";
import { useSession, type SessionScope } from "@/api/sessions";
import { Button } from "@/components/ui";
import { t } from "@/i18n";
import { OpenSessionDialog } from "./OpenSessionDialog";

/** Opens a review session over a scope given beforehand, such as a section or a baseline, for the project's editors. */
export function OpenSessionButton({ project, scope, size = "md" }: { project: Project; scope: Partial<SessionScope>; size?: "sm" | "md" }) {
  const [open, setOpen] = useState(false);
  if (!project.can.edit || project.archivedAt) return null;
  return (
    <>
      <Button variant={size === "sm" ? "ghost" : "secondary"} size={size} onClick={() => setOpen(true)} data-action="open-session">
        {t.sessions.open}
      </Button>
      {open && <OpenSessionDialog project={project} scope={scope} onClose={() => setOpen(false)} />}
    </>
  );
}

/** Says which session the document reviews, and leads back to its page. */
export function SessionBanner({ project, sessionId }: { project: Project; sessionId: string }) {
  const { data } = useSession(sessionId);
  if (!data) return null;
  return (
    <p role="status" className="mb-4 rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink" data-reviewing-session={sessionId}>
      <Link to="/p/$projectKey/sessions/$sessionId" params={{ projectKey: project.key, sessionId }} className="hover:text-accent hover:underline">
        {t.sessions.reviewingSession(data.title)}
      </Link>
    </p>
  );
}
