import { RouterProvider, type AnyRouter } from "@tanstack/react-router";
import { useProfileLanguage } from "@/api/auth";
import { useLanguage } from "@/i18n";

// Strings are read while rendering, so a new language draws the whole tree again;
// the query cache outlives it, so nothing is fetched twice.
export function LocalizedRouter({ router }: { router: AnyRouter }) {
  useProfileLanguage();
  const language = useLanguage();
  return <RouterProvider key={language} router={router} />;
}
