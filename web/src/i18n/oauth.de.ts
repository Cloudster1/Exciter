import type { oauthEn } from "./oauth.en";

export const oauthDe: typeof oauthEn = {
  title: "App verbinden",
  asks: (name: string) => `${name} möchte in Exciter in deinem Namen arbeiten.`,
  mayDo: "Sie darf lesen und ändern, was du darfst, außer du schränkst sie hier ein.",
  address: "Adresse der App",
  knownRegistered: "Sie hat sich selbst bei Exciter registriert.",
  knownMetadata: "Exciter hat ihre Beschreibung von dieser Adresse gelesen.",
  goesBack: (host: string) => `Danach geht es zurück zu ${host}.`,
  askedToRead: "Sie hat nur ums Lesen gebeten.",
  readOnly: "Nur lesen",
  readOnlyHint: "Sie liest alles, was du lesen kannst, und ändert nichts.",
  projectsSome: "Die App erreicht nur die hier gewählten Projekte, nie mehr als du, und nichts, was alle betrifft.",
  allow: "Erlauben",
  deny: "Ablehnen",
  leaving: "Es geht zurück zur App.",
  expired: "Diese Anfrage ist abgelaufen oder wurde schon beantwortet. Beginne das Verbinden noch einmal in der App.",
  noRequest: "Diese Seite wurde ohne Anfrage geöffnet. Beginne das Verbinden in der App.",
  refused: {
    invalid_client:
      "Exciter kennt die App nicht, die dich hierher geschickt hat. Beginne das Verbinden noch einmal in der App; passiert es wieder, kann sich die App wohl nicht mit Exciter verbinden.",
    invalid_redirect:
      "Die App wollte dich an eine Adresse zurückschicken, die sie nie registriert hat, deshalb hat Exciter angehalten. Beginne das Verbinden noch einmal in der App.",
    switched_off:
      "Apps können sich gerade nicht per OAuth mit diesem Server verbinden; die Administratoren haben es ausgeschaltet. Frag sie, oder verbinde dich mit einem persönlichen Zugriffstoken.",
  } as Record<string, string | undefined>,
  refusedOther: "Die Anfrage der App konnte nicht angenommen werden. Beginne das Verbinden noch einmal in der App.",
  switchLabel: "Assistenten per OAuth verbinden lassen",
  switchIntro:
    "Ist das an, verbindet sich eine App wie ein KI-Assistent allein mit der Adresse von Exciter, und die Person stimmt im Browser zu. Ist es aus, wird jede so verbundene App abgewiesen, bis es wieder an ist.",
  switchedOn: "Assistenten können sich per OAuth verbinden.",
  switchedOff: "OAuth-Verbindungen sind aus.",
};
