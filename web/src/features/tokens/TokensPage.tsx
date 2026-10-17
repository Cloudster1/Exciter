import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useCreateToken, useRevokeToken, useTokens, type ApiToken } from "@/api/tokens";
import { Button, Card, Checkbox, EmptyState, ErrorBanner, Field, PageHeader, Select, Table, Tag, Td, Th } from "@/components/ui";
import { Icon } from "@/components/icons";
import {
  COPY_FEEDBACK_MS,
  DAY_MS,
  MCP_PATH,
  MCP_SERVER_NAME,
  TOKEN_DEFAULT_EXPIRY_DAYS,
  TOKEN_EXPIRY_DAYS,
  TOKEN_NAME_MAX_LENGTH,
  TOKEN_READ_SCOPE,
} from "@/config";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { ProjectPicker } from "./ProjectPicker";

// Stator's tokens page, where a token is limited to projects rather than spaces.

export const when = localDateFormat({ dateStyle: "medium", timeStyle: "short" });

/** When a token made now for that many days stops working; 0 is never. */
export function expiryFrom(days: number, now: Date = new Date()): string | undefined {
  return days === 0 ? undefined : new Date(now.getTime() + days * DAY_MS).toISOString();
}

export function expired(token: Pick<ApiToken, "expiresAt">, now: Date): boolean {
  return token.expiresAt !== null && new Date(token.expiresAt) <= now;
}

/** What a token reaches, in a sentence. */
export function reachOf(token: Pick<ApiToken, "allProjects" | "projects">): string {
  if (token.allProjects) return t.tokens.reachesAll;
  return token.projects.length > 0 ? t.tokens.reachesOnly(token.projects.join(", ")) : t.tokens.reachesNone;
}

