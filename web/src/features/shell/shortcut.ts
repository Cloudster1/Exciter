import { t } from "@/i18n";

/** Whether the reader is on a Mac, whose keyboard says Cmd where others say Ctrl. */
export function onMac(): boolean {
  if (typeof navigator === "undefined") return false;
  const platform = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ?? navigator.platform ?? "";
  return /mac|iphone|ipad/i.test(platform);
}

/** The palette's shortcut as this reader's keyboard names it, in their language. */
export function switcherShortcutHint(): string {
  return onMac() ? t.switcher.shortcutMac : t.switcher.shortcut;
}
