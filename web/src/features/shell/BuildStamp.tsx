import { useMeta } from "@/api/meta";
import { COMMIT_SHORT_LENGTH } from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

const builtAt = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/**
 * Which build is running, in one quiet line under the person. The short hash
 * is what someone reads out when reporting a problem; the whole of it, and
 * when it was built, sit in the title for the one who then looks it up.
 */
export function BuildStamp() {
  const { data } = useMeta();
  if (!data) return null;
  const { version, commit, builtAt: built } = data.build;
  const label = commit ? `${version} (${commit.slice(0, COMMIT_SHORT_LENGTH)})` : version;
  const detail = [commit && t.shell.build.commit(commit), built && t.shell.build.built(builtAt.format(new Date(built)))].filter(Boolean).join(", ");
  return (
    <div className="truncate px-3 pb-2 text-2xs text-ink-subtle" title={detail || undefined} data-build>
      {label}
    </div>
  );
}
