import { RouterProvider, type AnyRouter } from "@tanstack/react-router";
import { useProfileLanguage } from "@/api/auth";
import { useLanguage } from "@/i18n";
import { useTimeZone } from "@/lib/format";

// Strings and times are read while rendering, so a new language or zone draws the
// whole tree again; the query cache outlives it, so nothing is fetched twice.
export function LocalizedRouter({ router }: { router: AnyRouter }) {
  useProfileLanguage();
  const language = useLanguage();
  const zone = useTimeZone();
  return <RouterProvider key={`${language} ${zone}`} router={router} />;
}
