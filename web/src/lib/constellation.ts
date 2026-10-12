import { CONSTELLATION_FALLBACK_COLOUR, CONSTELLATION_LINK_PX, CONSTELLATION_POINTS_PER_MEGAPIXEL, CONSTELLATION_SPEED_PX_PER_S } from "@/config";

// A network drawn live: points wander, and a line joins two while they are near, fading with the distance. The
// colours are the theme's own tokens, read off the canvas, so a theme that recolours the column recolours it.

interface Point {
  x: number;
  y: number;
  vx: number;
  vy: number;
  r: number;
}

/** A CSS colour as canvas wants it, at the alpha given; unknown shapes fall back to grey. */
export function withAlpha(color: string, alpha: number): string {
  const c = color.trim();
  const hex = /^#([0-9a-f]{3,8})$/i.exec(c);
  if (hex) {
    let h = hex[1]!;
    if (h.length === 3 || h.length === 4)
      h = h
        .split("")
        .map((ch) => ch + ch)
        .join("");
    const r = parseInt(h.slice(0, 2), 16);
    const g = parseInt(h.slice(2, 4), 16);
    const b = parseInt(h.slice(4, 6), 16);
    return `rgb(${r} ${g} ${b} / ${alpha})`;
  }
  const rgb = /^rgba?\(\s*(\d+)[\s,]+(\d+)[\s,]+(\d+)/i.exec(c);
  if (rgb) return `rgb(${rgb[1]} ${rgb[2]} ${rgb[3]} / ${alpha})`;
  return withAlpha(CONSTELLATION_FALLBACK_COLOUR, alpha);
}

function tokens(el: HTMLElement): { node: string; line: string } {
  const style = getComputedStyle(el);
  return {
    node: style.getPropertyValue("--color-accent") || CONSTELLATION_FALLBACK_COLOUR,
    line: style.getPropertyValue("--color-border-strong") || CONSTELLATION_FALLBACK_COLOUR,
  };
}

/** Draws on the canvas until the function answered is called, over the column marked `data-backdrop-host`;
 * with reduced motion asked for one frame is drawn and left, and a hidden tab draws nothing until shown. */
export function startConstellation(canvas: HTMLCanvasElement): () => void {
  const ctx = canvas.getContext("2d");
  const host = canvas.closest<HTMLElement>("[data-backdrop-host]") ?? canvas.parentElement;
  if (!ctx || !host) return () => {};

  let width = 0;
  let height = 0;
  let dpr = 1;
  const points: Point[] = [];
  let frame = 0;
  let last = 0;
  let colours = tokens(canvas);
  let colourAt = 0;
  const still = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

  function seed(count: number) {
    while (points.length < count) {
      const angle = Math.random() * Math.PI * 2;
      const speed = CONSTELLATION_SPEED_PX_PER_S * (0.4 + Math.random() * 0.8);
      points.push({
        x: Math.random() * width,
        y: Math.random() * height,
        vx: Math.cos(angle) * speed,
        vy: Math.sin(angle) * speed,
        r: 1 + Math.random() * 1.6,
      });
    }
    points.length = Math.min(points.length, count);
  }

  function resize() {
    width = Math.max(1, host!.clientWidth);
    height = Math.max(1, host!.clientHeight);
    dpr = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = Math.floor(width * dpr);
    canvas.height = Math.floor(height * dpr);
    canvas.style.width = `${width}px`;
    canvas.style.height = `${height}px`;
    ctx!.setTransform(dpr, 0, 0, dpr, 0, 0);
    seed(Math.round(((width * height) / 1_000_000) * CONSTELLATION_POINTS_PER_MEGAPIXEL));
    for (const p of points) {
      p.x = Math.min(p.x, width);
      p.y = Math.min(p.y, height);
    }
  }

  function step(dt: number) {
    const reach = CONSTELLATION_LINK_PX;
    for (const p of points) {
      p.x += p.vx * dt;
      p.y += p.vy * dt;
      if (p.x < -reach) p.x = width + reach;
      if (p.x > width + reach) p.x = -reach;
      if (p.y < -reach) p.y = height + reach;
      if (p.y > height + reach) p.y = -reach;
    }
  }

  function draw() {
    const c = ctx!;
    c.clearRect(0, 0, width, height);
    const reach = CONSTELLATION_LINK_PX;
    c.lineWidth = 1;
    for (let i = 0; i < points.length; i++) {
      const a = points[i]!;
      for (let j = i + 1; j < points.length; j++) {
        const b = points[j]!;
        const dx = a.x - b.x;
        const dy = a.y - b.y;
        const d2 = dx * dx + dy * dy;
        if (d2 > reach * reach) continue;
        const near = 1 - Math.sqrt(d2) / reach;
        c.strokeStyle = withAlpha(colours.line, 0.55 * near);
        c.beginPath();
        c.moveTo(a.x, a.y);
        c.lineTo(b.x, b.y);
        c.stroke();
      }
    }
    c.fillStyle = withAlpha(colours.node, 0.75);
    for (const p of points) {
      c.beginPath();
      c.arc(p.x, p.y, p.r, 0, Math.PI * 2);
      c.fill();
    }
  }

  function tick(now: number) {
    frame = 0;
    if (document.hidden) return;
    const dt = last ? Math.min((now - last) / 1000, 0.1) : 0;
    last = now;
    if (now - colourAt > 1000) {
      colours = tokens(canvas);
      colourAt = now;
    }
    step(dt);
    draw();
    frame = requestAnimationFrame(tick);
  }

  function wake() {
    if (!frame && !document.hidden && !still) {
      last = 0;
      frame = requestAnimationFrame(tick);
    }
  }

  const observer =
    typeof ResizeObserver === "function"
      ? new ResizeObserver(() => {
          resize();
          if (still) draw();
        })
      : null;
  observer?.observe(host);
  resize();
  // One frame at once, so a tab that starts hidden or asks for stillness
  // still shows the network rather than nothing.
  draw();
  if (!still) frame = requestAnimationFrame(tick);
  document.addEventListener("visibilitychange", wake);

  return () => {
    if (frame) cancelAnimationFrame(frame);
    frame = 0;
    observer?.disconnect();
    document.removeEventListener("visibilitychange", wake);
    ctx.clearRect(0, 0, width, height);
  };
}
