import type { AnchorHTMLAttributes, ButtonHTMLAttributes, ReactNode, Ref } from "react";
import { cx } from "./cx";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "link";
export type ControlSize = "sm" | "md" | "lg";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  ref?: Ref<HTMLButtonElement>;
  variant?: ButtonVariant;
  size?: ControlSize;
  loading?: boolean;
  /** An icon before the label; the label stays, so the button keeps its name. */
  icon?: ReactNode;
  iconRight?: ReactNode;
};

// Primary is ink, not the accent: the accent marks what is current, and a
// page that uses one colour for "this" and "do this" makes both harder to find.
const buttonVariants: Record<ButtonVariant, string> = {
  primary: "bg-primary text-on-primary hover:bg-primary-hover",
  secondary: "bg-surface text-ink border border-border-strong/70 hover:border-border-strong hover:bg-surface-raised",
  ghost: "text-ink-muted hover:bg-surface-raised hover:text-ink",
  danger: "bg-danger text-on-danger hover:bg-danger-hover",
  link: "text-ink-muted underline-offset-2 hover:text-ink hover:underline",
};

export const controlHeight: Record<ControlSize, string> = {
  sm: "h-7 text-sm",
  md: "h-8 text-sm",
  lg: "h-9 text-base",
};

const buttonPadding: Record<ControlSize, string> = { sm: "px-2.5", md: "px-3", lg: "px-4" };

export function Button({
  variant = "primary",
  size = "md",
  loading = false,
  icon,
  iconRight,
  className,
  children,
  disabled,
  type = "button",
  ...rest
}: ButtonProps) {
  return (
    <button
      {...rest}
      // Without a type the browser makes it a submit button, and any button
      // that happens to sit inside a form would submit it.
      type={type}
      disabled={disabled || loading}
      // aria-busy rather than swapping the label, so a screen reader announces
      // the state change without the button losing its accessible name.
      aria-busy={loading}
      className={cx(
        "inline-flex items-center justify-center gap-1.5 rounded-control font-medium whitespace-nowrap transition-colors",
        "disabled:cursor-not-allowed disabled:opacity-50",
        // A link variant is text in a sentence: no height and no padding of its own.
        variant === "link" ? "text-sm" : cx(controlHeight[size], buttonPadding[size]),
        buttonVariants[variant],
        className,
      )}
    >
      {loading ? <Spinner /> : icon}
      {children}
      {iconRight}
    </button>
  );
}

/** A link drawn as a button, for a destination the browser handles itself: a download, a file. */
export function ButtonLink({
  variant = "secondary",
  size = "md",
  icon,
  className,
  children,
  ...rest
}: AnchorHTMLAttributes<HTMLAnchorElement> & { variant?: ButtonVariant; size?: ControlSize; icon?: ReactNode }) {
  return (
    <a
      {...rest}
      className={cx(
        "inline-flex items-center justify-center gap-1.5 rounded-control font-medium whitespace-nowrap no-underline transition-colors",
        variant === "link" ? "text-sm" : cx(controlHeight[size], buttonPadding[size]),
        buttonVariants[variant],
        className,
      )}
    >
      {icon}
      {children}
    </a>
  );
}

/** A button that is only an icon. The label is required: it is the name. */
export function IconButton({
  icon,
  label,
  variant = "ghost",
  size = "md",
  className,
  type = "button",
  ...rest
}: Omit<ButtonProps, "children" | "icon" | "iconRight" | "size"> & { icon: ReactNode; label: string; size?: ControlSize | "xs" }) {
  // xs sits inside a table row or a chart label, where a full control would not.
  const square: Record<ControlSize | "xs", string> = { xs: "size-5 rounded", sm: "size-7", md: "size-8", lg: "size-9" };
  return (
    <button
      {...rest}
      type={type}
      aria-label={label}
      title={label}
      className={cx(
        "inline-flex shrink-0 items-center justify-center rounded-control transition-colors",
        "disabled:cursor-not-allowed disabled:opacity-50",
        square[size],
        buttonVariants[variant],
        className,
      )}
    >
      {icon}
      <span className="sr-only">{label}</span>
    </button>
  );
}

export function Spinner({ className }: { className?: string }) {
  return <span aria-hidden="true" className={cx("size-3 animate-spin rounded-full border-[1.5px] border-current border-t-transparent", className)} />;
}
