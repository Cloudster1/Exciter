// jsdom lays nothing out, so every measurement is an empty box; without elementFromPoint
// axe silently skips page-has-heading-one, so setup.ts loads this for every test.
const noRects = (): DOMRectList => Object.assign([], { item: () => null }) as unknown as DOMRectList;
Range.prototype.getBoundingClientRect ??= () => new DOMRect();
Range.prototype.getClientRects ??= noRects;
document.elementFromPoint ??= () => null;
