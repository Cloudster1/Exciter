import type { ImportPerson } from "@/api/imports";
import { useSubjectSearch } from "@/api/permissions";
import { Card, SelectInput, Table, Td, Th } from "@/components/ui";
import { t } from "@/i18n";

/** The answer to a name of the file: a member's id, or nobody, which the API takes as an empty choice. */
export type PersonChoices = Record<string, { member?: string }>;

/** Who the file's names are in the project, Armature's card without making accounts. */
export function PeopleCard({ people, choices, onChange }: { people: ImportPerson[]; choices: PersonChoices; onChange: (choices: PersonChoices) => void }) {
  const { people: members } = useSubjectSearch("", people.length > 0, false);
  if (people.length === 0) return null;

  function valueFor(person: ImportPerson): string {
    const picked = choices[person.name];
    if (picked) return picked.member ?? "";
    return person.member ?? "";
  }

  return (
    <Card className="p-4" data-import-people>
      <h3 className="mb-1 text-sm font-medium text-ink">{t.imports.peopleTitle}</h3>
      <p className="mb-3 text-sm text-ink-muted">{t.imports.peopleIntro}</p>
      <Table dense aria-label={t.imports.peopleTitle}>
        <thead>
          <tr>
            <Th>{t.imports.inFile}</Th>
            <Th>{t.imports.rows}</Th>
            <Th>{t.imports.isPerson}</Th>
          </tr>
        </thead>
        <tbody>
          {people.map((person) => (
            <tr key={person.name} data-import-person={person.name}>
              <Td className="text-sm text-ink">{person.name}</Td>
              <Td className="text-sm text-ink-muted tabular-nums">{person.count}</Td>
              <Td>
                <SelectInput
                  aria-label={t.imports.whoIs(person.name)}
                  controlSize="sm"
                  value={valueFor(person)}
                  onChange={(event) => onChange({ ...choices, [person.name]: event.target.value ? { member: event.target.value } : {} })}
                  data-import-person-choice={person.name}
                >
                  <option value="">{t.imports.nobody}</option>
                  {person.member && !members.some((m) => m.id === person.member) && <option value={person.member}>{person.email}</option>}
                  {members.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.name} ({m.email})
                    </option>
                  ))}
                </SelectInput>
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>
    </Card>
  );
}
