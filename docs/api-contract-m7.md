# API contract for M7: MCP integration

M7 grows the MCP endpoint of #22 into the way an assistant works with
requirements as the person it acts for:
- it speaks the current protocol and describes what its tools answer
- it offers tools for what M2 to M6 built, and none that decides a review
- it reads and writes requirements in Markdown
- it hands out requirements and specifications as resources, and prompts for
  common work
- it lets a client connect with Exciter's address alone, through OAuth
- it marks what an assistant wrote

The epic is #181; this file is #182. The route table in
`backend/internal/httpapi/operations.go` and `operations_m7.go` is the source
of truth for paths, fields and types, and this file says what they mean.
Operations it adds carry `pending: true` and answer 501 until their issue is
built. Parameters and fields it adds to operations that exist, and tools it
marks on rows that exist, arrive with their issue.

All paths are under `/api/v1` unless they start with `/.well-known/`, which
are served at the root, where RFC 8414 and RFC 9728 put them, as `/healthz`
is. A row says so with `root`, and the document gives such an operation the
server `/`. "view", "edit" and "approve" are the project rights of #12. A
project or requirement the caller may not view is 404, never 403. A refusal
is a sentence that says what to do, as in M1, except at the OAuth protocol's
own endpoints, which refuse in the shape their RFCs give clients
(`{error, error_description}`), with the sentence as the description.

## Migrations, packages and settings

