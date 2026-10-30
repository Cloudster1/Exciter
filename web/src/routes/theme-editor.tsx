import { createRoute } from "@tanstack/react-router";
import { useTheme } from "@/api/themes";
import { ErrorBanner, PageHeader, Page } from "@/components/ui";
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
      <Page width="content">
        <PageHeader title={t.themes.editTheme} />
        <ErrorBanner>{error.message}</ErrorBanner>
      </Page>
    );
  }
  if (themeId && (isLoading || !data)) return null;
  return (
    <Page width="content">
      <PageHeader title={data?.theme.name ?? t.themes.newTheme} />
      <ThemeEditor key={data?.theme.id ?? "new"} theme={data?.theme} />
    </Page>
  );
}
