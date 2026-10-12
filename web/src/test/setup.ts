import "@testing-library/jest-dom/vitest";
import "./layout";

// Node 26's built-in localStorage is inert without --localstorage-file and shadows jsdom's,
// so tests get an in-memory Storage; one that refuses storage can still spy on Storage.prototype.
class MemoryStorage implements Storage {
  #entries = new Map<string, string>();

  get length(): number {
    return this.#entries.size;
  }

  clear(): void {
    this.#entries.clear();
  }

  getItem(key: string): string | null {
    return this.#entries.get(key) ?? null;
  }

  key(index: number): string | null {
    return [...this.#entries.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.#entries.delete(key);
  }

  setItem(key: string, value: string): void {
    this.#entries.set(key, String(value));
  }
}

if (typeof globalThis.Storage === "undefined") {
  Object.defineProperty(globalThis, "Storage", { value: MemoryStorage, writable: true });
}

if (!globalThis.localStorage) {
  const storage = new MemoryStorage();
  Object.defineProperty(globalThis, "localStorage", { value: storage, writable: true });
  if (typeof window !== "undefined") {
    Object.defineProperty(window, "localStorage", { value: storage, writable: true });
  }
}

// jsdom leaves scrollTo unimplemented and logs an error each time the router
// restores the scroll position after a navigation.
if (typeof window !== "undefined") {
  window.scrollTo = () => {};
}

// jsdom has no canvas and logs an error for every context asked of it, which axe asks for.
if (typeof HTMLCanvasElement !== "undefined") {
  HTMLCanvasElement.prototype.getContext = (() => null) as typeof HTMLCanvasElement.prototype.getContext;
}
