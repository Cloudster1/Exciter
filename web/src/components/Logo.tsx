import { LOGO_WIDTH_PX, MARK_WIDTH_PX } from "@/config";

/** The whole picture, for a page with room for it. Decorative: the name is written beside it. */
export function Logo({ className }: { className?: string }) {
  return (
    <img
      src="/logo-256.webp"
      srcSet="/logo-128.webp 128w, /logo-256.webp 256w, /logo-512.webp 512w"
      sizes={`${LOGO_WIDTH_PX}px`}
      width={LOGO_WIDTH_PX}
      height={LOGO_WIDTH_PX}
      alt=""
      className={className}
      data-logo
    />
  );
}

/** The gopher's face alone, which still reads at the size of a line of text. */
export function LogoMark({ className }: { className?: string }) {
  return <img src="/mark-96.webp" width={MARK_WIDTH_PX} height={MARK_WIDTH_PX} alt="" className={className} data-logo-mark />;
}
