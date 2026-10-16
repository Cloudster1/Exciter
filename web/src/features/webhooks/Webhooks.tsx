import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import {
  useCreateWebhook,
  useDeleteWebhook,
  useRedeliverWebhook,
  useRotateWebhookSecret,
  useTestWebhook,
  useUpdateWebhook,
  useWebhookDeliveries,
  useWebhooks,
  type Webhook,
  type WebhookDelivery,
  type WebhookTopic,
} from "@/api/webhooks";
import { useProjects } from "@/api/projects";
import { Button, Card, Checkbox, Dialog, EmptyState, ErrorBanner, Field, IconButton, Menu, PageHeader, Select, Table, Tag, Td, Th, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import {
  COPY_FEEDBACK_MS,
  WEBHOOK_NAME_MAX_LENGTH,
  WEBHOOK_SIGNATURE_HEADER,
  WEBHOOK_TIMESTAMP_HEADER,
  WEBHOOK_TIMESTAMPED_SIGNATURE_HEADER,
  WEBHOOK_DEFAULT_VERSION,
  WEBHOOK_TOPIC_ANY,
  WEBHOOK_TOPICS,
  WEBHOOK_URL_MAX_LENGTH,
  WEBHOOK_VERSIONS,
} from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";

// Adapted from Stator's webhooks page (#48): the secret shown once as a new token is, the log in a dialog, and the
// projects a webhook listens to.

const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** A topic in the reader's words; a ping and anything newer than this client in the API's. */
export function topicName(topic: string): string {
  if (topic === WEBHOOK_TOPIC_ANY) return t.webhooks.everything;
  if (topic === "ping") return t.webhooks.ping;
  return (t.webhooks.topicNames as Record<string, string>)[topic] ?? topic;
}

type Version = (typeof WEBHOOK_VERSIONS)[number];

/** An envelope version in the reader's words; one newer than this client by its number. */
export function versionName(version: number): string {
  return (t.webhooks.versionNames as Record<number, string>)[version] ?? String(version);
}

/** What one attempt came to, in a sentence for its row. */
export function deliveryDetail(d: WebhookDelivery): string {
  if (d.state === "delivered" && d.status !== null) return t.webhooks.answered(d.status);
  if (d.state === "pending" && d.nextAttemptAt) return t.webhooks.nextTry(when.format(new Date(d.nextAttemptAt)));
  return d.error;
}

/** The installation's webhooks, for its administrators. */
export function Webhooks() {
  const { data, isLoading, error } = useWebhooks();
  const update = useUpdateWebhook();
  const remove = useDeleteWebhook();
  const rotate = useRotateWebhookSecret();
  const test = useTestWebhook();
  const [fresh, setFresh] = useState<{ name: string; secret: string } | null>(null);
  const [notice, setNotice] = useState("");
  const [logOf, setLogOf] = useState<Webhook | null>(null);
  const hooks = data ?? [];
  const failure = error ?? update.error ?? remove.error ?? rotate.error ?? test.error;

  function toggle(hook: Webhook) {
    update.mutate(
      { id: hook.id, name: hook.name, url: hook.url, topics: hook.topics, projects: hook.projects.map((p) => p.key), enabled: !hook.enabled },
      { onSuccess: (saved) => setNotice(saved.enabled ? t.webhooks.turnedOn(saved.name) : t.webhooks.turnedOff(saved.name)) },
    );
  }

  // The version is the one thing an administrator changes on a saved webhook besides turning it off; the rest stays.
  function switchVersion(hook: Webhook, version: Version) {
    update.mutate(
      { id: hook.id, name: hook.name, url: hook.url, topics: hook.topics, projects: hook.projects.map((p) => p.key), version },
      { onSuccess: (saved) => setNotice(t.webhooks.versionSwitched(saved.name, versionName(saved.version))) },
    );
  }

  function ping(hook: Webhook) {
    setNotice("");
    test.mutate(hook.id, {
      onSuccess: (d) =>
        setNotice(d.state === "delivered" && d.status !== null ? t.webhooks.testDelivered(hook.name, d.status) : t.webhooks.testFailed(hook.name, d.error)),
    });
  }

  function rotateSecret(hook: Webhook) {
    if (!window.confirm(t.webhooks.confirmRotate(hook.name))) return;
    rotate.mutate(hook.id, { onSuccess: (r) => setFresh({ name: r.name, secret: r.secret ?? "" }) });
  }

  function deleteHook(hook: Webhook) {
    if (!window.confirm(t.webhooks.confirmDelete(hook.name))) return;
    remove.mutate(hook.id, {
      onSuccess: () => {
        setNotice(t.webhooks.deleted(hook.name));
        if (fresh?.name === hook.name) setFresh(null);
      },
    });
  }

  return (
    <div className="mx-auto max-w-4xl">
      <PageHeader title={t.webhooks.title} meta={t.webhooks.intro} />
      {fresh?.secret && <FreshSecret name={fresh.name} secret={fresh.secret} onDone={() => setFresh(null)} />}
      <CreateWebhookForm
        onCreated={(hook) => {
          setNotice("");
          setFresh({ name: hook.name, secret: hook.secret ?? "" });
        }}
      />
      <section aria-labelledby="webhooks-list" className="space-y-3">
        <h2 id="webhooks-list" className="text-sm font-semibold text-ink">
          {t.webhooks.list}
        </h2>
        {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
        <p role="status" className="text-sm text-ink-muted empty:hidden" data-webhooks-notice>
          {notice}
        </p>
        {isLoading ? null : hooks.length === 0 ? (
          <EmptyState icon={<Icon.Share />} title={t.webhooks.empty} description={t.webhooks.emptyBody} />
        ) : (
          <Table>
            <thead>
              <tr>
                <Th>{t.webhooks.columnName}</Th>
                <Th className="hidden sm:table-cell">{t.webhooks.columnTopics}</Th>
                <Th className="hidden lg:table-cell">{t.webhooks.columnProjects}</Th>
                <Th className="hidden md:table-cell">{t.webhooks.columnOwner}</Th>
                <Th className="w-10">
                  <span className="sr-only">{t.webhooks.columnActions}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {hooks.map((hook) => (
                <tr key={hook.id} data-webhook={hook.name} data-webhook-enabled={hook.enabled ? "true" : "false"}>
                  <Td className="max-w-0 min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium text-ink">{hook.name}</span>
                      <Tag className={cx(!hook.enabled && "text-danger")} data-webhook-state>
                        {hook.enabled ? t.webhooks.on : hook.disabledReason === "failing" ? t.webhooks.offFailing : t.webhooks.off}
                      </Tag>
                      <Tag data-webhook-version={hook.version}>{versionName(hook.version)}</Tag>
                    </div>
                    <div className="truncate font-mono text-xs text-ink-muted" title={hook.url}>
                      {hook.url}
                    </div>
                    {hook.disabledReason === "failing" && <p className="text-xs text-ink-muted">{t.webhooks.failingHint(hook.failures)}</p>}
                  </Td>
                  <Td className="hidden text-ink-muted sm:table-cell">{hook.topics.map(topicName).join(", ")}</Td>
                  <Td className="hidden text-ink-muted lg:table-cell" data-webhook-projects>
                    {hook.projects.length ? hook.projects.map((p) => p.key).join(", ") : t.webhooks.everyProject}
                  </Td>
                  <Td className="hidden text-ink-muted md:table-cell" data-webhook-owner>
                    {hook.owner?.name ?? t.webhooks.noOwner}
                  </Td>
                  <Td className="text-right">
                    <Menu
                      label={t.webhooks.actionsFor(hook.name)}
                      align="end"
                      trigger={(props) => (
                        <IconButton
                          icon={<Icon.More />}
                          label={t.webhooks.actionsFor(hook.name)}
                          size="sm"
                          onClick={props.toggle}
                          aria-haspopup={props["aria-haspopup"]}
                          aria-expanded={props["aria-expanded"]}
                          aria-controls={props["aria-controls"]}
                          data-webhook-menu={hook.name}
                        />
                      )}
                      items={[
                        {
                          label: t.webhooks.deliveries,
                          icon: <Icon.ListBullet />,
                          onSelect: () => setLogOf(hook),
                          attrs: { "data-action": "webhook-deliveries" },
                        },
                        { label: t.webhooks.test, icon: <Icon.Check />, onSelect: () => ping(hook), attrs: { "data-action": "webhook-test" } },
                        {
                          label: hook.enabled ? t.webhooks.turnOff : t.webhooks.turnOn,
                          icon: <Icon.Eye />,
                          onSelect: () => toggle(hook),
                          attrs: { "data-action": "webhook-toggle" },
                        },
                        ...WEBHOOK_VERSIONS.filter((v) => v !== hook.version).map((v) => ({
                          label: t.webhooks.switchVersion(versionName(v)),
                          icon: <Icon.Share />,
                          onSelect: () => switchVersion(hook, v),
                          attrs: { "data-action": "webhook-version", "data-version": String(v) },
                        })),
                        { label: t.webhooks.rotate, icon: <Icon.Key />, onSelect: () => rotateSecret(hook), attrs: { "data-action": "webhook-rotate" } },
                        {
                          label: t.webhooks.delete,
                          icon: <Icon.Trash />,
                          danger: true,
                          onSelect: () => deleteHook(hook),
                          attrs: { "data-action": "webhook-delete" },
                        },
                      ]}
                    />
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </section>
      {logOf && <DeliveryLog hook={logOf} onClose={() => setLogOf(null)} />}
    </div>
  );
}

function CreateWebhookForm({ onCreated }: { onCreated: (hook: Webhook) => void }) {
  const create = useCreateWebhook();
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [topics, setTopics] = useState<WebhookTopic[]>([WEBHOOK_TOPIC_ANY]);
  const [projects, setProjects] = useState<string[]>([]);
  const [version, setVersion] = useState<Version>(WEBHOOK_DEFAULT_VERSION);
  const { data: known = [] } = useProjects();
  const [missing, setMissing] = useState<{ name?: boolean; url?: boolean; topics?: boolean }>({});
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const formError = create.error && Object.keys(fields).length === 0 ? create.error.message : null;
  const choices: WebhookTopic[] = [WEBHOOK_TOPIC_ANY, ...WEBHOOK_TOPICS];

  function choose(topic: WebhookTopic, on: boolean) {
    setMissing((m) => ({ ...m, topics: false }));
    setTopics((current) => (on ? [...current, topic] : current.filter((x) => x !== topic)));
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const gaps = { name: !name.trim(), url: !url.trim(), topics: topics.length === 0 };
    if (gaps.name || gaps.url || gaps.topics) {
      setMissing(gaps);
      return;
    }
    create.mutate(
      { name: name.trim(), url: url.trim(), topics, projects, version },
      {
        onSuccess: (hook) => {
          setName("");
          setUrl("");
          setTopics([WEBHOOK_TOPIC_ANY]);
          setProjects([]);
          setVersion(WEBHOOK_DEFAULT_VERSION);
          onCreated(hook);
        },
      },
    );
  }

  const topicsError = missing.topics ? t.webhooks.topicsMissing : fields.topics;
  return (
    <Card className="mb-6 p-4">
      <form onSubmit={submit} noValidate className="space-y-3" aria-labelledby="webhooks-new" data-webhook-form>
        <h2 id="webhooks-new" className="text-sm font-semibold text-ink">
          {t.webhooks.newTitle}
        </h2>
        {formError && <ErrorBanner>{formError}</ErrorBanner>}
        <Field
          label={t.webhooks.name}
          placeholder={t.webhooks.namePlaceholder}
          value={name}
          maxLength={WEBHOOK_NAME_MAX_LENGTH}
          error={missing.name ? t.webhooks.nameMissing : fields.name}
          onChange={(event) => {
            setName(event.target.value);
            setMissing((m) => ({ ...m, name: false }));
          }}
        />
        <Field
          label={t.webhooks.url}
          placeholder={t.webhooks.urlPlaceholder}
          hint={t.webhooks.urlHint}
          type="url"
          inputMode="url"
          value={url}
          maxLength={WEBHOOK_URL_MAX_LENGTH}
          className="font-mono text-xs"
          error={missing.url ? t.webhooks.urlMissing : fields.url}
          onChange={(event) => {
            setUrl(event.target.value);
            setMissing((m) => ({ ...m, url: false }));
          }}
        />
        <fieldset className="space-y-2" aria-describedby={topicsError ? "webhooks-topics-error" : undefined}>
          <legend className="mb-1 text-sm font-medium text-ink">{t.webhooks.topics}</legend>
          <div className="flex flex-col gap-1.5">
            {choices.map((topic) => (
              <Checkbox
                key={topic}
                label={topic === WEBHOOK_TOPIC_ANY ? t.webhooks.topicAny : topicName(topic)}
                checked={topics.includes(topic)}
                onChange={(event) => choose(topic, event.target.checked)}
                data-topic={topic}
              />
            ))}
          </div>
          {topicsError && (
            <p id="webhooks-topics-error" className="text-xs text-danger">
              {topicsError}
            </p>
          )}
        </fieldset>
        {known.length > 0 && (
          <fieldset className="space-y-2" aria-describedby="webhooks-projects-hint">
            <legend className="mb-1 text-sm font-medium text-ink">{t.webhooks.projects}</legend>
            <p id="webhooks-projects-hint" className="text-xs text-ink-muted">
              {t.webhooks.projectsHint}
            </p>
            <div className="flex max-h-48 flex-col gap-1.5 overflow-y-auto">
              {known.map((p) => (
                <Checkbox
                  key={p.key}
                  label={`${p.key} ${p.name}`}
                  checked={projects.includes(p.key)}
                  onChange={(event) => setProjects((current) => (event.target.checked ? [...current, p.key] : current.filter((k) => k !== p.key)))}
                  data-project={p.key}
                />
              ))}
            </div>
            {fields.projects && <p className="text-xs text-danger">{fields.projects}</p>}
          </fieldset>
        )}
        <Select
          label={t.webhooks.version}
          hint={t.webhooks.versionHint}
          value={version}
          error={fields.version}
          onChange={(event) => setVersion(Number(event.target.value) as Version)}
          data-webhook-version-choice
        >
          {WEBHOOK_VERSIONS.map((v) => (
            <option key={v} value={v}>
              {versionName(v)}
            </option>
          ))}
        </Select>
        <p className="text-xs text-ink-muted">{t.webhooks.ownerNote}</p>
        <Button type="submit" icon={<Icon.Plus />} loading={create.isPending} data-action="create-webhook">
          {t.webhooks.create}
        </Button>
      </form>
    </Card>
  );
}

// The status line is always there, empty until needed, so a screen reader
// hears the copy succeed.
function FreshSecret({ name, secret, onDone }: { name: string; secret: string; onDone: () => void }) {
  const [said, setSaid] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  async function copy() {
    let message = t.webhooks.copied;
    try {
      await navigator.clipboard.writeText(secret);
    } catch {
      message = t.webhooks.copyFailed;
    }
    setSaid(message);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setSaid(""), COPY_FEEDBACK_MS);
  }

  return (
    <Card className="mb-6 space-y-2 border-accent/40 p-4" data-webhook-secret>
      <h2 className="text-sm font-semibold text-ink">{t.webhooks.freshTitle}</h2>
      <p className="text-sm text-ink-muted">{t.webhooks.freshBody(WEBHOOK_SIGNATURE_HEADER, WEBHOOK_TIMESTAMPED_SIGNATURE_HEADER, WEBHOOK_TIMESTAMP_HEADER)}</p>
      <Field
        label={t.webhooks.freshLabel(name)}
        value={secret}
        readOnly
        className="font-mono text-xs"
        onFocus={(event) => event.target.select()}
        data-webhook-secret-value
      />
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="secondary" size="sm" icon={<Icon.Copy />} onClick={copy} data-action="copy-webhook-secret">
          {t.webhooks.copy}
        </Button>
        <Button variant="ghost" size="sm" onClick={onDone} data-action="webhook-secret-done">
          {t.webhooks.done}
        </Button>
        <p role="status" className="text-xs text-ink-muted">
          {said}
        </p>
      </div>
    </Card>
  );
}

function DeliveryLog({ hook, onClose }: { hook: Webhook; onClose: () => void }) {
  const { data, error } = useWebhookDeliveries(hook.id);
  const redeliver = useRedeliverWebhook();
  const [said, setSaid] = useState("");
  const deliveries = data ?? [];
  const failure = error ?? redeliver.error;
  return (
    <Dialog title={t.webhooks.logTitle(hook.name)} wide onClose={onClose} data-webhook-log={hook.name}>
      <p className="mb-3 text-xs text-ink-muted">{t.webhooks.logIntro}</p>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      <p role="status" className="mb-2 text-sm text-ink-muted empty:hidden">
        {said}
      </p>
      {data && deliveries.length === 0 ? (
        <p className="text-sm text-ink-muted">{t.webhooks.logEmpty}</p>
      ) : (
        <ul className="divide-y divide-border">
          {deliveries.map((d) => {
            const at = when.format(new Date(d.attemptedAt ?? d.createdAt));
            return (
              <li key={d.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5 text-sm" data-delivery={d.topic} data-delivery-state={d.state}>
                <Tag className={cx(d.state === "failed" && "text-danger")}>{t.webhooks.states[d.state]}</Tag>
                <span className="min-w-0 flex-1">
                  <span className="block text-ink">
                    {topicName(d.topic)}{" "}
                    <span className="text-ink-subtle">
                      {t.webhooks.attempt(d.attempt)}
                      {d.manual && `, ${t.webhooks.manual}`}
                    </span>
                  </span>
                  <span className="block text-xs break-words text-ink-muted">{deliveryDetail(d)}</span>
                </span>
                <span className="text-xs text-ink-subtle">{at}</span>
                {d.state !== "pending" && (
                  <Button
                    variant="secondary"
                    size="sm"
                    loading={redeliver.isPending && redeliver.variables?.deliveryId === d.id}
                    aria-label={t.webhooks.redeliverLabel(topicName(d.topic), at)}
                    onClick={() =>
                      redeliver.mutate(
                        { webhookId: hook.id, deliveryId: d.id },
                        { onSuccess: (again) => setSaid(t.webhooks.sentAgain(t.webhooks.states[again.state])) },
                      )
                    }
                    data-action="redeliver"
                  >
                    {t.webhooks.redeliver}
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </Dialog>
  );
}