- Migration ranges, in build order:
  - 00800 to 00819 for OAuth (#188)
  - 00820 to 00839 for what an assistant wrote (#190)
  - #183 to #187 and #189 need none
- Packages:
  - `internal/oauth` (#188) holds the authorization server.
  - `internal/document` gains the Markdown conversion (#186), parsed with
    `github.com/yuin/goldmark` and its GFM tables and strikethrough. The
    writer is Exciter's own.
  - The integration suite drives MCP with the official Go SDK client,
    `github.com/modelcontextprotocol/go-sdk`, in the tests only (#183).
- Settings:
  - No new environment variables. The issuer and the protected resource are
    named from `EXCITER_APP_URL`: the resource is that address and
    `/api/v1/mcp`.
  - The web's nginx and the chart's ingress send `/.well-known/oauth-` to the
    api, as they send `/api/`.
  - The installation gains `oauthConnections`, on by default (#188).
- Limits, in `internal/httpapi` and `internal/oauth`:
  - `MCPMaxToolResultBytes` stays 64 KiB.
  - `MCPMaxResourceBytes` = 256 KiB: one resource read.
  - `MCPResourcePageSize` = 50: resources in one `resources/list` page.
  - `oauth.AccessTTL` = 1 hour, `oauth.RefreshTTL` = 30 days,
    `oauth.CodeTTL` = 1 minute, `oauth.RequestTTL` = 10 minutes,
    `oauth.UnusedClientTTL` = 24 hours.
  - `oauth.MaxRegistrationsPerHour` = 10 per client address,
    `oauth.MaxClientMetadataBytes` = 5 KiB, `oauth.MaxRedirectURIs` = 10,
    `oauth.MaxClientNameLength` = 100.
- **Decided:** no tool decides a review, and Exciter is its own authorization
  server ahead of Armature and Stator (`docs/decisions.md`, 2026-10-17).
- **As built** (#190):
  - One function, `via_held()`, reads what `app.via` names when the acting
    person holds it. The trigger stamps from it, and `events.Emit` adds it to
    the event, so a row and its event always say the same.
  - A webhook carries `via` on its `actor`, beside whom it names, rather
    than once per payload type; it is left out for a session.
  - A review's `via` is how it was asked for. A link answers `via`, though
    no page shows who made a link, so the pages mark the history, the
    activity, comments, a pending review and the audit log.
  - The audit export gains `via_kind` and `via_name` as its last columns, so
    a reader of the earlier export finds the others where they were.
  - A write as the admin role on somebody's behalf, such as the worker's,
    carries no `via`: it names no person's token.

## #183 The protocol

`POST /mcp` stays one stateless endpoint, a tool call the HTTP call it stands
for, run through the whole router as the caller (#22).

- **Versions.** `MCPProtocolVersions` are `2025-11-25` and `2025-06-18`.
  - `initialize` answers the version the client asks for when it is one of
    them, and the newest otherwise, for the client to decide.
  - A request whose `MCP-Protocol-Version` header names a version not offered
    is 400 `unsupported_protocol_version`: "Exciter speaks MCP 2025-11-25 and
    2025-06-18. Update the client, or set it to one of them."
  - A request without the header is answered as before.
- **No stream.** `GET /mcp` and `DELETE /mcp` are 405, as any other method on
  a route is; a client opens no stream and ends no session.
  - Batches are still refused, and notifications are still 202.
- **Origin.** A call to `/mcp` whose `Origin` is neither `EXCITER_APP_URL` nor
  one of `EXCITER_CORS_ORIGINS` is 403 `cross_site`, whether it carries a
  cookie or a bearer, against DNS rebinding. Without `Origin`, as clients
  outside a browser send it, it passes.
- **`initialize`** answers `capabilities` with `tools`, `resources` and, from
  #187, `prompts`, each with `listChanged: false`. It also gains
  `instructions`, which tell the model:
  - a requirement is named by its key, such as `CHK-R42`, and a project by
    its key, `CHK`
  - a change to a title, statement or rationale sends `baseRevision`, the
    revision it was made from, and a newer one is refused with the
    comparison to merge
  - the specification numbers its entries by outline, `1.2.3`, and
    `get_spec` reads it in windows
  - reviews are asked for here, and decided by people in the browser
- **Each tool** in `tools/list` gains:
  - `title`: the name in words, `get_requirement` as "Get requirement".
  - `outputSchema`: the schema of the row's 2xx answer, self-contained as
    `inputSchema` is, and absent for an answer with no body.
  - `annotations`:
    - `readOnlyHint`: the operation is a GET.
    - `destructiveHint`: the row is marked `destructive`, which only
      `update_comment` is, as editing a comment keeps no earlier words.
      Every other write keeps what it replaced in the history, and no tool
      removes anything (#22).
    - `idempotentHint`: the operation is a PUT or a PATCH.
    - `openWorldHint`: the row is marked `openWorld`, which the tools whose
      answer is read from Armature or Stator are.
- **Each result**:
  - A 2xx answer is `structuredContent`, and its text the same JSON.
  - An answer past `MCPMaxToolResultBytes` is no longer cut, since a cut one
    would not fit its `outputSchema`. It is an error result: "The answer is
    larger than 64 KiB. Narrow the query, or page with limit and offset."
  - A refusal is an error result with its sentence, as before.
- **`GET /meta`** lists `mcp@1`. It promises `POST /api/v1/mcp` speaking the
  versions above, with tools, resources and prompts as their lists name
  them. A partner reads `tools/list`; the names are not promised one by
  one.
- `docs/mcp.md` says how to connect a client with a personal token, and from
  #189 with OAuth.
- **As built** (#183):
  - A header naming a revision newer than both, in the protocol's form of a
    date, passes. A newer client asks in its own revision first: the
    official Go SDK, v1.8.0, sends `server/discover` under `2026-07-28` and
    falls back to `initialize` when the method is unknown, which it is here.
    Refusing the header would end the conversation before the fallback.
    `2026-07-28` itself, stateless and with requests over several round
    trips, is not offered; it is a change of its own.
  - `list_armature_links` reads what Exciter stored and is not open world,
    so of #22's tools only `link_armature_issue` is.
  - The origin rule is the same-site check's: `/mcp` is held to it with any
    credential, where it is switched on, as in production. The integration
    suite's server does not check origins, so the unit suite proves the
    refusal.
  - `outputSchema` is the first of the row's 200, 201 and 202 answers, its
    top level inlined so it is an object. The `/mcp` row documents the
    header and its 400 and 403.

## #184 and #185 The tools

Each tool marks the row of the operation it stands for, as in #22, and each
tool's sentence says how to page a large read. A tool whose row is `orgWide`
is left out for a token limited to projects, and a writing tool for a token
that reads, as #22 has it. `contractTools` in `mcp_test.go` is this list and
#22's.

The tools of #184: the document, its structure and baselines.

| Tool | Operation | Hints |
|---|---|---|
| `get_spec` | `GET /projects/{projectKey}/spec` | read |
| `list_types` | `GET /projects/{projectKey}/types` | read |
| `list_attributes` | `GET /projects/{projectKey}/attributes` | read |
| `list_statuses` | `GET /projects/{projectKey}/statuses` | read |
| `list_project_link_types` | `GET /projects/{projectKey}/link-types` | read |
| `get_activity` | `GET /requirements/{reqKey}/activity` | read |
| `move_requirement` | `POST /requirements/{reqKey}/move` | |
| `list_labels` | `GET /labels` | read |
| `label_requirement` | `POST /requirements/{reqKey}/labels` | |
| `list_bulk_transitions` | `GET /projects/{projectKey}/transitions` | read |
| `transition_requirements` | `POST /projects/{projectKey}/transitions` | |
| `list_baselines` | `GET /projects/{projectKey}/baselines` | read |
| `get_baseline` | `GET /baselines/{baselineID}` | read |
| `get_baseline_spec` | `GET /baselines/{baselineID}/spec` | read |
| `compare_baseline` | `GET /baselines/{baselineID}/compare` | read |
| `take_baseline` | `POST /projects/{projectKey}/baselines` | |

The tools of #185: review, comments and trace.

| Tool | Operation | Hints |
|---|---|---|
| `list_waiting_reviews` | `GET /reviews/waiting` | read |
| `list_reviews` | `GET /requirements/{reqKey}/reviews` | read |
| `get_review` | `GET /reviews/{reviewID}` | read |
| `get_approval` | `GET /requirements/{reqKey}/approval` | read |
| `request_review` | `POST /requirements/{reqKey}/reviews` | |
| `request_reviews` | `POST /projects/{projectKey}/reviews` | |
| `withdraw_review` | `POST /reviews/{reviewID}/withdraw` | |
| `get_comment` | `GET /comments/{commentID}` | read |
| `reply_to_comment` | `POST /comments/{commentID}/replies` | |
| `update_comment` | `PATCH /comments/{commentID}` | destructive, idempotent |
| `list_suspect_links` | `GET /projects/{projectKey}/suspects` | read |
| `clear_suspect_link` | `POST /links/{linkID}/clear` | |
| `clear_armature_link` | `POST /armature-links/{linkID}/clear` | |
| `clear_stator_link` | `POST /stator-links/{linkID}/clear` | |
| `get_coverage` | `GET /projects/{projectKey}/coverage` | read |
| `get_matrix` | `GET /projects/{projectKey}/matrix` | read |
| `list_trace_chains` | `GET /projects/{projectKey}/trace-chains` | read |
| `search_stator_pages` | `GET /stator/search` | read, open world |
| `list_stator_links` | `GET /requirements/{reqKey}/stator-links` | read, open world |
| `link_stator_page` | `POST /requirements/{reqKey}/stator-links` | open world |
| `search_armature_issues` | `GET /armature/issues` | read, open world |
| `create_armature_issue` | `POST /projects/{projectKey}/armature-issues` | open world |

- **As built** (#184): as agreed. A token limited to projects is offered all
  sixteen, none of their rows being installation-wide.
- `get_approval` joins #185's list so that an assistant can say where a
  requirement's approval stands without reading every review.
- #183 marks #22's `list_armature_links` and `link_armature_issue` open
  world.
- **No tool decides a review.** `POST /reviews/{reviewID}/decisions` carries
  no tool, and a unit test refuses a table where it does. An approval is a
  person's own judgement, and signed approvals (#46) prove the person to the
  identity provider anyway.
- The partner tools reach Armature and Stator as the caller, with their own
  partner token, as the browser's calls do.
- **As built** (#185):
  - `search_armature_issues` stands for `GET /armature/search`, Armature's
    search by key or words; `GET /armature/issues` only looks issues up by
    their keys.
  - `list_armature_projects` (`GET /armature/projects`, read, open world)
    joins, as `create_armature_issue` needs a project the caller may file in.
  - `resolve_comment_thread` (`PUT /comment-threads/{threadID}/resolution`,
    idempotent) joins, since resolving is not an edit of a comment.
    `update_comment` only rewrites, and a reply reopens a thread, so no tool
    stands for the reopening `DELETE`.
  - `list_stator_links` reads what Exciter stored and is not open world, as
    `list_armature_links` is not.
  - `TestNoToolDecidesAReview` holds whatever row `handleDecideReview` serves
    untooled, and no tool's path ends in `/decisions`.

## #186 Markdown

- **Reads.** `format`, `document` (the default) or `markdown`, on:
  - `GET /requirements/{reqKey}` and `GET /requirements/{reqKey}/revisions/{revisionNumber}`
  - `GET /projects/{projectKey}/spec`
  - `GET /requirements/{reqKey}/comments` and `GET /comments/{commentID}`

  With `markdown`, each answers, beside the document, `statementMarkdown` and
  `rationaleMarkdown`, or a comment's `bodyMarkdown`. Each is null where there
  is no document, or where the document holds what Markdown cannot (below),
  so a reader falls back to the document rather than reading less than is
  there.
- **Writes.**
  - Creating a requirement (`POST /projects/{projectKey}/requirements`) and
    changing one (`PATCH /requirements/{reqKey}`) take `statementMarkdown`
    and `rationaleMarkdown`.
  - Starting, answering and editing a comment take `bodyMarkdown`, held to
    what a comment may hold (`CommentAllowed`).
  - Sending both forms of one field is 422 on the Markdown field: "Send the
    statement as a document or as Markdown, not both."
  - What is converted is stored and hashed as the editor's document would
    be, so a revision does not tell how it was written.
- **The spelling.** CommonMark with GFM tables and strikethrough:
  - Blocks:
    - paragraphs, and headings `#` to `###` (`MaxHeadingLevel`)
    - lists `-` and `1.`, with an ordered list's start
    - quotes `>`
    - fenced code with its language
    - `---` for a rule
    - tables whose first row is the header, with each column's alignment
  - Inline:
    - `**bold**`, `_italic_`, `~~strike~~`, `` `code` ``
    - `[text](href)` for a link, which gets the target and rel the editor
      gives every link
    - a backslash at the end of a line for a break
  - Exciter's own:
    - a mention is `[@Name](exciter:person/<id>)`
    - a picture is `![alt](exciter:attachment/<id> "width=<n>")`
    - a file is `[name](exciter:attachment/<id>)`
- **Lossless both ways**, except a table's column widths, which are not kept.
  A document holds what Markdown cannot when it has:
  - an ordered list numbered other than `1`
  - a table cell that spans, or holds anything but one paragraph
  - a table without a header row
- **Refused**, as 422 on the field, with a sentence that says what to change:
  - raw HTML
  - a heading deeper than `###`
  - a picture from an outside address: "Upload the picture as an attachment
    of the requirement, then point at it with exciter:attachment/<id>."
  - an attachment that is not the requirement's, which on creation is any
    attachment
  - a mention of somebody who is not a member
- **Tools.** A tool whose operation takes `format` is called with `markdown`
  unless its arguments name another, and its sentence says so. Writing tools
  say to send Markdown.

- **As built** (#186):
  - The conversion is `internal/markdown`, Stator's package at 6dfe0a2 cut
    to a requirement's nodes, rather than part of `internal/document`.
    Stator keeps what does not come across with a warning, where this
    refuses with the warning's sentence.
  - Stator's forms win where they differ from the spelling above: a mention
    is `<span data-exciter="mention" data-id="<id>">@Name</span>`, as Stator
    marks its own `data-stator`, and a picture with a width is an `img`
    element. `docs/markdown.md` is the spelling as built.
  - The Markdown fields are left out, not null, where they are not answered,
    so no client's types change.
  - A rationale of no words clears it, as an empty rationale is none.
  - A comment holds no files, so its Markdown points at none.

## #187 Resources and prompts

| Operation | Needs | Answers |
|---|---|---|
| `GET /projects/{projectKey}/document.md` | view | 200 `text/markdown`; 404 |
| `GET /baselines/{baselineID}/document.md` | view | 200 `text/markdown`; 404 |

- **The Markdown document** is the document of #45, the one Word and PDF are
  written from, with `section` as theirs:
  - a title and a line saying who exported it when
  - each entry a heading by its depth, at most `######`, reading
    `1.2 CHK-R42 Title`
  - its status, type, owner and attributes as a list
  - its statement and its rationale as #186 writes them, or as plain text
    marked "(as plain text)" where Markdown cannot hold them
  - its links as a list of `type KEY Title`
  - past `specdoc.MaxEntries`, a line saying how many it holds of how many
- **Resources.**
  - `resources/templates/list` offers:
    - `exciter://projects/{projectKey}/spec{?section}`: the project's
      document, or one section of it
    - `exciter://requirements/{reqKey}`: one requirement and everything below
      it, its project's document narrowed to it
    - `exciter://baselines/{baselineID}/spec`: a baseline's document
  - `resources/list` lists `exciter://openapi.json` and the specification of
    each project the caller may view, `MCPResourcePageSize` at a time, with
    `nextCursor`.
  - `resources/read` reads each through the in-process call it stands for,
    as the caller, as a tool call is. A requirement is first read for its
    project.
  - What the caller may not view is the protocol's -32002 "Resource not
    found", with the sentence.
  - A read past `MCPMaxResourceBytes` is cut at the last entry that fits,
    with "The document was cut here, after N requirements. Read a section
    with exciter://projects/KEY/spec?section=KEY."
- **Prompts.** `prompts/list` and `prompts/get` offer three. A missing
  required argument is -32602, with a sentence naming it.
  - `review_section` takes `projectKey` (required), `section` (the key of a
    section, the whole document when absent) and `base` (`previous` or
    `approved`, `approved` when absent). It asks the model to read the
    changes with `get_spec` and `onlyChanged`, and to comment on each
    finding with `comment_on_requirement`.
  - `write_requirements` takes `projectKey` (required), `section` and `source`
    (required, the text to draft from). It asks the model to read the
    project's types and the section first, then to create each requirement
    in Markdown, one statement each.
  - `check_quality` takes `reqKey` (required). It embeds the requirement as
    its resource and asks the model to hold it to the characteristics of a
    well-formed requirement: necessary, singular, unambiguous, verifiable,
    feasible, free of design, and consistent with the requirements around
    it.
  - The prompts' text is English, kept in Go beside the tools.
- `initialize` announces `prompts`.
- **As built** (#187):
  - A resource reads the JSON document of #45 in process, as the caller,
    and writes the Markdown from it, so it can be cut between two entries;
    `document.md` writes the same Markdown whole.
  - A read that names nothing the caller may read is -32602 with the
    sentence and the `uri`, not -32002: the protocol's SEP-2164 settled on
    invalid params, and the official Go SDK answers so since v1.7.0.
  - The cut says how many entries it holds and reads on from the next one,
    `exciter://projects/KEY/spec?section=` and its key; a baseline's points
    at `get_baseline_spec` and the offset, since a baseline has no
    sections. An entry larger than a read alone points at
    `get_requirement`.
  - The document's added words are English, as the tools' are; Word and
    PDF speak the reader's language. Each requirement lists its revision
    first, which a change sends as `baseRevision`.
  - `get_spec` takes no section, so `review_section` asks the model to keep
    to the entries under the section's outline.

## #188 OAuth

| Operation | Needs | Answers |
|---|---|---|
| `GET /.well-known/oauth-protected-resource`, and the same with `/api/v1/mcp` after it | public | the resource (RFC 9728); 404 while off |
| `GET /.well-known/oauth-authorization-server` | public | the server (RFC 8414); 404 while off |
| `POST /oauth/register` | public | 201 the client (RFC 7591); 400; 429 |
| `GET /oauth/authorize` | public | 302 to the consent page, or to the client with an error |
| `GET /oauth/requests/{requestID}` | a session | `{request}`; 403 `session_only`; 404 |
| `POST /oauth/requests/{requestID}` | a session | `{redirectTo}`; 403 `session_only`; 404; 422 |
| `POST /oauth/token` | public, the client | the tokens (RFC 6749); 400; 401 |
| `POST /oauth/revoke` | public, the client | 200 (RFC 7009); 400 |
| `PUT /installation/oauth-connections` | admin, a session | `{installation}` |

- **Discovery.**
  - The resource is `EXCITER_APP_URL` and `/api/v1/mcp`. Its authorization
    server is `EXCITER_APP_URL`, its scopes are `mcp` and `mcp:read`, and its
    tokens are sent in the header.
  - The server's metadata names:
    - the endpoints, under `/api/v1/oauth/`
    - `response_types` `code` and `grant_types` `authorization_code`,
      `refresh_token` and the token exchange (#273, #235)
    - `S256` alone
    - client authentication `none`, `client_secret_basic` and
      `client_secret_post`
    - `client_id_metadata_document_supported: true`
  - A 401 at `/mcp` carries `WWW-Authenticate: Bearer resource_metadata="<the
    resource's metadata address>", scope="mcp"`, with `error="invalid_token"`
    when a token was sent and refused.
- **Clients** are known in one of two ways.
  - **Registered** (RFC 7591):
    - 1 to `MaxRedirectURIs` redirects, each https, or http on a loopback
      address (`localhost`, `127.0.0.1`, `[::1]`) at any port, without a
      fragment.
    - `token_endpoint_auth_method` is `none` (the default), or
      `client_secret_basic` or `client_secret_post`, which are answered a
      secret once, stored as its SHA-256.
    - `grant_types` within the two above; `client_name` at most
      `MaxClientNameLength`, the redirect's host when absent.
    - Refused as 400 `invalid_redirect_uri` or `invalid_client_metadata`.
    - At most `MaxRegistrationsPerHour` per address, then 429
      `temporarily_unavailable`.
    - A registration that leads to no grant within `UnusedClientTTL`, and a
      client whose last grant ended `RefreshTTL` ago, are removed by the
      worker.
  - **By a metadata document**:
    - A `client_id` that is an https address with a path is fetched through
      `netguard`, at most `MaxClientMetadataBytes`.
    - The document is held to the same rules, with `client_id` equal to its
      address and `token_endpoint_auth_method` `none`.
    - It is kept as its cache headers allow, at most a day.
- **Authorizing.** `GET /oauth/authorize` checks the request in this order.
  - The client is unknown, or the redirect is not exactly one of its: the
    browser goes to the web page `/oauth/consent?error=invalid_client` or
    `invalid_redirect`, never to the redirect.
  - While OAuth connections are off: `/oauth/consent?error=switched_off`.
  - Otherwise each refusal goes back to the redirect with `error`, `state`
    and `iss` (RFC 9207):
    - `unsupported_response_type` unless `response_type=code`
    - `invalid_request` without `state`, or without `code_challenge` and
      `code_challenge_method=S256`
    - `invalid_target` unless `resource` is the MCP endpoint's address
      (RFC 8707)
    - `invalid_scope` for a scope other than `mcp` or `mcp:read`
  - A request that passes is kept for `RequestTTL` under a random id, and
    the browser goes on to `/oauth/consent?request=<id>`.
- **Consenting.**
  - The consent page needs a session: signing in comes back to it, and a
    token is 403 `session_only`. It reads the request and shows:
    - the client's name, its address and how it is known
    - the host the browser goes back to
    - whether it asked only to read
  - The person allows or denies.
    - They may narrow full access to reading, and limit the grant to
      projects they may view, at most `MaxTokenProjects`, as a token's
      (#22): 422 on `projects`.
    - A client that asked to read cannot be granted more: 422 on
      `readOnly`.
    - An expired or decided request is 404.
  - On allowing:
    - The grant is made: the person, the client, reading or full, and the
      projects.
    - An earlier grant of the same client to the same person ends, so a
      client holds one grant per person.
    - A code, single use for `CodeTTL` and stored as its SHA-256, goes back
      as `redirect_uri?code=&state=&iss=`.
  - Denying goes back with `error=access_denied`.
- **Tokens.**
  - `POST /oauth/token` takes the form of RFC 6749, the client authenticated
    as it registered.
  - `authorization_code` checks the code against the client, the
    redirect, `code_verifier` and `resource` when sent, then answers
    `{access_token, token_type: "Bearer", expires_in, refresh_token, scope}`
    with `Cache-Control: no-store`.
    - An access token is `exciter_oat_` and 43 characters, for `AccessTTL`.
    - A refresh token is `exciter_ort_` and 43 characters.
    - Only their SHA-256 is stored.
    - A code used twice ends the grant it made.
  - `refresh_token` answers a new pair and ends the refresh token it was
    sent; a scope may only narrow. A refresh token used after it was
    replaced ends the grant, as a stolen one would be.
  - Errors are those of RFC 6749 and RFC 8707: `invalid_request`,
    `invalid_client` (401), `invalid_grant`, `unauthorized_client`,
    `unsupported_grant_type`, `invalid_scope`, `invalid_target`.
  - `POST /oauth/revoke` ends an access token, or a refresh token and with
    it the grant, and answers 200 for a token unknown or already ended too.
- **Using a token.**
  - An access token is accepted at `POST /api/v1/mcp` and for the calls MCP
    makes in process, which the request's context marks, and nowhere else:
    401 `invalid_token`, "This token is for MCP alone. Use a personal access
    token, or sign in."
  - It acts as the person, with reading and the projects as a personal
    token's (#22): `app.token_projects`, `read_only_token`,
    `projects_token`.
  - `CallingToken` gains `kind`, `personal` or `oauth`. An OAuth one has the
    grant's id and the client's name, so `whoami` says what is connected.
- **Switching off.**
  - `PUT /installation/oauth-connections` `{enabled}` switches OAuth
    connections; `GET /installation` answers `oauthConnections`.
  - While off:
    - discovery is 404, and registering and token requests are refused with
      `temporarily_unavailable`
    - every grant's tokens are refused, with a 401 that names no resource
      metadata
    - grants are kept for switching on
  - Switching somebody off ends their grants, as it ends their tokens (#86).
- **Audit:** `oauth.granted` (client, reading or full, projects) and
  `installation.oauth_switched`.
- **Tables** (00800 to 00819): `oauth_client`, `oauth_request`,
  `oauth_grant` with `oauth_grant_project`, `oauth_code` and `oauth_token`.
  - Row level security keeps a grant and its projects to its owner and the
    installation's administrators, and a client to those signed in.
  - Requests, codes and tokens are read only through the admin role, as
    sessions are, since they are used before anybody is signed in.
- **As built** (#188):
  - The server's metadata also says
    `authorization_response_iss_parameter_supported: true`. The redirect
    carries `iss`, and the official Go SDK refuses one the server does not
    advertise.
  - A loopback redirect matches the one registered at any port (RFC 8252),
    and `resource` may end with a slash.
  - A registration's metadata Exciter does not use is ignored, as RFC 7591
    says. A registered client's id is random; a client secret is
    `exciter_ocs_` and a code `exciter_oac_`, so a scanner tells them apart.
  - The registrations of an address are counted in the database, from
    `oauth_client.registered_from` behind the trusted proxy, not in Valkey.
  - A wrong verifier, redirect or resource leaves a code unused; only a
    second trade ends its grant.
  - The worker sweeps every `oauth.SweepInterval`, 10 minutes: what ran out,
    and a grant left with neither a live code nor a refresh token.
  - A metadata document is fetched without following redirects, since it
    names itself by its address; without cache headers it is fetched again
    at each use.
  - The client endpoints are left out of the same-site check: they never
    read the session, so a cookie the browser adds proves nothing.
  - While off, a grant's token is 401 `invalid_token` with a sentence, and no
    `WWW-Authenticate` names the metadata.
  - `GET /auth/me` reads the installation, so its `oauthConnections` is the
    setting rather than false.
  - Switching somebody off ends their grants in the database, by a trigger
    on `app_user`, as both ways of letting somebody go do.
  - The consent page is a route of the app, so somebody signed out signs in
    and comes back to it with the request.

## #189 Connected apps

| Operation | Needs | Answers |
|---|---|---|
| `GET /oauth/grants` | member | `{grants}`, the caller's |
| `DELETE /oauth/grants/{grantID}` | the owner, a session | 204; 403 `session_only` |
| `GET /oauth/grants/everyone` | admin | `{grants}` with owners |
| `DELETE /oauth/grants/everyone/{grantID}` | admin, a session | 204; 403 `session_only` |

- **A grant** answers:
  - its id, and the client (`{id, name, uri, kind}`)
  - the host the browser went back to
  - `scopes`, as a token's: `read`, or none
  - `projects` and `allProjects`
  - `createdAt` and `lastUsedAt`, which moves at most once a minute
- The lists are ordered by last use, newest first.
- Ending a grant ends its access and refresh tokens at once. All four are
  `orgWide`, as the token routes are.
- **Audit:** `oauth.revoked`, with who ended it: its person, an
  administrator, the client, or a reused token.
- **UI.**
  - `/settings/connected-apps` sits beside `/settings/tokens` under the
    account menu, and `/settings/all-connected-apps` beside
    `/settings/all-tokens` for administrators.
  - "Connect an assistant" says to add Exciter's address in a client that
    connects with OAuth, and keeps the token settings for clients that send
    a header.
- **As built** (#189):
  - The page says **Disconnect** for revoking, as a person ends a connection
    rather than a token; the routes and `oauth.revoked` keep the contract's
    words.
  - A grant the sweeper removes because it ran out, and one replaced by
    connecting the client again, are not recorded as revoked: nobody ended
    them, and `oauth.granted` records the new one.
  - The token page's "Connect an assistant" says that an OAuth app needs the
    address alone and is found under Connected apps, and keeps the client
    settings with a header for the others.

## #273 Partner apps and the token exchange

A tool beside Exciter that signs people in with the same identity provider,
such as Stator, reads Exciter as each of them. Rather than each person pasting
an `exciter_pat_` into it, the tool trades the person's access token from the
shared realm for a grant here (RFC 8693), as it does at Armature
(Cloudster1/Armature#280). Exciter's side follows Armature's
`docs/oauth.md`, "Token exchange", so the three tools answer alike.

| Operation | Needs | Answers |
|---|---|---|
| `GET /oauth/clients` | admin | `{clients}`, the partner apps by name |
| `POST /oauth/clients` | admin | 201 `{client}` with `secret`; 422 on `name`, `clientUri`, `keycloakClientId`, `scopes` |
| `PATCH /oauth/clients/{clientID}` | admin | `{client}`; what is left out keeps its value; 404; 422 |
| `POST /oauth/clients/{clientID}/rotate-secret` | admin | `{client}` with the new `secret`; the old one stops at once; 404 |
| `DELETE /oauth/clients/{clientID}` | admin | 204, every grant of it ended; 404 |
| `POST /oauth/token` with the exchange grant | public, the partner app | the tokens; 400; 401 |

- **A partner app** answers `id` (its `client_id`), `name` (1 to 100
  characters), `clientUri`, `keycloakClientId`, `scopes`, `grants` (how many
  people it holds a grant of) and `createdAt`, and `secret` once, on
  registering and on rotating: `exciter_ocs_` and 43 characters, kept as a
  digest. An administrator registers it with a session or a token, so a
  deployment's setup job wires the tools; a partner app's own token is
  refused with 403.
- **Scopes.** `read`, always among them, and `write`, which lets a grant do
  whatever its person may, as a personal token without a scope does. Left
  out, a new app reads only. Taking `write` away narrows every grant of the
  app at once.
- **`keycloakClientId`** is the identity provider's client the app signs
  people in with. Empty, the app exchanges nothing: `unauthorized_client`.
- **The exchange.** `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`,
  `subject_token` the person's access token,
  `subject_token_type=urn:ietf:params:oauth:token-type:access_token`, an
  optional `scope` (`read`, or `read write`; left out, all the app holds) and
  `requested_token_type` the access token type or nothing. The app
  authenticates with its id and secret, HTTP Basic form-encoded first or both
  in the form.
  - The token is verified as the installation's provider's: its issuer and
    keys, unexpired, `azp` the app's `keycloakClientId`, and `aud` holding
    Exciter's own OIDC client id. In the realm, an audience mapper on the
    partner's client adds it.
  - The person is the one the token names, matched as a sign-in matches
    them, by issuer and subject and else by the verified address, and
    somebody not yet in Exciter is let in by the group mapping, as at
    sign-in, their groups synced. Somebody the mapping does not let in is
    `invalid_grant`, "Ada Lovelace has no Exciter account yet. Sign in to
    Exciter once, or ask an administrator to let them in.", and no account
    is left behind.
  - The answer is the tokens of the person's one grant of the app, with
    `issued_token_type` the access token type: an hour's `exciter_oat_` and
    an `exciter_ort_` that rotates on every refresh, as an assistant's. A
    new exchange ends the grant's earlier tokens; asking for other scopes
    than the grant holds replaces it.
  - The token is good for the whole API as a personal token is, reads only
    without `write`, and never reaches further than its person. A partner
    app's token cannot manage partner apps.
  - The administrator who named the Keycloak client consented for everybody,
    so no page is shown. `oauth.granted` records the grant with `by:
    token_exchange`.
  - Refusals: `invalid_request` for a missing subject or another token type,
    `unauthorized_client` for an app without a Keycloak client or an
    installation without single sign-on, `invalid_scope`, `invalid_grant`
    for a token the provider did not vouch for as asked, with the reason,
    and `temporarily_unavailable` when the provider cannot be reached.
    An assistant's client exchanging anything but an access token of its
    own grant is `unauthorized_client`; its own token it narrows (#235).
- **Connected apps.** The grant is among the person's connected apps (#189),
  its client of kind `partner`, and ends there, at the revocation endpoint,
  with the app, or as any grant does. The page says it was connected at the
  person's sign-in in the partner rather than where it sends back to.
- **The switch.** `PUT /installation/oauth-connections` is for assistants. A
  partner app is the administrators' own doing, and removing it is its off,
  so the switch leaves partner apps working.
- **Discovery.** The server's metadata lists the exchange among its
  `grant_types`; its `scopes_supported` stay the assistants', since a client
  may ask for every scope listed there.
- **The capability.** `oauth.token-exchange@1`, with Armature's promise
  (Cloudster1/Armature#302), resting on `POST /oauth/token`.
- **Audit:** `oauth_app.created`, `oauth_app.updated`,
  `oauth_app.secret_rotated` and `oauth_app.deleted`, with the app's id,
  name, Keycloak client and scopes.
- **UI.** The installation page lists the partner apps, registers one and
  shows its client id and secret once, lets it write or not, gives it a new
  secret and removes it.

## #190 What an assistant wrote

- **Where.** What is written through a personal token or an OAuth grant
  carries `via`, on:
  - revisions (`requirement_revision`) and changes (`requirement_change`),
    which together are the activity
  - comments, and review requests (`requirement_review`)
  - trace links, and Armature and Stator links (`trace_link`,
    `armature_issue_link`, `stator_page_link`)
  - the audit log
- **How the database writes it.**
  - `Cluster.inTx` sets `app.via`, the calling token's or grant's id, beside
    `app.user_id`.
  - A trigger on each table writes `via_kind` (`token` or `oauth`), `via_id`
    and `via_name`, the token's name or the client's at that moment, from
    the token or grant of that id held by the acting person. A row written
    without the setting has none.
  - A value sent by hand is overwritten on insert, and changing one is
    refused.
  - Revoking a token or ending a grant keeps its name on what it wrote.
- **The API.** Revisions, activity entries, comments, reviews, links and audit
  entries answer `via: {kind, name}`, null for what was written in a session.
  The webhooks Exciter sends add `via` to the event's data, which receivers of
  either envelope version read past (#103).
- **UI.** The history, the activity, comments, reviews and the audit log show
  "through" and the name beside the person.

## #235 A narrower token for one run

A client trades an access token of its own grant for a shorter one that may
do less, for one run, and the grant keeps what its person allowed. Rotor
holds a run to reading this way until a person confirms a write
(Cloudster1/Rotor#32), and parallel runs never share a refresh token.

- **The exchange.** `POST /oauth/token` with
  `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`,
  `subject_token` an `exciter_oat_` access token of the calling client's
  grant, `subject_token_type=urn:ietf:params:oauth:token-type:access_token`,
  and `requested_token_type` the same or nothing. The client authenticates
  as it registered. Any other subject is a partner's exchange (#273).
  - `scope` may only narrow: `mcp:read` from a grant that writes, and an
    evidence grant stays `evidence`. Left out, it is the subject's.
  - `resource`, when sent, is the MCP endpoint.
  - `exciter_expires_in` asks for 1 to 3600 seconds. The token never
    outlives its subject, which is also its lifetime when nothing is asked.
  - `exciter_projects` names project keys, space-separated, at most as many
    as a personal token holds. Each must be one the grant reaches and its
    person may view, read under their rights, so an unknown key and one out
    of reach refuse alike.
- **The answer.** `{access_token, token_type: "Bearer", expires_in, scope,
  issued_token_type}` and no refresh token: the token is an `exciter_oat_`
  like any other, accepted where the grant's are, with the reading and
  projects asked for.
- **What stays.** The grant, its refresh token and its other access tokens
  are untouched. An exchanged token is never the subject of another.
- **Ending.** Revoking an exchanged token (RFC 7009) ends it alone; ending
  the grant, by revoking its refresh token, at the person's connected apps,
  or by switching the person off, ends every token exchanged from it.
- **Refusals.** `invalid_grant` for a subject that is not a live access token
  of the client's own grant, or that was exchanged itself;
  `invalid_scope` for a wider scope or a project out of reach;
  `invalid_request` for another token type or a lifetime out of range;
  `invalid_target` for another resource; `unauthorized_client` for a partner
  app, which narrows by refreshing to `read`.
- **Registration.** A client may register for
  `urn:ietf:params:oauth:grant-type:token-exchange` beside
  `authorization_code`, which it still needs; the exchange of its own token
  does not depend on it.
- **The database.** `oauth_token.exchanged` and `projects_only` (migration
  01400), and `oauth_token_project`, which only the admin role reads and
  writes, as the tokens. Authentication reads a token's own projects instead
  of its grant's when it has them; they lie within the grant's.
- **The capability.** `oauth.token-exchange@1` promises this exchange too.
