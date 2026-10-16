import type { reuseEn } from "./reuse.en";

/** The German strings of reuse and variants, held to the English shape. */
export const reuseDe: typeof reuseEn = {
  reuse: "In einem Projekt wiederverwenden",
  title: (key: string) => `${key} wiederverwenden`,
  intro: "Die Kopie folgt Titel, Aussage und Begründung dieser Anforderung. Typ, Werte, Status und Verknüpfungen gehören dem anderen Projekt.",
  children: "Auch alles darunter wiederverwenden",
  submit: "Wiederverwenden",
  noTarget: "Sie bearbeiten kein anderes Projekt, in dem Sie sie wiederverwenden könnten.",
  following: (key: string, revision: number) => `Folgt ${key} aus der Bibliothek, in deren Revision ${revision}.`,
  followingHidden: (revision: number) => `Folgt einer Bibliotheksanforderung, die Sie nicht lesen dürfen, in deren Revision ${revision}.`,
  variant: (key: string, revision: number) => `Eine Variante von ${key} aus der Bibliothek, abgezweigt bei deren Revision ${revision}.`,
  variantHidden: (revision: number) => `Eine Variante einer Bibliotheksanforderung, die Sie nicht lesen dürfen, abgezweigt bei deren Revision ${revision}.`,
  behind: (key: string, revision: number) => `${key} hat jetzt Revision ${revision}.`,
  whatChanged: "Was sich in der Quelle geändert hat",
  take: (revision: number) => `Revision ${revision} übernehmen`,
  takeAgain: (revision: number) => `Revision ${revision} übernehmen und wieder folgen`,
  makeVariant: "Zur Variante machen",
  taken: (revision: number) => `Revision ${revision} der Quelle übernommen.`,
  madeVariant: "Dies ist jetzt eine Variante. Ihre Worte sind ihre eigenen.",
  locked: "Titel, Aussage und Begründung folgen der Bibliotheksquelle. Machen Sie dies zur Variante, um sie hier zu ändern.",
  badgeFollowing: "Wiederverwendet",
  badgeBehind: "Hinter der Quelle",
  badgeVariant: "Variante",
};
