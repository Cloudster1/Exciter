import type { ReactNode } from "react";
import { Button, cx } from "@/components/ui";
import { Icon } from "@/components/icons";

/** A titled fold in the sidebar. The title is the switch, so a group takes no extra row; the chevron says which way it is. */
export function SidebarGroup({
  id,
  title,
  open,
  onToggle,
  nested = false,
  children,
}: {
  id: string;
  title: string;
  open: boolean;
  onToggle: () => void;
  nested?: boolean;
  children: ReactNode;
}) {
  return (
    <div className={cx(nested ? "mt-1" : "mb-3")} data-sidebar-group={id} data-open={open}>
      <Button
        variant="ghost"
        size="sm"
        onClick={onToggle}
        aria-expanded={open}
        aria-controls={`sidebar-group-${id}`}
        className="w-full justify-between px-2! text-2xs font-medium tracking-wide text-ink-subtle uppercase"
        iconRight={<Icon.ChevronDown className={cx("shrink-0 transition-transform", !open && "-rotate-90")} />}
      >
        <span className="min-w-0 flex-1 truncate text-left">{title}</span>
      </Button>
      {/* Hidden rather than unmounted, so aria-controls always names an element that exists. */}
      <div id={`sidebar-group-${id}`} hidden={!open} className={cx(nested && "ml-2 border-l border-border/60 pl-1")}>
        {children}
      </div>
    </div>
  );
}
