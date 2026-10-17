# Connecting an assistant

Exciter speaks the [Model Context Protocol](https://modelcontextprotocol.io)
at one address, so an assistant can read and write requirements as the person
it works for:

```
https://<your Exciter>/api/v1/mcp
```

The assistant sees and may do exactly what that person may, no more: every
tool call is the HTTP call the browser would make, run with the person's
rights. Exciter speaks the protocol's revisions `2025-11-25` and `2025-06-18`
over streamable HTTP, without a stream: each request has one answer.

## With a personal access token

1. In Exciter, open the account menu, then **Access tokens**, and create a
   token.
   - **Read only** keeps the assistant to reading. It is offered no
     writing tool, and is refused one if it asks.
   - **Projects** keeps it to the projects you pick. It finds nothing of the
     others, and is offered no tool that concerns the whole installation.
   - An expiry ends the token on that day. You can revoke it on the same
     page at any time.
2. Copy the secret, which starts with `exciter_pat_`. Exciter shows it once.
3. Give the client the address and the secret as a bearer token.

For Claude Code:

```sh
claude mcp add --transport http exciter https://<your Exciter>/api/v1/mcp \
  --header "Authorization: Bearer exciter_pat_..."
```

For a client configured with a JSON file, the **Connect an assistant**
section of the tokens page shows the settings with the address filled in:

```json
{
  "mcpServers": {
    "exciter": {
      "type": "http",
      "url": "https://<your Exciter>/api/v1/mcp",
      "headers": { "Authorization": "Bearer exciter_pat_..." }
    }
  }
}
```

## What the assistant gets

- **Tools**, as the client's list shows them. They cover:
  - projects and their specifications, read as one numbered document and
    compared with the previous revision, the last approval or a baseline
  - a project's types, attributes, statuses and link types, so that what an
    assistant writes fits the project
  - requirements: reading, writing, moving and labelling them, their
    revisions, comparisons and activity
  - workflow transitions, one requirement or many at once
  - baselines: taking one, reading it, and what changed since
  - reviews: asking for them, one requirement or many, withdrawing one, and
    reading what waits for you and where an approval stands
  - comments: starting, answering, rewriting your own, and resolving threads
  - trace links and their suspects, coverage, the matrix and the chains of
    the trace model
  - Armature issues and Stator pages: finding them, linking requirements to
    them, filing issues from requirements, and confirming a suspect link
  - search
  - who you are
  Each tool says whether it only reads, whether it changes something in a way
  the history does not keep, and whether it reaches Armature or Stator.
- **Resources** an assistant reads whole, as Markdown in the spelling of
  [`docs/markdown.md`](markdown.md), each read as you, as a tool call is:
  - `exciter://projects/{projectKey}/spec`: a project's specification, each
    entry a heading with its outline, key and title, then its fields, text
    and links; `?section=` and a key narrow it to that section and what is
    below it
  - `exciter://requirements/{reqKey}`: one requirement and what is below it
  - `exciter://baselines/{baselineID}/spec`: a baseline as it was taken
  - `exciter://openapi.json`: the API document, for an assistant that wants
    to know more than the tools say

  The list of resources names the specification of each project you may
  view. A read is at most 256 KiB; a longer document ends after the last
  entry that fits, with a sentence naming the address to read on from.
- **Prompts** a client offers to start common work, each with the arguments
  it names:
  - `review_section`: what changed in a project or a section since its last
    approval, or since each requirement's previous revision, with a comment
    on each finding
  - `write_requirements`: requirements drafted in a project, under a section
    when one is given, from a source text
  - `check_quality`: one requirement held to the characteristics of a
    well-formed requirement, with how it could read better
- **Instructions** the client hands the model when it connects: how keys look,
  that a change to a requirement's text names the revision it was made from,
  how the specification is numbered, and where it is read whole.

Requirements and comments come and go as Markdown: a reading tool answers
statements, rationales and comments as Markdown beside the editor's
documents, and a writing tool takes them as Markdown, as
[`docs/markdown.md`](markdown.md) spells it.

An assistant never decides a review. It can read reviews and the approval of a
requirement, but approving or rejecting is done by a person in Exciter's
pages.

Large answers are refused rather than cut, with a sentence asking the
assistant to narrow the query or page with `limit` and `offset`. One answer is
at most 64 KiB.

## When it does not connect

- **401**: the token is wrong, expired, revoked, or its person was switched
  off. Make a new one.
- **403 `cross_site`**: the request came from a web page of another site,
  which Exciter refuses to protect a server reachable from a browser. Clients
  outside a browser send no `Origin` and are not affected.
- **400 `unsupported_protocol_version`**: the client asked to go on in a
  revision Exciter does not speak. Update the client, or set it to
  `2025-11-25` or `2025-06-18`.
- **405** on `GET`: the client tried to open a stream. Exciter has none;
  clients that follow the protocol carry on without one.
