import { locale } from "@/i18n";

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

/**
 * Formats dates in the active locale. Modules keep one at their top level; the
 * locale is looked up on each call, so a change of language reaches it.
 */
export function localDateFormat(options: Intl.DateTimeFormatOptions): LocalDateFormat {
  const shape = JSON.stringify(options);
  return {
    format: (date) => {
      const tag = locale();
      return cached(dateFormats, `${tag} ${shape}`, () => new Intl.DateTimeFormat(tag, options)).format(date);
    },
  };
}

/** A number in the active locale: 1,234.5 in English, 1.234,5 in German. */
export function formatNumber(value: number, options: Intl.NumberFormatOptions = {}): string {
  const tag = locale();
  return cached(numberFormats, `${tag} ${JSON.stringify(options)}`, () => new Intl.NumberFormat(tag, options)).format(value);
}
