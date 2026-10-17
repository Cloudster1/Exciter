# Markdown

A requirement's statement and rationale, and a comment's body, are stored as
the editor's document. A script or an assistant reads and writes them as
Markdown as well (#186), converted in the API by `backend/internal/markdown`,
which is adapted from Stator's. The document stays what is stored, so a
revision does not tell how it was written.

## Reading

`format=markdown` on these reads answers each document as Markdown beside it:

| Operation | Answers |
|---|---|
| `GET /requirements/{reqKey}` | `statementMarkdown`, `rationaleMarkdown` |
| `GET /requirements/{reqKey}/revisions/{revisionNumber}` | the same, as the revision said it |
| `GET /projects/{projectKey}/spec` | the same on each entry |
| `GET /requirements/{reqKey}/comments`, `GET /comments/{commentID}` | `bodyMarkdown` on each comment |

A field is left out where there is no document, and where the document holds
what Markdown cannot, listed below, so a reader falls back to the document
rather than reading less than is there. The MCP tools read Markdown unless a
call asks for `format=document`.

A specification as one document is Markdown too, from
`GET /projects/{projectKey}/document.md` and
`GET /baselines/{baselineID}/document.md`, and to an assistant as the
resources of [`docs/mcp.md`](mcp.md) (#187). In it, a text Markdown cannot
hold is written as its words, marked "(as plain text)".

## Writing

| Operation | Takes |
|---|---|
| `POST /projects/{projectKey}/requirements` | `statementMarkdown`, `rationaleMarkdown` |
| `PATCH /requirements/{reqKey}` | the same; a rationale of no words clears it |
| `POST /requirements/{reqKey}/comments`, `POST /comments/{commentID}/replies`, `PATCH /comments/{commentID}` | `bodyMarkdown` |

Each field takes one form: a document or Markdown, never both. What is written
is held to the same rules as a document sent as one. Where Stator imports a
file from elsewhere and keeps going with a warning, a requirement's text is
normative and its writer is at hand, so what would not come back as written is
refused, 422 on the Markdown field, with a sentence that says what to change.

## How each node is written

Markdown is CommonMark with the GitHub extensions for tables and
strikethrough, read by goldmark. What Markdown has no syntax for is written as
an HTML element that reads as its words anywhere, marked `data-exciter`, as
Stator marks its own with `data-stator`. No other HTML reaches a requirement.

| Node or mark | Markdown |
|---|---|
| heading 1 to 3 | `#` to `###` |
| paragraph, hard break | text, `\` at the end of a line |
| bold, italic, strike, code | `**b**`, `*i*`, `~~s~~`, `` `c` `` |
| link | `[words](href "title")`, given the target and rel the editor gives every link |
| bullet, numbered list | `-`, `1.` with its start |
| blockquote | `>` |
| code block | fenced, with its language |
| horizontal rule | `---` |
| table | GFM table, its first row the header, alignment per column |
| picture | `![alt](exciter:attachment/<id>)`, or `<img src="exciter:attachment/<id>" alt="alt" width="320">` with a width |
| file | `[name](exciter:attachment/<id>)` |
| mention | `<span data-exciter="mention" data-id="<id>">@Name</span>` |

Two lists of one kind in a row are kept apart by an empty comment, `<!-- -->`,
since Markdown would join them.

### What Markdown cannot hold

A document with any of these is not answered as Markdown:

- a numbered list numbered by letters or roman numerals
- a table cell that spans, or holds anything but one paragraph
- a table without a header row, or with header cells below it
- a break that ends a paragraph, or a line break inside text

A table's column widths are not written, and spaces at the edge of styled words
are written outside the style; neither changes what the text says.

### What is refused

- HTML other than the forms above
- a heading deeper than `###`
- a picture from elsewhere: upload it as an attachment of the requirement and
  point at it with `exciter:attachment/<id>`
- a picture or file that is not the requirement's own, which on creating a
  requirement is any, since it has none yet
- a mention of somebody who is not a member
- a link that is not a web, mail or site address (`javascript:`, `data:`)
- Markdown over 1 MB, or nested deeper than a document may be
