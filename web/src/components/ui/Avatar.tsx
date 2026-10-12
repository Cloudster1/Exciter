import { useState } from "react";
import { t } from "@/i18n";
import { cx } from "./cx";

export type AvatarSize = "xs" | "sm" | "md" | "lg";

const sizes: Record<AvatarSize, string> = {
  xs: "size-4 text-[9px]",
  sm: "size-5 text-[9px]",
  md: "size-6 text-2xs",
  lg: "size-16 text-xl",
};

// A person's picture when they have one, their initials when they do not, and
// the initials again if the picture fails, so a row never shows a broken image.
export function Avatar({ name, src, size = "md", className }: { name?: string; src?: string; size?: AvatarSize; className?: string }) {
  const [broken, setBroken] = useState(false);
  if (!name) {
    return (
      <span
        title={t.common.unassigned}
        className={cx(
          "inline-flex shrink-0 items-center justify-center rounded-full border border-dashed border-border-strong text-ink-subtle",
          sizes[size],
          className,
        )}
      >
        <span aria-hidden="true">?</span>
        <span className="sr-only">{t.common.unassigned}</span>
      </span>
    );
  }
  if (src && !broken) {
    return (
      <img
        src={src}
        alt={name}
        title={name}
        onError={() => setBroken(true)}
        className={cx("inline-block shrink-0 rounded-full object-cover", sizes[size], className)}
      />
    );
  }
  const initials = name
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("");
  return (
    <span
      title={name}
      className={cx("inline-flex shrink-0 items-center justify-center rounded-full bg-primary font-medium text-on-primary", sizes[size], className)}
    >
      <span aria-hidden="true">{initials}</span>
      <span className="sr-only">{name}</span>
    </span>
  );
}
