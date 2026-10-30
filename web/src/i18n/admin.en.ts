/** The English strings of the Admin area: its page, its sections and the pages it holds. */
export const adminEn = {
  title: "Admin",
  intro: "Everything an administrator sets for everyone in this installation.",
  navLabel: "Admin sections",
  menuLabel: "Section",
  allSections: "All sections",
  /** How the palette names a page of the Admin area. */
  inPalette: (page: string) => `Admin / ${page}`,
  refusedTitle: "Only administrators can open this page",
  refusedBody: "Ask an administrator of this installation to make the change, or to give you the role.",
  sections: {
    people: "People and access",
    work: "Work",
    integrations: "Integrations",
    records: "Records",
  },
  pages: {
    installation: { label: "Installation", detail: "What the installation is called, the theme everybody sees until they choose, and the suite demo." },
    access: { label: "Access", detail: "Roles, what each one may do, the members and groups that hold them, and single sign-on." },
    users: { label: "Users", detail: "Who waits to be let in, adding people with a password, and switching an account off or back on." },
    linkTypes: { label: "Link types", detail: "The kinds of link every project offers between requirements." },
    armature: { label: "Armature", detail: "The connection to Armature: its address, its app for Exciter and its events." },
    stator: { label: "Stator", detail: "The connection to Stator: its address, its app for Exciter and its events." },
    connectedApps: { label: "Connected apps", detail: "The apps that may act for people here, and every token and grant they hold." },
    webhooks: { label: "Webhooks", detail: "Addresses Exciter tells when requirements, links and reviews change." },
    audit: { label: "Audit log", detail: "Who changed what of the installation, and when." },
  },
  users: {
    title: "Users",
    intro: "Let in who waits, add somebody with a password, and switch an account off so it cannot sign in, or back on. Roles are under Access.",
    accounts: "Accounts",
    columnState: "State",
    on: "Can sign in",
    empty: "Nobody is a member yet.",
  },
  installation: {
    defaultTheme: "Default theme",
    defaultThemeIntro: "What everybody sees until they choose a theme of their own. Any theme shared with everyone can be it.",
    builtIn: "The built-in theme",
    noShared: "No theme is shared with everyone yet. Share one under Themes, and it can be the default.",
  },
  connectedApps: {
    title: "Connected apps",
    intro: "The apps that may act for people here: the partner tools, the assistants people connected, and every token people made.",
  },
};
