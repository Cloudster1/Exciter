import { useState } from "react";
import { IconButton, Tooltip } from "@/components/ui";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { applyTheme, nextTheme, readTheme, type Theme } from "@/lib/theme";

const glyphs: Record<Theme, typeof Icon.Monitor> = { system: Icon.Monitor, light: Icon.Sun, dark: Icon.Moon };

/** Steps through Auto, Light and Dark. The button's name is the current choice, which is what a screen reader
 * and the tests read. */
export function ThemeButton({ side = "right" }: { side?: "top" | "bottom" | "right" }) {
  const [theme, setTheme] = useState<Theme>(readTheme);
  const label = t.theme[theme];
  const Glyph = glyphs[theme];

  function cycle() {
    const next = nextTheme(theme);
    setTheme(next);
    applyTheme(next);
  }

  return (
    <Tooltip text={t.theme.tooltip(label)} side={side}>
      <IconButton icon={<Glyph />} label={label} size="sm" onClick={cycle} data-action="theme" data-theme-choice={theme} />
    </Tooltip>
  );
}
