import { CONFETTI_COLOURS, CONFETTI_GLYPHS, CONFETTI_GRAVITY_PX_PER_S2, CONFETTI_LIFE_MS, CONFETTI_PIECES_PER_CLICK } from "@/config";

// A burst of colour where the page is clicked: pieces fly out, tumble, fall and fade. Nothing is drawn
// between clicks, and nothing at all for anyone who asked their system for less motion.

export interface Piece {
  x: number;
  y: number;
  vx: number;
  vy: number;
  spin: number;
  angle: number;
  size: number;
  color: string;
  /** Some pieces are a character rather than a scrap of paper. */
  glyph?: string;
  born: number;
}

const COLORS = CONFETTI_COLOURS;
const GLYPHS = CONFETTI_GLYPHS;

/** The pieces one click throws from a point, at the moment given. */
export function burst(x: number, y: number, now: number, count = CONFETTI_PIECES_PER_CLICK): Piece[] {
  const out: Piece[] = [];
  for (let i = 0; i < count; i++) {
    const angle = (Math.PI * 2 * i) / count + Math.random() * 0.4;
    const speed = 260 + Math.random() * 420;
    const glyph = i % 4 === 0 ? GLYPHS[(i / 4 + Math.floor(Math.random() * GLYPHS.length)) % GLYPHS.length] : undefined;
    out.push({
      x,
      y,
      vx: Math.cos(angle) * speed,
      vy: Math.sin(angle) * speed - 240,
      spin: (Math.random() - 0.5) * 14,
      angle: Math.random() * Math.PI,
      size: glyph ? 18 + Math.random() * 10 : 6 + Math.random() * 8,
      color: COLORS[i % COLORS.length]!,
      glyph,
      born: now,
    });
  }
  return out;
}

/** Moves every piece on by dt seconds and drops the ones whose life is over. */
export function step(pieces: Piece[], dt: number, now: number): Piece[] {
  const alive: Piece[] = [];
  for (const p of pieces) {
    if (now - p.born > CONFETTI_LIFE_MS) continue;
    p.vy += CONFETTI_GRAVITY_PX_PER_S2 * dt;
    p.vx *= 0.985;
    p.x += p.vx * dt;
    p.y += p.vy * dt;
    p.angle += p.spin * dt;
    alive.push(p);
  }
  return alive;
}

/** Starts listening for clicks and drawing on the canvas; returns what stops it. */
export function startConfetti(canvas: HTMLCanvasElement): () => void {
  const ctx = canvas.getContext("2d");
  const still = typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (!ctx || still) return () => {};

  let pieces: Piece[] = [];
  let frame = 0;
  let last = 0;

  function fit() {
    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.floor(window.innerWidth * dpr);
    canvas.height = Math.floor(window.innerHeight * dpr);
    ctx!.setTransform(dpr, 0, 0, dpr, 0, 0);
  }

  function draw(now: number) {
    const dt = last ? Math.min((now - last) / 1000, 0.05) : 0;
    last = now;
    pieces = step(pieces, dt, now);
    ctx!.clearRect(0, 0, window.innerWidth, window.innerHeight);
    for (const p of pieces) {
      const life = 1 - (now - p.born) / CONFETTI_LIFE_MS;
      ctx!.save();
      ctx!.globalAlpha = Math.max(0, Math.min(1, life * 1.4));
      ctx!.translate(p.x, p.y);
      ctx!.rotate(p.angle);
      if (p.glyph) {
        ctx!.font = `${p.size}px serif`;
        ctx!.textAlign = "center";
        ctx!.textBaseline = "middle";
        ctx!.fillText(p.glyph, 0, 0);
      } else {
        ctx!.fillStyle = p.color;
        ctx!.fillRect(-p.size / 2, -p.size / 4, p.size, p.size / 2);
      }
      ctx!.restore();
    }
    if (pieces.length) frame = requestAnimationFrame(draw);
    else {
      frame = 0;
      last = 0;
    }
  }

  function onPointerDown(event: PointerEvent) {
    if (event.button !== 0) return;
    pieces.push(...burst(event.clientX, event.clientY, performance.now()));
    if (!frame) frame = requestAnimationFrame(draw);
  }

  fit();
  window.addEventListener("resize", fit);
  window.addEventListener("pointerdown", onPointerDown, { passive: true });
  return () => {
    window.removeEventListener("resize", fit);
    window.removeEventListener("pointerdown", onPointerDown);
    if (frame) cancelAnimationFrame(frame);
  };
}
