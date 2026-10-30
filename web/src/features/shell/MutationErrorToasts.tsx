import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useToast } from "@/components/ui";
import { onUnreportedMutationError } from "@/lib/session";

// A change the server refuses would otherwise flip back without a word, so
// the server's sentence is said wherever the change's own control is silent.
export function MutationErrorToasts() {
  const queryClient = useQueryClient();
  const toast = useToast();
  useEffect(() => onUnreportedMutationError(queryClient, (message) => toast.error(message)), [queryClient, toast]);
  return null;
}
