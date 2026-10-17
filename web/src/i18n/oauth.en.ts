export const oauthEn = {
  title: "Connect an app",
  asks: (name: string) => `${name} wants to work in Exciter as you.`,
  mayDo: "It may read and change what you may, unless you limit it here.",
  address: "The app's address",
  knownRegistered: "It registered itself with Exciter.",
  knownMetadata: "Exciter read its description from this address.",
  goesBack: (host: string) => `You go back to ${host} afterwards.`,
  askedToRead: "It asked only to read.",
  readOnly: "Read only",
  readOnlyHint: "It reads everything you can and changes nothing.",
  projectsSome: "The app reaches only the projects picked here, never more than you can, and nothing that concerns everyone.",
  allow: "Allow",
  deny: "Deny",
  leaving: "Taking you back to the app.",
  expired: "This request to connect has expired or was answered already. Start connecting from the app again.",
  noRequest: "This page was opened without a request to connect. Start connecting from the app.",
  refused: {
    invalid_client:
      "Exciter does not know the app that sent you here. Start connecting from the app again; if it keeps happening, the app may not be able to connect to Exciter.",
    invalid_redirect: "The app asked to send you back to an address it never registered, so Exciter stopped. Start connecting from the app again.",
    switched_off:
      "Apps cannot connect to this server with OAuth right now; its administrators switched it off. Ask them, or connect with a personal access token.",
  } as Record<string, string | undefined>,
  refusedOther: "The app's request could not be accepted. Start connecting from the app again.",
  switchLabel: "Let assistants connect with OAuth",
  switchIntro:
    "With this on, an app such as an AI assistant connects with Exciter's address alone, and the person consents in the browser. Off, every app connected this way is refused until it is on again.",
  switchedOn: "Assistants can connect with OAuth.",
  switchedOff: "OAuth connections are off.",
};
