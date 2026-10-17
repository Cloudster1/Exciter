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
  - projects and their specifications
  - requirements: reading, writing, their revisions and comparisons
  - workflow transitions
  - trace links, and Armature issues
  - comments and search
  - who you are
  Each tool says whether it only reads, whether it changes something in a way
  the history does not keep, and whether it reaches Armature or Stator.
- **The API document** as the resource `exciter://openapi.json`, for an
  assistant that wants to know more than the tools say.
- **Instructions** the client hands the model when it connects: how keys look,
  that a change to a requirement's text names the revision it was made from,
  and how the specification is numbered.

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
