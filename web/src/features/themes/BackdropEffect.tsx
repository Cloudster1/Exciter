import { useEffect, useRef, useState } from "react";
import { useActiveTheme } from "@/api/themes";
import { startConfetti } from "@/lib/confetti";
import { startConstellation } from "@/lib/constellation";
import { subscribeThemePreview, themePreviewEffect } from "./ThemeLoader";

/** The moving picture a theme asks for, drawn live under everything at the top of the scrolling column, taking
 * no clicks; nothing is drawn when the theme asks for nothing, so the default look costs nothing. */
export function BackdropEffect() {
  const { data } = useActiveTheme();
  const [preview, setPreview] = useState<string | null | undefined>(themePreviewEffect());
  useEffect(() => subscribeThemePreview(() => setPreview(themePreviewEffect())), []);
  const effect = preview !== undefined ? preview : (data?.theme?.spec.effect ?? null);
  if (effect === "confetti") return <Confetti />;
  if (effect !== "constellation") return null;
  return <Constellation />;
}

// Confetti lives over the page rather than under it, since a burst under the content would be hidden by the very
// card that was clicked; it takes no clicks itself.
function Confetti() {
  const canvas = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    if (!canvas.current) return;
    return startConfetti(canvas.current);
  }, []);
  return (
    <div className="pointer-events-none fixed inset-0 z-50" data-backdrop-effect="confetti" aria-hidden="true">
      <canvas ref={canvas} className="block h-full w-full" />
    </div>
  );
}

function Constellation() {
  const canvas = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    if (!canvas.current) return;
    return startConstellation(canvas.current);
  }, []);
  return (
    <div className="pointer-events-none sticky top-0 -z-10 h-0" data-backdrop-effect="constellation" aria-hidden="true">
      <canvas ref={canvas} className="absolute left-0 top-0 block" />
    </div>
  );
}
