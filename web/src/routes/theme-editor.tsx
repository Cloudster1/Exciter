import { createRoute } from "@tanstack/react-router";
import { useTheme } from "@/api/themes";
import { ErrorBanner, PageHeader } from "@/components/ui";
import { ThemeEditor } from "@/features/themes/ThemeEditor";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const themeNewRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/themes/new",
  component: () => <EditorPage />,
});

export const themeEditRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/themes/$themeId",
  component: () => {
    const { themeId } = themeEditRoute.useParams();
    return <EditorPage themeId={themeId} />;
  },
});

function EditorPage({ themeId }: { themeId?: string }) {
  const { data, error, isLoading } = useTheme(themeId);
  if (themeId && error) {
    return (
      <div className="mx-auto max-w-5xl">
        <PageHeader title={t.themes.editTheme} />
        <ErrorBanner>{error.message}</ErrorBanner>
      </div>
    );
  }
  if (themeId && (isLoading || !data)) return null;
  return (
    <div className="mx-auto max-w-5xl">
      <PageHeader title={data?.theme.name ?? t.themes.newTheme} />
      <ThemeEditor key={data?.theme.id ?? "new"} theme={data?.theme} />
    </div>
  );
}