/** A person's own tokens: made here, shown once, listed and revoked, with how to connect an assistant. */
export function TokensPage() {
  const { data, isLoading, error } = useTokens();
  const revoke = useRevokeToken();
  const [fresh, setFresh] = useState<ApiToken | null>(null);
  const [notice, setNotice] = useState("");
  const tokens = data ?? [];
  const failure = error ?? revoke.error;
  const now = new Date();

  function revokeToken(token: ApiToken) {
    if (!window.confirm(t.tokens.confirmRevoke(token.name))) return;
    revoke.mutate(token.id, {
      onSuccess: () => {
        setNotice(t.tokens.revoked(token.name));
        if (fresh?.id === token.id) setFresh(null);
      },
    });
  }

  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader title={t.tokens.title} meta={t.tokens.intro} />
      {fresh?.secret && <FreshToken secret={fresh.secret} onDone={() => setFresh(null)} />}
      <CreateTokenForm
        onCreated={(token) => {
          setNotice("");
          setFresh(token);
        }}
      />
      <section aria-labelledby="tokens-yours" className="space-y-3">
        <h2 id="tokens-yours" className="text-sm font-semibold text-ink">
          {t.tokens.yours}
        </h2>
        {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
        <p role="status" className="text-sm text-ink-muted empty:hidden" data-tokens-notice>
          {notice}
        </p>
        {isLoading ? null : tokens.length === 0 ? (
          <EmptyState icon={<Icon.Key />} title={t.tokens.empty} description={t.tokens.emptyBody} />
        ) : (
          <Table>
            <thead>
              <tr>
                <Th>{t.tokens.columnName}</Th>
                <Th>{t.tokens.columnLastUsed}</Th>
                <Th>{t.tokens.columnExpires}</Th>
                <Th className="w-10">
                  <span className="sr-only">{t.tokens.columnActions}</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {tokens.map((token) => (
                <tr key={token.id} data-token-row={token.name} data-token-scope={token.scopes.join(",")}>
                  <Td>
                    <span className="font-medium text-ink">{token.name}</span>
                    {token.scopes.includes(TOKEN_READ_SCOPE) && <Tag className="ml-2">{t.tokens.tagReadOnly}</Tag>}
                    {expired(token, now) && <Tag className="ml-2">{t.tokens.tagExpired}</Tag>}
                    <p className="text-xs text-ink-muted" data-token-projects={token.allProjects ? "" : token.projects.join(",")}>
                      {reachOf(token)}
                    </p>
                  </Td>
                  <Td className="text-ink-muted" data-token-last-used>
                    {token.lastUsedAt ? when.format(new Date(token.lastUsedAt)) : t.tokens.neverUsed}
                  </Td>
                  <Td className="text-ink-muted" data-token-expires>
                    {token.expiresAt ? when.format(new Date(token.expiresAt)) : t.tokens.neverExpires}
                  </Td>
                  <Td className="text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={t.tokens.revokeLabel(token.name)}
                      loading={revoke.isPending && revoke.variables === token.id}
                      onClick={() => revokeToken(token)}
                      data-action="revoke-token"
                    >
                      {t.tokens.revoke}
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </section>
      <ConnectAssistant />
    </div>
  );
}

/** The client settings an MCP client takes, with the token left for the reader to fill in. */
export function mcpClientSettings(endpoint: string, token: string): string {
  return JSON.stringify({ mcpServers: { [MCP_SERVER_NAME]: { type: "http", url: endpoint, headers: { Authorization: `Bearer ${token}` } } } }, null, 2);
}

function ConnectAssistant() {
  const endpoint = `${window.location.origin}${MCP_PATH}`;
  return (
    <section aria-labelledby="tokens-mcp" className="mt-8 space-y-3" data-mcp>
      <h2 id="tokens-mcp" className="text-sm font-semibold text-ink">
        {t.tokens.mcpTitle}
      </h2>
      <p className="text-sm text-ink-muted">{t.tokens.mcpBody}</p>
      <p className="text-sm text-ink-muted" data-mcp-oauth>
        {t.tokens.mcpOAuth}
      </p>
      <Field
        label={t.tokens.mcpEndpoint}
        value={endpoint}
        readOnly
        className="font-mono text-xs"
        onFocus={(event) => event.target.select()}
        data-mcp-endpoint
      />
      <p className="text-sm text-ink-muted">{t.tokens.mcpHeader}</p>
      <figure className="space-y-1">
        <figcaption className="text-sm font-medium text-ink">{t.tokens.mcpConfig}</figcaption>
        {/* biome-ignore lint/a11y/noNoninteractiveTabindex: settings wider than the page scroll, and a keyboard scrolls only what has focus */}
        <pre tabIndex={0} className="overflow-x-auto rounded-control bg-surface-sunken p-3 font-mono text-xs text-ink" data-mcp-config>
          {mcpClientSettings(endpoint, t.tokens.mcpTokenPlaceholder)}
        </pre>
      </figure>
      <p className="text-xs text-ink-muted">{t.tokens.mcpReadOnly}</p>
      <p className="text-xs text-ink-muted">{t.tokens.mcpProjects}</p>
    </section>
  );
}

function CreateTokenForm({ onCreated }: { onCreated: (token: ApiToken) => void }) {
  const create = useCreateToken();
  const [name, setName] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [days, setDays] = useState<number>(TOKEN_DEFAULT_EXPIRY_DAYS);
  // No project picked means the token reaches every project its owner does.
  const [projects, setProjects] = useState<string[]>([]);
  const [missing, setMissing] = useState(false);
  const fields = create.error instanceof ApiError ? create.error.fields : {};
  const formError = create.error && Object.keys(fields).length === 0 ? create.error.message : null;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) {
      setMissing(true);
      return;
    }
    create.mutate(
      { name: name.trim(), scopes: readOnly ? [TOKEN_READ_SCOPE] : [], ...(projects.length > 0 ? { projects } : {}), expiresAt: expiryFrom(days) },
      {
        onSuccess: (token) => {
          setName("");
          setReadOnly(false);
          setDays(TOKEN_DEFAULT_EXPIRY_DAYS);
          setProjects([]);
          onCreated(token);
        },
      },
    );
  }

  return (
    <Card className="mb-6 p-4">
      <form onSubmit={submit} noValidate className="space-y-3" aria-labelledby="tokens-new" data-token-form>
        <h2 id="tokens-new" className="text-sm font-semibold text-ink">
          {t.tokens.newTitle}
        </h2>
        {formError && <ErrorBanner>{formError}</ErrorBanner>}
        <Field
          label={t.tokens.name}
          placeholder={t.tokens.namePlaceholder}
          value={name}
          maxLength={TOKEN_NAME_MAX_LENGTH}
          error={missing ? t.tokens.nameMissing : fields.name}
          onChange={(event) => {
            setName(event.target.value);
            setMissing(false);
          }}
        />
        <div className="flex flex-wrap items-end gap-4">
          <Select label={t.tokens.expires} value={days} error={fields.expiresAt} onChange={(event) => setDays(Number(event.target.value))} className="min-w-40">
            {TOKEN_EXPIRY_DAYS.map((choice) => (
              <option key={choice} value={choice}>
                {t.tokens.inDays(choice)}
              </option>
            ))}
            <option value={0}>{t.tokens.never}</option>
          </Select>
          <div className="pb-2">
            <Checkbox
              label={t.tokens.readOnly}
              checked={readOnly}
              onChange={(event) => setReadOnly(event.target.checked)}
              aria-describedby="tokens-read-only-hint"
            />
            <p id="tokens-read-only-hint" className="text-xs text-ink-muted">
              {t.tokens.readOnlyHint}
            </p>
          </div>
        </div>
        {fields.scopes && <ErrorBanner>{fields.scopes}</ErrorBanner>}
        <ProjectPicker id="tokens-projects" picked={projects} onChange={setProjects} some={t.tokens.projectsSome} error={fields.projects} />
        <Button type="submit" icon={<Icon.Plus />} loading={create.isPending} data-action="create-token">
          {t.tokens.create}
        </Button>
      </form>
    </Card>
  );
}

// The status line is always there, empty until needed, so a screen reader hears the copy succeed.
function FreshToken({ secret, onDone }: { secret: string; onDone: () => void }) {
  const [said, setSaid] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  async function copy() {
    let message = t.tokens.copied;
    try {
      await navigator.clipboard.writeText(secret);
    } catch {
      message = t.tokens.copyFailed;
    }
    setSaid(message);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setSaid(""), COPY_FEEDBACK_MS);
  }

  return (
    <Card className="mb-6 space-y-2 border-accent/40 p-4" data-fresh-token>
      <h2 className="text-sm font-semibold text-ink">{t.tokens.freshTitle}</h2>
      <p className="text-sm text-ink-muted">{t.tokens.freshBody}</p>
      <Field label={t.tokens.freshLabel} value={secret} readOnly className="font-mono text-xs" onFocus={(event) => event.target.select()} data-token-secret />
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="secondary" size="sm" icon={<Icon.Copy />} onClick={copy} data-action="copy-token">
          {t.tokens.copy}
        </Button>
        <Button variant="ghost" size="sm" onClick={onDone} data-action="token-done">
          {t.tokens.done}
        </Button>
        <p role="status" className="text-xs text-ink-muted" data-copy-status>
          {said}
        </p>
      </div>
    </Card>
  );
}
