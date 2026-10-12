// Geometry of the graphs the app draws by hand, the workflow designer first: plain data and pure functions, so
// a picture can be tested without a browser.

export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface Point {
  x: number;
  y: number;
}

/** An edge as the bending needs it: which two boxes, and a key of its own. */
export interface Connection {
  key: string;
  from: string;
  to: string;
}

/** How far each edge bows out: a lone edge runs straight, two between the same boxes in opposite directions
 * bend to their own sides so neither hides the other, and further ones bend further, by step each time. */
export function bends(edges: Connection[], step: number): Map<string, number> {
  const out = new Map<string, number>();
  const groups = new Map<string, Connection[]>();
  for (const edge of edges) {
    const pair = [edge.from, edge.to].sort().join("|");
    groups.set(pair, [...(groups.get(pair) ?? []), edge]);
  }
  for (const group of groups.values()) {
    const forward = group.filter((edge) => edge.from <= edge.to);
    const backward = group.filter((edge) => edge.from > edge.to);
    const twoWay = forward.length > 0 && backward.length > 0;
    // The curve bows to the right of its own direction, so the same sign on
    // an edge running the other way is already the other side of the line.
    const spread = (list: Connection[]) => {
      list.forEach((edge, index) => {
        // With traffic both ways, nothing runs straight; alone, the first does.
        const bend = twoWay ? index + 1 : Math.ceil(index / 2) * (index % 2 === 0 ? -1 : 1);
        out.set(edge.key, bend * step || 0);
      });
    };
    spread(forward);
    spread(backward);
  }
  return out;
}

export interface EdgeShape {
  /** An SVG path from the border of one box to the border of the other. */
  path: string;
  /** Where a label sits, at the middle of the curve. */
  label: Point;
}

function centre(rect: Rect): Point {
  return { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 };
}

/** The point where a line from a rect's centre towards a target leaves the rect. */
export function clipToRect(rect: Rect, towards: Point): Point {
  const c = centre(rect);
  const dx = towards.x - c.x;
  const dy = towards.y - c.y;
  if (dx === 0 && dy === 0) return c;
  const scale = Math.min(dx === 0 ? Infinity : rect.width / 2 / Math.abs(dx), dy === 0 ? Infinity : rect.height / 2 / Math.abs(dy));
  return { x: c.x + dx * scale, y: c.y + dy * scale };
}

/** Halfway along a curve, where its label goes. */
const MIDDLE = 0.5;

/** A quadratic curve's coordinate at t, from its start, control and end. */
export function along(start: number, control: number, end: number, t: number): number {
  return (1 - t) * (1 - t) * start + 2 * (1 - t) * t * control + t * t * end;
}

/** A quadratic curve between two rects, bowing out by `bend` pixels to one side. */
export function edgeShape(from: Rect, to: Rect, bend: number): EdgeShape {
  const a = centre(from);
  const b = centre(to);
  const length = Math.hypot(b.x - a.x, b.y - a.y) || 1;
  // The control point sits off the midpoint, perpendicular to the straight line.
  const control = {
    x: (a.x + b.x) / 2 + (-(b.y - a.y) / length) * bend,
    y: (a.y + b.y) / 2 + ((b.x - a.x) / length) * bend,
  };
  const start = clipToRect(from, control);
  const end = clipToRect(to, control);
  const label = { x: along(start.x, control.x, end.x, MIDDLE), y: along(start.y, control.y, end.y, MIDDLE) };
  const round = (n: number) => Math.round(n * 10) / 10;
  return {
    path: `M ${round(start.x)} ${round(start.y)} Q ${round(control.x)} ${round(control.y)} ${round(end.x)} ${round(end.y)}`,
    label,
  };
}
