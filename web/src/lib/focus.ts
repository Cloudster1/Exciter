import { useCallback, useEffect, useReducer, useRef } from "react";

type Find = () => HTMLElement | null | undefined;

/**
 * Focuses what `find` returns once a commit of the calling component has rendered it,
 * for targets that appear only after a mutation's state or refetched data lands.
 */
export function useFocusWhenRendered(): (find: Find) => void {
  const pending = useRef<Find>(undefined);
  const [, rerender] = useReducer((count: number) => count + 1, 0);
  // No dependencies on purpose: a refetch can commit the target a render or
  // two after the request, and a frame callback may run before either.
  useEffect(() => {
    const target = pending.current?.();
    if (!target) return;
    pending.current = undefined;
    target.focus();
  });
  return useCallback((find: Find) => {
    pending.current = find;
    rerender();
  }, []);
}
