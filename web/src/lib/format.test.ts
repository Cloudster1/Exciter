import { afterEach, describe, expect, it } from "vitest";
import { applyLanguage } from "@/i18n";
import { applyTimeZone, browserTimeZone, localDateFormat, relativeTime, timeZone } from "./format";

afterEach(() => {
  applyLanguage("");
  applyTimeZone("");
});

describe("times in the reader's zone", () => {
  const noon = Date.parse("2026-09-05T12:00:00Z");
  const at = localDateFormat({ timeStyle: "short", hourCycle: "h23" });

  it("follows the browser until the profile names a zone", () => {
    expect(timeZone()).toBe(browserTimeZone());
    applyTimeZone("Europe/Berlin");
    expect(timeZone()).toBe("Europe/Berlin");
    expect(at.format(noon)).toBe("14:00");
    applyTimeZone("America/New_York");
    expect(at.format(noon)).toBe("08:00");
  });

  it("follows the browser again for an empty or unknown zone", () => {
    applyTimeZone("Europe/Berlin");
    expect(applyTimeZone("Mars/Olympus")).toBe(browserTimeZone());
    applyTimeZone("Europe/Berlin");
    expect(applyTimeZone("")).toBe(browserTimeZone());
  });

  it("keeps the zone a format names for itself, as a calendar day kept in UTC does", () => {
    applyTimeZone("Pacific/Honolulu");
    const day = localDateFormat({ dateStyle: "medium", timeZone: "UTC" });
    expect(day.format(Date.parse("2026-11-02T00:00:00Z"))).toBe("Nov 2, 2026");
  });
});

describe("relative times", () => {
  const now = Date.parse("2026-09-05T12:00:00Z");

  it("keeps the short forms and falls back to a date after a month", () => {
    expect(relativeTime("2026-09-05T11:59:30Z", now)).toBe("just now");
    expect(relativeTime("2026-09-05T11:30:00Z", now)).toBe("30m ago");
    expect(relativeTime("2026-09-05T09:00:00Z", now)).toBe("3h ago");
    expect(relativeTime("2026-09-02T12:00:00Z", now)).toBe("3d ago");
    applyTimeZone("UTC");
    expect(relativeTime("2026-06-01T12:00:00Z", now)).toBe("Jun 1, 2026");
  });

  it("says them in the language shown", () => {
    applyLanguage("de");
    expect(relativeTime("2026-09-05T11:59:30Z", now)).toBe("gerade eben");
    expect(relativeTime("2026-09-05T09:00:00Z", now)).toBe("vor 3 Std.");
    expect(relativeTime("2026-09-02T12:00:00Z", now)).toBe("vor 3 T.");
  });
});
