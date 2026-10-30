import { useSyncExternalStore } from "react";
import { locale, t } from "@/i18n";

/** A date formatter that writes in whichever language the interface speaks when it is called. */
export interface LocalDateFormat {
  format: (date: Date | number) => string;
}

const dateFormats = new Map<string, Intl.DateTimeFormat>();
const numberFormats = new Map<string, Intl.NumberFormat>();

function cached<T>(cache: Map<string, T>, key: string, make: () => T): T {
  let found = cache.get(key);
  if (!found) {
    found = make();
    cache.set(key, found);
  }
  return found;
}

/** The zone the browser is in, which is the reader's until their profile names one. */
export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

/** Whether the browser knows a zone by this name. */
export function isTimeZone(name: string): boolean {
  if (!name) return false;
  try {
    new Intl.DateTimeFormat("en", { timeZone: name });
    return true;
  } catch {
    return false;
  }
}

/**
 * Every zone the browser knows, for a profile to choose from. The API carries
 * the same database, so a zone picked here is one it takes.
 */
export function timeZones(): string[] {
  try {
    return (Intl as unknown as { supportedValuesOf: (key: string) => string[] }).supportedValuesOf("timeZone");
  } catch {
    return [browserTimeZone()];
  }
}

// The profile's zone, or none to follow the browser. Kept here rather than read
// from the session on each call, as the language is, so a module-level format
// writes in it too.
let chosenZone: string | undefined;
const zoneListeners = new Set<() => void>();

/** The zone times are written in: the profile's, else the browser's. */
export function timeZone(): string {
  return chosenZone ?? browserTimeZone();
}

/** Writes times in a profile's zone from now on; an empty or unknown one follows the browser. */
export function applyTimeZone(choice: string | null | undefined): string {
  const next = choice && isTimeZone(choice) ? choice : undefined;
  if (next !== chosenZone) {
    chosenZone = next;
    for (const listener of zoneListeners) listener();
  }
  return timeZone();
}

function subscribeZone(listener: () => void): () => void {
  zoneListeners.add(listener);
  return () => zoneListeners.delete(listener);
}

/** The zone times are written in, re-rendering the caller when it changes. */
export function useTimeZone(): string {
  return useSyncExternalStore(subscribeZone, timeZone, timeZone);
}

/**
 * Formats dates in the active locale and the reader's zone. Modules keep one at
 * their top level; both are looked up on each call, so a change reaches it. A
 * format that names its own zone, such as a calendar day kept in UTC, keeps it.
 */
export function localDateFormat(options: Intl.DateTimeFormatOptions): LocalDateFormat {
  const shape = JSON.stringify(options);
  return {
    format: (date) => {
      const tag = locale();
      const zone = options.timeZone ?? timeZone();
      return cached(dateFormats, `${tag} ${zone} ${shape}`, () => new Intl.DateTimeFormat(tag, { ...options, timeZone: zone })).format(date);
    },
  };
}

/** A number in the active locale: 1,234.5 in English, 1.234,5 in German. */
export function formatNumber(value: number, options: Intl.NumberFormatOptions = {}): string {
  const tag = locale();
  return cached(numberFormats, `${tag} ${JSON.stringify(options)}`, () => new Intl.NumberFormat(tag, options)).format(value);
}

const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const MONTH = 30 * DAY;

const olderThanAMonth = localDateFormat({ dateStyle: "medium" });

/**
 * How long ago something was, in the short forms the suite shares and the
 * language shown: "2h ago", "vor 2 Std.". Older than a month it is a date.
 */
export function relativeTime(when: Date | number | string, now: number = Date.now()): string {
  const then = new Date(when).getTime();
  const seconds = Math.round((now - then) / 1000);
  if (seconds < MINUTE) return t.kit.ago.now;
  const minutes = Math.round(seconds / MINUTE);
  if (minutes < 60) return t.kit.ago.minutes(minutes);
  const hours = Math.round(seconds / HOUR);
  if (hours < 24) return t.kit.ago.hours(hours);
  if (seconds < MONTH) return t.kit.ago.days(Math.round(seconds / DAY));
  return olderThanAMonth.format(then);
}
