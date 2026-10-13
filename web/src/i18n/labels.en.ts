/** The strings of labels on requirements, the label box and a label's own page. */
export const labelsEn = {
  title: "Labels",
  add: "Add a label",
  placeholder: "Type a label",
  options: "Labels to pick",
  create: (name: string) => `Add ${name}`,
  remove: (name: string) => `Remove label ${name}`,
  added: (name: string) => `Added the label ${name}.`,
  removed: (name: string) => `Removed the label ${name}.`,
  none: "No labels yet.",
  loading: "Looking for labels",
  noMatch: "No label starts with that yet.",
  invalid: "Use letters, digits, hyphens, underscores and dots, and start with a letter or a digit.",
  tooLong: (max: number) => `Keep a label to ${max} characters.`,
  used: (requirements: number) => (requirements === 1 ? "on 1 requirement" : `on ${requirements} requirements`),
  heading: (name: string) => `Label ${name}`,
  inProject: (project: string) => `Requirements in ${project} with this label`,
  everywhere: "Requirements in every project with this label",
  showEverywhere: "Show every project",
  filtered: (name: string) => `Showing requirements labelled ${name}`,
  clearFilter: "Show all requirements",
  count: (total: number, from: number, to: number) =>
    total === 0
      ? "No requirements"
      : total <= to && from === 1
        ? total === 1
          ? "1 requirement"
          : `${total} requirements`
        : `${from} to ${to} of ${total} requirements`,
  emptyTitle: "No requirement carries this label",
  emptyBody: "Put the label on a requirement from the requirement itself, or check how it is spelled.",
  failed: "The requirements with this label could not be loaded. Try again in a moment.",
  changed: (who: string, when: string) => (who ? `${who} changed it on ${when}` : `Changed on ${when}`),
  paging: "Pages of this list",
  previous: "Previous",
  next: "Next",
  pageOf: (page: number, pages: number) => `Page ${page} of ${pages}`,
};
