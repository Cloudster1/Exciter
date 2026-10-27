import { ReactRenderer } from "@tiptap/react";
import type { MentionNodeAttrs } from "@tiptap/extension-mention";
import type { SuggestionOptions } from "@tiptap/suggestion";
import type { Mentionable } from "./extensions";
import { MentionList, type MentionListHandle } from "./MentionList";

/** Who an at sign may name, for what was typed after it. */
export type MentionSource = (query: string) => Promise<Mentionable[]>;

/** Where the list goes: in the landmark the editor is in, so a screen reader finds it where the author writes. Neither
 *  a dialog nor main transforms or filters itself, so the list stays fixed to the viewport as the caret's rectangle is. */
function landmarkOf(dom: Element): Element {
  return dom.closest('[role="dialog"], dialog, main') ?? document.body;
}

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
          landmarkOf(props.editor.view.dom).appendChild(renderer.element);
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
