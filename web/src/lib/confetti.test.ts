import { describe, expect, it } from "vitest";
import { burst, step } from "./confetti";
import { CONFETTI_LIFE_MS, CONFETTI_PIECES_PER_CLICK } from "@/config";

describe("confetti", () => {
  it("throws a click's worth of pieces from the point clicked, some of them characters", () => {
    const pieces = burst(100, 50, 0);
    expect(pieces).toHaveLength(CONFETTI_PIECES_PER_CLICK);
    expect(pieces.every((p) => p.x === 100 && p.y === 50)).toBe(true);
    expect(pieces.some((p) => p.glyph)).toBe(true);
    expect(pieces.some((p) => !p.glyph)).toBe(true);
  });

  it("lets the pieces fall and drops them when their life is over", () => {
    let pieces = burst(0, 0, 0);
    const before = pieces.map((p) => p.vy);
    pieces = step(pieces, 0.1, 100);
    expect(pieces).toHaveLength(CONFETTI_PIECES_PER_CLICK);
    expect(pieces.every((p, i) => p.vy > before[i]!)).toBe(true);
    expect(step(pieces, 0.1, CONFETTI_LIFE_MS + 1)).toHaveLength(0);
  });
});
