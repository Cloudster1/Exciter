import { ReactRenderer } from "@tiptap/react";
import type { MentionNodeAttrs } from "@tiptap/extension-mention";
import type { SuggestionOptions } from "@tiptap/suggestion";
import type { Mentionable } from "./extensions";
import { MentionList, type MentionListHandle } from "./MentionList";

/** Who an at sign may name, for what was typed after it. */
export type MentionSource = (query: string) => Promise<Mentionable[]>;

/** The at sign's suggestion: people found as the author types, offered under the caret. */
export function mentionSuggestion(find: MentionSource): Omit<SuggestionOptions<Mentionable, MentionNodeAttrs>, "editor"> {
  return {
    items: ({ query }) => find(query),
    render: () => {
      let renderer: ReactRenderer<MentionListHandle> | null = null;
      const close = () => {
        renderer?.element.remove();
        renderer?.destroy();
        renderer = null;
      };
      return {
        onStart: (props) => {
          renderer = new ReactRenderer(MentionList, { props, editor: props.editor });
          document.body.appendChild(renderer.element);
        },
        onUpdate: (props) => renderer?.updateProps(props),
        onKeyDown: ({ event }) => {
          if (event.key === "Escape") {
            close();
            return true;
          }
          return renderer?.ref?.onKeyDown(event) ?? false;
        },
        onExit: close,
      };
    },
  };
}
