import { useEffect, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api } from "./client";

/** A diagram drawn as SVG by the server's renderer (#284). */
export async function drawDiagram(language: string, source: string): Promise<string> {
  return (await api.POST("/diagrams", { body: { language, source } })).data!.svg;
}

export const diagramQueryKey = (language: string, source: string) => ["diagram", language, source] as const;

/** A diagram's SVG once its source stood still for the delay: never asked twice, since it changes only with its
 * source, and the last one stays while the next is drawn, so a preview does not flicker. */
export function useDiagram(language: string, source: string, delay = 0) {
  const [asked, setAsked] = useState(delay > 0 ? "" : source);
  useEffect(() => {
    if (delay <= 0) {
      setAsked(source);
      return;
    }
    const timer = setTimeout(() => setAsked(source), delay);
    return () => clearTimeout(timer);
  }, [source, delay]);
  return useQuery({
    queryKey: diagramQueryKey(language, asked),
    queryFn: () => drawDiagram(language, asked),
    enabled: asked.trim() !== "",
    staleTime: Number.POSITIVE_INFINITY,
    placeholderData: keepPreviousData,
  });
}
