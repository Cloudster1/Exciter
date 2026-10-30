import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useTypes } from "@/api/attributes";
import { COUNTS, SUSPECT_ENDS, useLinkTypeActions, type Counts, type LinkType, type LinkTypeRule, type SuspectEnd } from "@/api/links";
import { Button, Checkbox, Dialog, ErrorBanner, Field, IconButton, Select, SelectInput } from "@/components/ui";
import { Icon } from "@/components/icons";
import { LINK_TYPE_KEY_MAX_LENGTH, LINK_TYPE_MAX_RULES, LINK_TYPE_NAME_MAX_LENGTH, LINK_TYPE_PHRASE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";

/** Makes a link type, the installation's or with a project the project's own, or changes the one given. */
export function LinkTypeForm({ projectKey, type, onDone }: { projectKey?: string; type?: LinkType; onDone: () => void }) {
  const actions = useLinkTypeActions(projectKey);
  const save = type ? actions.update : actions.create;
  const [name, setName] = useState(type?.name ?? "");
  const [key, setKey] = useState(type?.key ?? "");
  const [forward, setForward] = useState(type?.forward ?? "");
  const [backward, setBackward] = useState(type?.backward ?? "");
  const [acyclic, setAcyclic] = useState(type?.acyclic ?? false);
  const [suspectEnd, setSuspectEnd] = useState<SuspectEnd>(type?.suspectEnd ?? "none");
  const [counts, setCounts] = useState<Counts>(type?.counts ?? "nothing");
  const fields = save.error instanceof ApiError ? save.error.fields : {};

  function submit(event: FormEvent) {
    event.preventDefault();
    const body = { name, forward, backward, acyclic, suspectEnd, counts };
    if (type) actions.update.mutate({ id: type.id, ...body }, { onSuccess: () => onDone() });
    else actions.create.mutate({ ...body, key: key.trim() || undefined }, { onSuccess: () => onDone() });
  }

  return (
    <form onSubmit={submit} className="space-y-3" noValidate data-link-type-form>
      {save.error && Object.keys(fields).length === 0 && <ErrorBanner>{save.error.message}</ErrorBanner>}
      <div className="grid gap-3 sm:grid-cols-2">
        <Field
          label={t.linkTypes.name}
          value={name}
          maxLength={LINK_TYPE_NAME_MAX_LENGTH}
          onChange={(event) => setName(event.target.value)}
          error={fields.name}
          autoFocus
          required
        />
        <Field
          label={t.linkTypes.key}
          hint={t.linkTypes.keyHint}
          value={key}
          maxLength={LINK_TYPE_KEY_MAX_LENGTH}
          className="font-mono"
          readOnly={Boolean(type)}
          onChange={(event) => setKey(event.target.value.toLowerCase())}
          error={fields.key}
        />
        <Field
          label={t.linkTypes.forward}
          hint={t.linkTypes.forwardHint}
          value={forward}
          maxLength={LINK_TYPE_PHRASE_MAX_LENGTH}
          onChange={(event) => setForward(event.target.value)}
          error={fields.forward}
          required
        />
        <Field
          label={t.linkTypes.backward}
          hint={t.linkTypes.backwardHint}
          value={backward}
          maxLength={LINK_TYPE_PHRASE_MAX_LENGTH}
          onChange={(event) => setBackward(event.target.value)}
          error={fields.backward}
          required
        />
        <Select
          label={t.linkTypes.suspectEnd}
          value={suspectEnd}
          onChange={(event) => setSuspectEnd(event.target.value as SuspectEnd)}
          error={fields.suspectEnd}
        >
          {SUSPECT_ENDS.map((each) => (
            <option key={each} value={each}>
              {t.linkTypes.suspectEnds[each]}
            </option>
          ))}
        </Select>
        <Select label={t.linkTypes.counts} value={counts} onChange={(event) => setCounts(event.target.value as Counts)} error={fields.counts}>
          {COUNTS.map((each) => (
            <option key={each} value={each}>
              {t.linkTypes.countsValues[each]}
            </option>
          ))}
        </Select>
      </div>
      <Checkbox label={t.linkTypes.acyclic} checked={acyclic} onChange={(event) => setAcyclic(event.target.checked)} data-link-type-acyclic />
      <div className="flex gap-2">
        <Button type="submit" loading={save.isPending} data-action="save-link-type">
          {t.linkTypes.save}
        </Button>
        <Button variant="secondary" onClick={onDone} data-action="cancel-link-type">
          {t.linkTypes.cancel}
        </Button>
      </div>
    </form>
  );
}

/** The pairs of the project's requirement types a link type joins there; none is any pair. */
export function PairsDialog({ projectKey, type, onClose }: { projectKey: string; type: LinkType; onClose: () => void }) {
  const { data: types } = useTypes(projectKey);
  const { use } = useLinkTypeActions(projectKey);
  const [pairs, setPairs] = useState<LinkTypeRule[]>(type.rules);
  const fields = use.error instanceof ApiError ? use.error.fields : {};
  const offered = (types ?? []).filter((each) => !each.retired || pairs.some((pair) => pair.sourceTypeId === each.id || pair.targetTypeId === each.id));
  const change = (index: number, patch: Partial<LinkTypeRule>) => setPairs(pairs.map((pair, i) => (i === index ? { ...pair, ...patch } : pair)));

  const end = (label: string, value: string | null, onChange: (value: string | null) => void) => (
    <SelectInput aria-label={label} value={value ?? ""} onChange={(event) => onChange(event.target.value || null)} className="w-auto">
      <option value="">{t.linkTypes.anyType}</option>
      {offered.map((each) => (
        <option key={each.id} value={each.id}>
          {each.name}
        </option>
      ))}
    </SelectInput>
  );

  return (
    <Dialog open title={t.linkTypes.pairsTitle(type.name)} onClose={onClose} attrs={{ "data-pairs-dialog": "" }}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          use.mutate({ id: type.id, enabled: type.enabled, rules: pairs }, { onSuccess: onClose });
        }}
        className="space-y-3"
      >
        <p className="text-sm text-ink-muted">{t.linkTypes.pairsIntro}</p>
        {use.error && <ErrorBanner>{fields.rules ?? use.error.message}</ErrorBanner>}
        <ol className="space-y-2">
          {pairs.map((pair, index) => (
            <li key={`${index}:${pair.sourceTypeId}:${pair.targetTypeId}`} className="flex flex-wrap items-center gap-2" data-pair={index}>
              {end(`${t.linkTypes.from} ${index + 1}`, pair.sourceTypeId, (value) => change(index, { sourceTypeId: value }))}
              <Icon.ChevronDown className="-rotate-90 text-ink-subtle" aria-hidden />
              {end(`${t.linkTypes.to} ${index + 1}`, pair.targetTypeId, (value) => change(index, { targetTypeId: value }))}
              <IconButton icon={<Icon.X />} label={t.linkTypes.removePair(index + 1)} size="sm" onClick={() => setPairs(pairs.filter((_, i) => i !== index))} />
            </li>
          ))}
        </ol>
        <Button
          variant="secondary"
          icon={<Icon.Plus />}
          disabled={pairs.length >= LINK_TYPE_MAX_RULES}
          onClick={() => setPairs([...pairs, { sourceTypeId: null, targetTypeId: null }])}
          data-action="add-pair"
        >
          {t.linkTypes.addPair}
        </Button>
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t.linkTypes.cancel}
          </Button>
          <Button type="submit" loading={use.isPending} data-action="save-pairs">
            {t.linkTypes.savePairs}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
