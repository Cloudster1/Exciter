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

// React Flow measures its nodes before it draws anything, and jsdom measures nothing: these are the stand-ins its
// testing guide asks for, as Armature's setup has them, each only where jsdom has left a gap.
if (typeof globalThis.ResizeObserver === "undefined") {
  class StubResizeObserver implements ResizeObserver {
    #callback: ResizeObserverCallback;
    #stopped = false;
    constructor(callback: ResizeObserverCallback) {
      this.#callback = callback;
    }
    observe(target: Element): void {
      queueMicrotask(() => {
        if (this.#stopped) return;
        this.#callback([{ target, contentRect: target.getBoundingClientRect() } as ResizeObserverEntry], this);
      });
    }
    unobserve(): void {}
    disconnect(): void {
      this.#stopped = true;
    }
  }
  Object.defineProperty(globalThis, "ResizeObserver", { value: StubResizeObserver, writable: true });
}
if (typeof globalThis.DOMMatrixReadOnly === "undefined") {
  class StubDOMMatrixReadOnly {
    m22: number;
    constructor(transform?: string) {
      const scale = transform?.match(/scale\(([1-9.])\)/)?.[1];
      this.m22 = scale !== undefined ? Number(scale) : 1;
    }
  }
  Object.defineProperty(globalThis, "DOMMatrixReadOnly", { value: StubDOMMatrixReadOnly, writable: true });
}
if (typeof window !== "undefined" && !(window.SVGElement.prototype as { getBBox?: unknown }).getBBox) {
  (window.SVGElement.prototype as unknown as { getBBox: () => DOMRect }).getBBox = () => new DOMRect();
}
