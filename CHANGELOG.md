# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- The suite drives the shell in a real browser: every place it lists, read
  from its own sidebars, passes axe in light and dark; the folded sidebar
  and a folded group survive a reload; a review and the notifications are
  counted on bob's entries and fall as he reads and decides; Back to
  Exciter keeps the whole address; the palette reaches the audit log and
  the full search, and offers a member no settings; the drawer's links,
  close button, Escape and theme button work at phone width; the profile
  names who is signed in; and a sign-in never follows `next` off the site.
  Home's states, the palette's failures and a refused profile save have
  component tests (#331).
- The suite calls every MCP tool through the protocol's own client, each
  answer held to its schema and to the route it stands for, and proves the
  rest of OAuth and tokens: a consent answered twice or past its time, a
  denial over the API, a code and an access token that ran out, another
  client's refresh and revocation, a second consent replacing the first
  grant, a member refused every partner app write, a rotated secret, the
  sweeper, and a metadata document fetched from its own address alone. In
  the browser an administrator revokes a token and disconnects an app of
  somebody else's, a token and a partner secret are copied, a partner
  secret rotated, and a consent narrowed to one project (#329).
- The suite proves the Stator integration's refusals and deliveries: a
  reader holding a Stator token, the trash and an archived project change
  no link, the lookup and search keep to their bounds, a webhook past its
  size or of no envelope changes nothing, a late publish never moves a
  link back, every link sync sentence is written, the sign-in exchange at
  Stator fails, forgets and renews on its own, a print's token is listed
  nowhere and writes nothing, and the stub's token endpoint fits Stator's
  document. In the browser a page deleted and moved in Stator, the connect
  hint, picking another page and the webhook card are driven (#327).
- The suite proves outbound webhooks to the end of their delivery chain: a
  receiver that never recovers is tried six times on the documented waits
  and then left; turning a webhook off cancels what was due, and the worker
  turning one off keeps an attempt asked for by hand; the log is pruned
  but keeps what is still due; a timeout, a host that does not resolve, a
  secret that no longer opens, an unknown envelope version and a redirect
  into the network each say why; two senders post a delivery once; a save
  and a rotation are audited without the secret. In the browser a failed
  delivery is sent again, a rotated secret copied and checked by a ping,
  and a webhook the worker turned off turned on and deleted (#328).
- The integration suite's cleanup tries a statement again when it loses a
  deadlock to the stack's worker, which on a slow runner still writes the
  notifications of the project being removed (#405).
- The suite proves the Armature integration to its edges: a viewer with
  an Armature token, an archived project and the trash change no trace,
  the lookup and the search keep to their bounds, only a session stores
  a token, the app's secret is never audited, every sentence a sign-in's
  exchange keeps is produced, two reads at once renew a grant once, a
  refused refresh waits, the development panel and the MCP tools ask
  Armature only as the reader may, and the database guards clearances,
  sync records and the tokens of people without a role. In the browser a
  suspect trace is confirmed, a chip's connect hint followed, an issue
  picked again, a requirement left out of a round, a round partly refused
  and the webhook address copied (#326).
- The suite proves dashboards and computed attributes to their edges: a
  reader neither orders nor adds widgets, the 51st dashboard and the 31st
  widget are refused, every widget setting out of bounds is refused on its
  field, whoever leaves the project loses even their own dashboards while
  its administrators keep the shared ones, the database keeps widgets to
  whoever may change them and a dashboard to its project, the MCP tools
  read as the API does, and a sum over children follows a child to the
  trash and back. In the browser a dashboard is shared, read by a second
  person, copied, printed, renamed, rearranged, deleted and made the home,
  and a computed attribute is refused, recomputed and sorted by (#319).
- The suite proves imports past their happy path: a CSV and a ReqIF import
  stopped while they run end cancelled and keep what they made; another
  administrator reads and stops an import but neither maps nor starts it;
  an archived project takes no import and no template and keeps its
  templates as they are; a start reads the project again; a CSV export
  carries an attribute by its name and an import maps it back; a .reqifz,
  a Word document and a template small to upload and large to unpack are
  refused; and a shared template is changed only in its own project. In
  the browser problems are answered, and imports are discarded, stopped
  and started again, a downloaded .reqif goes into another project, a
  Word pattern changes the preview, and a template is shared and deleted
  (#318).
- The suite proves the writing rules, the glossary and duplicates beyond
  the first look: linking and merging a pair are refused for a reader, a
  stale side, the same requirement twice, another project's side, an
  unknown link type, a long comment and a locked side, with no row
  written; the pairs are paged and narrowed; taking back an acceptance
  and changing a term are refused with their codes, the term as it was;
  and `get_quality_findings`, `check_requirement_text`, `list_terms` and
  `get_similar_requirements` answer as their routes do. In the browser a
  finding is shown, accepted and taken back, rules and word lists are
  saved, a term is added, redefined, confirmed, reloaded, linked by hand
  and deleted, a library glossary is chosen, and pairs are linked,
  merged, filtered, paged, read again and listed by the threshold (#320).
- Every pull request shows Go unit, Go integration and web coverage, in
  total and per package, and fails below floors kept in `coverage/`,
  which only rise (#334). They start at 35.5% unit, 76% integration and
  91.5% of the web client's lines, leaving out its strings and constants.
- Eight tests of the coverage work assert what their items state, not
  less (#406): every field refusal the integration suite checks is a 422,
  and the toolbar, quick search, watchers, link types, trace graph and
  baseline signer tests each fail when what they name breaks.
- The suite proves verification evidence to its edges: an archived project
  takes no run, scan, counting branch or tag words, every upload limit and
  field is refused with nothing queued, a run the worker cannot read ends
  failed, code tags and a requirement's code keep to one branch and page,
  the trace check fails on gaps only when asked, the evidence MCP tools
  answer as their routes do, and the database keeps runs, results, scans
  and both settings to their rules. In the browser the counting branch and
  a tag word are set and take effect and the code tags are filtered and
  paged, and the gate runs the exciter-ci image through its make targets
  against the stack (#323).
- The suite proves reviews, signed approvals and review sessions through
  every role and scope: an editor sets no rule and moderates nobody else's
  session, an archived project takes no rule or review, a rule of one type
  comes first, an author approves where the rule lets them, a signature
  keeps its German meaning, sessions open over a query and since an
  approval or a baseline, refuse an empty scope, a bad quorum, too many
  requirements, approvers or rounds and a taken baseline name, the
  reminder tells only who still decides, the sessions list and its MCP
  tools keep to their readers, and the database holds a session to its
  opening. In the browser a review is rejected and withdrawn, a typed rule
  changed and removed, a signature given at Keycloak and cancelled, and
  sessions opened over a baseline and a query and their report downloaded
  (#317).
- The suite proves the traceability matrix and the trace model to their
  edges: another project's requirements as columns both ways and in the
  files, every combination the matrix refuses, a matrix too wide for a
  file, the trace model refused a retired type, a link type turned off or
  another project's, too many levels, a reader and an archived project,
  chains paged and ending in Stator pages or Armature issues, views that
  lost a link type or a level, and `get_matrix` and `list_trace_chains`
  answering as their routes do. In the browser issue columns, paging, a
  suspect cell, moved and removed levels, levels of tests and code, and
  chains narrowed, downloaded and shared are used (#315).
- The stack's primary database logs the plan of any statement that runs
  past three seconds, with the rows each step gave, and a failed gate
  keeps it with its reports (#405). The FMEA table's query has run past
  ten seconds on CI's runners with nothing to say why.
- Nine more tests of the coverage work assert what their items state
  (#416): a draft's statement that is no document and a reader's bulk
  transition are refused with nothing moved, the tester bounds and a due
  date kept are checked by their own sentence and value, and the dashboard
  list, the home dashboard, the home executions, the start dialog's
  refusals and a live Armature chip each fail when what they name breaks.
- The suite proves test plans and executions past the happy path: every
  field a start, a plan, a result, a file and an issue refuses, a reader's
  403 and the archived project with nothing written, aborting so a result
  counts for nothing, the due reminder once per due date, a group testing
  through its members, all eleven MCP tools answering as their routes do,
  and each guard of the executions' tables through SQL. In the browser a
  plan's cases are moved, removed and found by a changed query, a plan is
  deleted, an execution changed, aborted and printed, a case's iterations
  walked with its outcome set by hand and a file deleted, and a failure
  filed in Armature (#324).
- The suite proves baselines and their signatures to every refusal: a
  baseline of an archived project is neither taken, renamed, deleted nor
  signed, a root or a comparison outside its project is refused, a
  baseline being signed keeps its name, a signer who left the frozen group
  or cannot read the project is refused, a provider sign-in signs once and
  wrong passwords count with reviews, and the database keeps a baseline's
  root, project, rule and held links. In the browser a baseline is
  renamed, deleted, taken of a section and compared with another, its
  matrix downloads as a workbook, and two signers sign under a changed rule
  (#316).
- The suite proves coverage and the trace graph against every way they are
  narrowed: coverage rules are refused to a reader, for an id that is none
  and in an archived project, and the rules stay as they were; the report's
  window is checked; each reader's coverage is cached apart; a link type's
  count changes the next read; the graph draws Stator pages, follows a
  project's own link types and answers a section and the trash; neither
  the graph, coverage nor the query language names an end the reader may
  not see; `get_trace_graph` and `get_coverage` answer as their routes do.
  In the browser every control of the coverage and trace graph pages is
  used, by keyboard too, and a reader walks the report a page at a time
  (#314).
- The suite proves trace links, link types and suspect links to their last
  control: a type for all projects is refused to a project's administrator,
  a taken name is refused in either scope, a type comes back from
  retirement, a link past a requirement's share and links of an archived
  project are refused with nothing changed, the suspect list is paged and
  keeps to what its reader sees, every revision of a watched end turns a
  link, `list_links` answers as its route does, and the database's own
  guards on names, scopes, types and the trash are tried as `exciter_app`.
  In the browser every control of the links panel, the link types, their
  pairs and the suspect list is used (#313).
- The suite proves the edges of comments, files, watching and
  notifications: every refusal leaves the comments, threads, files and
  watches as they were, a mention and the mention picker reach only who
  may read, an edit and a reopening tell whom they concern, watches and
  notifications follow what a person may still view, each preference and
  the hourly digest have their effect, and the trash and an archive hold
  straight through SQL. In the browser comments are edited, reopened and
  moderated, projects watched, the Watching and Notifications pages used,
  and files dropped on the panel (#312).
- The suite proves every workflow rule and the shaping of a project: each
  rule refuses or acts on a transition saved in a workflow, a rule that
  could never run is refused on save, workflows, statuses, types and
  attributes are reached only through their project, an archived project
  takes no new shape, field rules keep to the fixed fields and the
  project's roles, an assistant lists and takes transitions, and the
  database keeps rules and steps to the administrators. In the browser
  types, attributes, field rules, statuses and workflows are made,
  changed and deleted, the designer's inspector draws a workflow, and
  each change is checked on a requirement (#322).
- The suite proves the risk scheme, the FMEA table and the risk report to
  their edges: an archived project keeps its scheme, a save is audited
  with its actor, detection stays on while a rating gives it, the table
  pages and refuses a page or a cell off its bounds, a project without a
  scheme rates nothing and reports nothing, a query narrows the matrix,
  the report and the workbook, every download is audited, a hidden
  mitigation counts but names nothing, the five risk MCP tools answer as
  their routes do, and the database keeps the scheme to its
  administrators. In the browser the method, detection, levels, values,
  rating in the table, its pages, the matrix's sides and every download
  are used and their effect checked (#325).
- The suite proves the grid's and the search's pages, filters and views:
  every window of the grid and the search holds each row once in the order
  asked for, the search narrows by owner and day, a token limited to one
  project searches only there, `search_requirements` and
  `list_requirements` answer as the API does, a view is refused when
  changed or deleted by who may not and stays as it was, and every control
  of the grid and the search page is used in the browser, a shared view
  also by a second person (#311).
- The suite proves what a requirement's restore, copy, hand lock and trash
  refuse (#309), each with its status and code and the rows left as they
  were; that an archived project, a name forged on a revision or a change
  and a 51st label are refused straight through SQL; and that the history
  tools answer as their routes do. In the browser it locks and unlocks by
  hand, copies with what is below, takes an owner off, empties the trash,
  walks a long history, and writes with every tool of the editor, a link
  and a mention.
- The suite proves the moves in the tree, the specification page and its
  exports: every refusal of a move leaves the tree as it was, a mapped move
  lands in the status mapped, `requirement.moved` is posted once for each
  requirement moved, `get_tree` answers as the tree does, each entry says
  what its reader may do, a document is read for a nested section, past
  its 1000 entries and without the links its reader may not follow, and
  every control of the specification page is used in the browser (#310).
- The suite proves the guards on reuse and the via stamp (#321): a reuse
  is refused into a project its person only reads or that is archived, a
  take and a variant are refused in the trash and while locked, a take
  brings only the files, steps and rating values its source shows, and
  reviews, test runs, Armature and Stator links name the token they came
  through, also when written straight through SQL.
- The menus follow the layout Armature, Exciter and Stator share (#280).
  The top bar is gone: the breadcrumbs sit above each page's title, the
  bell, Search and Projects join Home on the rail, and the avatar at the
  rail's foot opens "Your account" with a header naming the person, their
  own pages, the other tools, Settings for administrators and Sign out. The
  installation's settings left the sidebar for a settings area at
  `/settings`, with a sidebar of its own that lists You and, for
  administrators, Settings, and a way back to the page you came from;
  `/settings` itself is a hub linking both. The sidebar counts unread
  notifications and waiting reviews on their entries, keeps the last
  project in view where the page names none, and folds Trash, Project
  settings and Permissions under Project admin. A new Your profile page
  shows who you are and is where the interface language is chosen, still
  saved to your account.

### Added

- A connected app can take a narrower, shorter access token for one run
  without narrowing what the person allowed it (#235). `POST /oauth/token`
  exchanges an access token of the app's own grant (RFC 8693) for one that
  only reads, reaches fewer projects (`exciter_projects`) or ends sooner
  (`exciter_expires_in`), never outliving the token it came from and with no
  refresh token. Revoking it ends it alone, and ending the grant ends it
  too. Apps may now register for the token exchange grant.
- The account menu links the other tools (#298). Under "Switch to" it lists
  Armature and Stator, with their icons, once the installation is connected
  to them, and each opens that tool's home at the connection's address. The
  links are plain links, so they open in a new tab too, and everyone signed
  in sees them, guests included; a tool whose connection is removed leaves
  the list, and with none the group is not shown.

### Fixed

- The Ctrl K palette no longer offers the requirements found for an earlier
  word while the answer for the word now typed is still on its way (#331).
  Pressing Enter then could open a requirement that does not match it.
- The database refuses a Stator page link whose address is not under the
  connected Stator's (#327). The service always wrote it there, but a
  writer straight through SQL could point a link everyone clicks anywhere.
- A requirement's link to a Stator page stays in the space the page was
  moved to when Stator delivers the publish of that version late (#327).
  The move did not keep its version, so the late publish moved the link
  back to the space the page had left, with that space's address.
- A webhook listening to a project hears that a requirement moved out of
  it to another project (#328). Only the webhooks of the project it moved
  to, or of every project, were posted `requirement.moved`.
- The matrix paging browser test waits up to 20 seconds for each page of
  five thousand cells to show (#391). On a busy shard they rendered for
  longer than the default five seconds after the page had arrived.
- Tracing a requirement to an Armature issue, removing such a trace or
  filing issues from an archived project says the project is archived
  (409 `project_archived`), as confirming a trace there already did
  (#326). It answered 403 and told an editor to ask for edit rights.
- Starting a test execution with testers it refuses reads the refusal out
  with the testers (#416). It showed below them, but a screen reader did
  not tie it to them.
- Saving a project's details says "Saved." (#319). The form started again
  from the saved project and lost the word before anyone read it.
- Saving the writing rules or the word lists says so (#320). The cards
  started again from the settings saved and lost their notice, so a save
  looked as if nothing had happened.
- Changing a term somebody may only read, or one of an archived project,
  is refused as not theirs to change or as archived (#320). It answered
  that the term was not found, though the same person could read it.
- Linking a pair of duplicates by a link type the project does not use
  says why in the dialog (#320). The refusal named a field the dialog
  does not have, so nothing was shown and the link was simply not made.
- The grid shows a computed attribute's value as text (#319). It offered a
  box to type a value in, which every save then refused.
- A dashboard widget counting by an attribute is refused when it is saved
  with formatted text or another project's attribute (#319), on the
  attribute's field. It was saved and then could not be read.
- A .reqifz whose document unpacks past what one import takes is refused
  as the file, naming the document, as a Word document that does is
  (#318). It answered with a server error.
- Only the person who uploaded a file maps and starts its import, straight
  through SQL too (#318). The database let another administrator of the
  project change an import's mapping or choices and start it; they still
  read and stop it.
- A tool call refused on its fields tells the assistant what each field
  needs (#324), as a form shows it beside the field. It said only that some
  fields need attention.
- A tester recording, uploading or filing an issue in an archived project's
  execution, and its manager changing or closing it, are told the project
  is archived (#324), as is whoever changes or deletes one of its plans.
  They were told it was not theirs to do.
- A CSV or ReqIF import that could not be stopped says why under its
  progress (#318). Stop went back to waiting without a word.
- A person a CSV file names who is nobody in the project shows "Pick who
  it is" until answered, and picking Nobody answers it (#318). The list
  showed Nobody already, which a browser does not let anyone pick again,
  so the import could not start.
- Each accessibility check in the browser suite adds 15 seconds to its
  test's time (#389). On busy CI shards axe took 12 to 13 of the 20
  seconds a test has, so the members and specification tests ran out of
  time with nothing wrong in them.
- The audit log records an FMEA workbook as it records every other export,
  naming its project under project and its kind as risks (#325). It named
  them projectKey and of, so a search of the log for a project's exports
  missed its workbooks.
- The risk matrix of a project without a risk scheme names its two scales
  as the API document says it does (#325). They came without a dimension,
  which the document does not allow. The document now also names the
  severity and probability that narrow the FMEA workbook to one cell.
- The FMEA workbook names a mitigation from a project its reader may not
  view as "A requirement you may not view", with its coverage and whether
  it is in effect, as the table and the Word report do (#325). Its row
  was empty, so the risk read as mitigated by nothing.
- A mitigation keeps joining a requirement that is no risk to a risk when
  either end changes its type (#325). A risk could be given a type of
  requirements, its mitigating requirement a type of risks, or that
  requirement's type marked as risks, after the link was made; each is
  now refused, through the API and straight through SQL alike.
- The exciter-ci check against the stack reads a scan's tags and a run's
  results until a replica shows them, rather than once (#401). Read as
  the owner, not the token that wrote them, they could still be missing
  and fail the integration job on main.
- A transition one of its conditions keeps from somebody is refused with
  that condition (#322), such as "Only the requirement's owner may take
  it.". It said they might read the project but not change its
  requirements, also to an editor.
- A workflow's rule asking for links of a type takes only a link type of
  its own project or of every project (#322). One of another project was
  saved, and no requirement could ever meet it.
- Saving a fixed field's rule says it is saved (#322). The form was drawn
  again with the saved rule and the notice was lost. A status or a
  workflow still used once says "once" rather than "1 times".
- The project's trace settings say "Saved." once the counting branch or
  the tag words are saved (#323). The form started again from what was
  saved and lost the notice, so nothing said the save had gone through.
- An earlier review on a requirement's page names who decided it and what
  they said (#317), so its author reads why a revision was rejected. Only
  the review still waiting showed its decisions.
- The people offered after an at sign are listed inside the page's main
  content, or the dialog the editor is in, so a screen reader finds them
  where the text is written rather than at the end of the page (#377).
- The suite demo links each requirement it made, also when the replica a
  step reads from has not caught up with the steps before it (#365). It
  then said its links waited for issues Armature did not have yet.
- Deleting a baseline on its own page leads back to the project's
  baselines and says "The baseline <name> is deleted." (#316). The page
  asked for the deleted baseline again and stayed on "That baseline was
  not found" instead.
- The impact file of a trace graph marks an item suspect only when the
  link that reached it is, as the graph's page does (#314). It marked a
  requirement suspect for a suspect link leading on from it as well.
- Linking a requirement of an archived project, or removing a link whose
  ends are all in archived projects, is refused as archived (409
  `project_archived`), as confirming such a link already was, rather than
  telling an administrator to ask for edit rights (#313).
- Watching a project that cannot be changed now says so on the project's
  page, as the requirement's watch menu does, instead of nothing (#312).
- Stopping the last watch on a later page of Watching goes back to the
  page before, instead of saying you watch nothing (#312).
- The discussion of a requirement in the trash or of an archived project
  says it takes no comments while it is there (#312). It told an editor
  to ask for a role they already held.
- A file is no longer attached to a requirement in the trash straight
  through SQL (#312). The service refused it, but the database let an
  editor attach one there.
- Sharing a view, or changing a shared one, in an archived project is
  refused as archived (409 `project_archived`), saying to unarchive the
  project, rather than telling an administrator to ask for edit rights
  (#311). Taking a view back from sharing still works there.
- An assistant whose query is refused through `list_requirements` or
  another tool is told the character it went wrong at, as the grid points
  at it, beside the sentence (#311).
- The grid no longer offers to edit a title its reader may not change in
  the requirement's status, which the server refused after it was typed
  (#311). The title shows as it is, as the other fields a rule closes do.
- A view whose delete is refused after a refused save now says why the
  delete was refused, not the save's reason again (#311).
- Saving a link in the editor saves only the link (#309). "Save link" also
  submitted the form around the editor, so a new requirement was written
  before the rest of it was typed, an edit was saved as a revision, and a
  comment was posted.
- A save that sends a requirement's statement back as it was read no
  longer counts as changing it (#309). Where the field rules kept the
  statement from a role, its saves of the title were refused whenever the
  statement came back with its keys in another order than the database
  keeps them, as a script's or an assistant's do; and a save changing
  several attributes the rules refuse is now refused on the same one every
  time.
- A requirement carries at most 50 labels also when written straight
  through the database (#309). Only the server held the bound before.
- A requirement out of the trash no longer goes below one in the trash, even
  when written straight to the database (#310). It vanished from the tree
  and the specification without showing in the trash. The API refused such
  a place already; the database now refuses it too.
- A signature interrupted by a sign-in at the identity provider is finished
  on the requirement it was for when the specification shows several
  reviews waiting (#310). The first review of the document took the way
  back for itself and dropped it, so the signature never reopened.
- Once a page has shown you something the server finished in the
  background, such as a ReqIF import done, the next pages show it too
  (#374). While the database's read replica was behind, one page could be
  answered from the primary and the next from the replica, so an import read
  as done and the project's tree came back empty until a reload.
- Something saved while its list is still loading for the first time now
  shows in that list (#372). The list used to come back as it stood before
  the change and kept that until the page was reloaded. It showed on a busy
  server, for instance as a partner app missing from the list right after
  it was registered.
- The FMEA table and the risk matrix read their mitigations in one pass and
  work out each mitigating requirement's coverage once, rather than once for
  every risk it mitigates, so a project with a few hundred mitigations opens
  several times faster and no longer runs a busy server out of time (#370).
- A request that runs past the server's time limit answers 503 `timed_out`
  with a `Retry-After` header, saying to try again in a moment, instead of a
  500 that reads like a fault (#370).
- `make suite-up` connects the suite again against current Armature and
  Stator (#368). Alice and bob, whom Armature's own seed now lets in, get
  the suite's role and password; Stator's Armature connection no longer
  names an organization; Exciter is registered at Stator with the fields
  and scopes Stator takes, and anew on a rerun, since Stator does not
  rotate an app's secret; and Stator gets the space `SUITE`, so
  `make suite-smoke` finds a page to link.
- With `telemetry.serviceMonitor.enabled`, the chart's ServiceMonitor
  scrapes the worker's metrics as well as the api's (#359). The worker has
  a headless Service of its own, `<release>-worker`, for the scraper.
- A mail relay that accepts the connection and then stops answering no
  longer holds back every notification mail and digest after it (#356).
  Each send gives up after 30 seconds, or sooner when the worker is
  stopping, and the worker logs the mail it could not send and carries on.
- The database holds a workflow to its own project's statuses, for every
  step and the status a new revision sends it to, and keeps a status or a
  workflow from moving to another project while a step names it (#340).
- Turning a link type's acyclic flag off first checks that you may change
  the type, and a refusal because a formula reads along it names the
  computed attribute only when you may view its project (#339).
- A theme's extra CSS is held to loading only the theme's own files and
  inline images through `image-set()`, `image()`, `cross-fade()` and `src()`
  as through `url()`, and may no longer contain CSS escapes (#343). Write
  each character as itself instead of a backslash escape.
- A test result links its commit to where Armature shows it only when that
  address is a web address, `http` or `https` (#344); any other commit is
  shown by its SHA alone.
- Outbound calls through a proxy from `HTTP_PROXY` or `HTTPS_PROXY` are
  checked for their destination as direct calls are (#341). A destination
  goes to the proxy only when none of its addresses lies inside the network,
  or `EXCITER_OUTBOUND_ALLOW` names it; a name this server cannot resolve
  itself goes through a proxy only when `EXCITER_OUTBOUND_ALLOW` names it.
- Outbound calls treat every address the special-purpose registries set
  aside as inside the network, and judge an address under a translation
  prefix by the IPv4 address it carries (#342). `EXCITER_OUTBOUND_ALLOW`
  still lets a named range through.
- The suite's seed makes Exciter's webhook at Stator, as an administrator
  connecting Stator does, and stores its secret on Exciter's Stator
  connection (#358). Page changes in the suite now reach Exciter, and the
  suite smoke watches a link follow a page published in Stator.
- A refresh of a sign-in's grant that Armature or Stator refused or did not
  answer is not asked again for two minutes (#349). Reads and the link worker
  answer rejected or unreachable meanwhile instead of asking the partner on
  every use; checking the account asks at once.
- When Armature or Stator does not answer the token exchange at sign-in in
  time, the account page now says so (#348). The outcome used to be lost
  with the time that had run out.
- A Stator page event that arrives late, for an older version than a link
  holds, no longer moves the link back to the space the page left (#347).
- Sending a webhook delivery again by hand no longer ends its automatic
  retries (#346). The next retry after a failure takes the attempt after the
  highest one logged, so a redelivery in between leaves it scheduled.
- Filing a failed test run as an Armature issue on a step the case does not
  have is refused, naming the step, before anything is filed (#345). It used
  to leave an issue in Armature that Exciter never linked back to.
- The specification's tree stays below the page's header and scrolls on its
  own to its last entry, without moving the document (#361). On a wide
  screen the tree sits at the left of the page, a little wider, and the
  document beside it keeps a readable width.
- Groups and their memberships are held to administrators in the database
  too, not only by the service (#335). Everybody still sees the groups a
  grant or a picker names, and sign-in through the identity provider keeps
  joining, leaving and creating provider groups as before.
- An Armature issue shows the requirements it implements or verifies as
  requirements, no longer among its pages (#297). Where the connected
  Armature takes relations, link sync sends `requirement` for an implements
  or verifies trace and `page` for any other; links synced before are
  corrected by the requirement's next sync.
- A requirement in the trash comes back with Restore although its status
  was made to lock while it was there (#350). It returns locked as its
  status says; what it says still takes no change in the trash.
- A review session of an archived project can no longer be changed, moved
  to a new round or closed, by its moderator or the project's
  administrators, in the app and in the database (#351). The session says
  so and offers none of them until the project is unarchived.
- Coverage follows a link type at once when what it counts changes or it is
  retired (#352), instead of answering the old figures for up to a minute.
- The notification settings list every kind of notification, so review
  sessions, test executions, code scans, glossary terms and revised library
  sources can be switched off in the app and by email too (#353).
- Moving a requirement to a project that lacks one of its statuses works in
  the move dialog (#354). The dialog asks where the requirements in each
  such status go, and asks again for every status that moves when the
  server says a type's workflow there lacks one.
- The owner of a shared view who no longer edits its project is told why
  the view cannot be changed while it is shared, instead of getting a
  server error (#355). Taking the view back still works, and then it is
  theirs to change.
- The MCP test of clearing a suspect link checks that the link the tool
  answers has no ends left that moved on (#357); it compared with a flag
  the answer never has, so it could not fail.
- Computed attributes whose formula calls `today()` are worked out again
  each day (#360). Shortly after midnight UTC the worker marks the
  requirements those formulas show on, and nothing else; what reads such a
  value elsewhere follows as with any change.

## [0.1.0] - 2026-10-21

### Added

- The suite demo draws each tool's architecture (#286). The projects `ARM`,
  `EXC` and `STA` open with a section "Architecture" after the
  introduction: PlantUML diagrams of the tool's parts and data stores, its
  deployment, how people sign in and how it reaches the other two tools,
  each drawn from the tool's own documentation and linked to its page in
  Stator.

- Architecture drawings in PlantUML (#284). The editor's PlantUML diagram
  button puts a diagram into a statement, rationale or rich text value,
  written as PlantUML with a preview drawn below it as it is typed. Every
  view shows the drawing, the document view, history, baselines and print
  included, with the drawing to save as SVG; one that cannot be drawn shows
  its source and why. History compares a diagram's source, search finds its
  words, Markdown writes it as a `plantuml` fence, and Word embeds it as a
  picture. Diagrams are drawn by a Kroki container that compose and the
  Helm chart now run beside the api (`EXCITER_KROKI_URL`, Helm `kroki`);
  without one, diagrams show as their source.

- A suite demo of the three tools' own requirements (#270). An
  administrator adds it once under Installation, or `make seed` does: the
  projects `ARM`, `EXC` and `STA` hold the user stories and acceptance
  criteria of Armature's, Exciter's and Stator's issues, filed by milestone,
  approved where the issue is closed, with their dependencies traced and a
  baseline of each. Where Armature and Stator are connected and have their
  own demo, each requirement links to its issue and to the API contract and
  architecture pages that describe it; linking again adds what a partner
  could not take before. `make suite-up DEMO=1` adds it to all three.

- People connected to Armature and Stator as they sign in (#274). Where the
  three tools share one Keycloak realm and the operator switches it on
  (`EXCITER_ARMATURE_TOKEN_EXCHANGE`, `EXCITER_STATOR_TOKEN_EXCHANGE`, Helm
  `armature.tokenExchange`, `stator.tokenExchange`), Exciter exchanges the
  person's sign-in at each partner that offers `oauth.token-exchange@1`, as
  the connected app whose client id and secret an administrator stores on
  the connection, so nobody pastes a token. The tokens are renewed as they
  are used, the link worker's included; a token somebody pasted stays, and
  the account page says in a sentence why a partner is not connected. A
  failed exchange never fails the sign-in.
- Partner apps connect people with no pasted token (#273). An administrator
  registers a tool beside Exciter, such as Stator, with the Keycloak client
  it signs people in with, under Installation or at `/oauth/clients`, and
  is shown its secret once. The tool then trades a person's access token
  from the shared sign-in for a token here (RFC 8693) at
  `POST /oauth/token`, with no consent page: the person is matched as at
  sign-in and let in by the group mapping, the token reads no more than
  the app may and the person can, and it shows under the person's
  connected apps, where it is disconnected. `/meta` lists
  `oauth.token-exchange@1`, with Armature's promise.

- Risks in traceability, baselines, the risk report and project templates
  (#261). The trace graph and the chain report mark a risk with its level
  and state; a baseline records each risk's levels, state and mitigations
  as they stood, and its document shows them so; the risk report lists
  the levels and every risk with its ratings and mitigations, as PDF and
  as Word through the project's templates. Two project templates set up a
  medical device risk file and a functional safety analysis, with their
  types, scheme, link types and coverage rules.
- Mitigations, the FMEA table and the risk matrix (#260). A requirement
  mitigates a risk through the new link type Mitigates, and the mitigation
  is in effect once the requirement is implemented and verified as its
  coverage rules ask and the link is not suspect; a risk is unmitigated,
  claimed or mitigated, and counts with its rating after mitigation only
  once mitigated. Risks, in a project's navigation, holds the FMEA table,
  which rates risks in place, filters with a query and exports as Excel,
  and the risk matrix, before mitigation and as the risks stand, a cell
  opening its risks. Leads to joins one kind of risk to the next.
- Risks and their ratings (#259). A project's administrators mark a
  requirement type as risks and set the project's risk scheme: the values
  of severity, probability and, if wanted, detection, the levels with their
  colours, and a matrix or a priority number with thresholds that gives a
  rating its level. A risk is rated before and after mitigation; the rating
  is part of each revision and its hash, history compares it value by
  value, and the document, Word, Markdown, CSV and ReqIF carry it. Queries
  ask for severity, probability, detection, the priority number and the
  level. A value a rating uses stays on its scale.
- The API contract for M13, Risk management (#258):
  `docs/api-contract-m13.md` agrees how a requirement type marked as risks
  gives its requirements a rating before and after mitigation on the scales
  and levels a project's administrators set, how a matrix or a priority
  number gives the level, how requirements mitigate a risk and when a
  mitigation is in effect, the FMEA table and the risk matrix, the marks in
  the trace graph and chain report, what a baseline records, the risk
  report and two project templates. The operations it adds answer 501
  until they are built.
- Test executions in traceability (#252). A CI upload on the counting
  branch fills the open test executions holding the test cases it verifies,
  never over a tester's result or a newer upload. A coverage rule may count
  test executions for verified: a requirement is verified in full when each
  test case verifying it last passed at its current revision through a link
  that is not suspect. The query language gains `testCase`, `runResult`,
  `runOutdated` and `casesResult`; the chain report and the matrix mark a
  test case with its last result; a baseline records the last results of
  the test cases it holds.
- Test plans and test executions (#251). A project's editors group test
  cases into plans, picked in order or found by a query, and start an
  execution of a plan against a baseline, a version and a configuration,
  naming its testers, who are told and reminded. A tester runs each case
  step by step, records passed, failed, blocked or skipped with the actual
  result and files, and files a failure as an Armature issue linked to the
  case and the requirements it verifies. The manager closes or aborts it;
  a closed execution never changes, and its report downloads as PDF or
  Word. MCP tools read and record them.
- Test cases with steps (#250). A project's administrators mark a
  requirement type as test cases; its requirements carry preconditions,
  ordered steps with an action and an expected result each, and parameters
  whose values line up into iterations. They are part of every revision:
  history compares them step by step, a restore brings them back, and
  baselines, the document, Word, Markdown, CSV and ReqIF carry them. The
  editor adds, moves and removes steps; the MCP tools read and write them
  as Markdown.
- The API contract for M12, Test management (#253):
  `docs/api-contract-m12.md` agrees how a requirement type marked as test
  cases gives its requirements preconditions, steps and parameters, how
  test plans group them and test executions run them against a baseline, a
  version and a configuration with testers recording each step, how a
  failure is filed in Armature, and how results from CI and executed test
  cases count in coverage, the matrix, the chains and queries. The
  operations it adds answer 501 until they are built.
- Exports through Word templates, and as an HTML site (#214). A project's
  administrators upload a Word file whose styles, cover, headers and
  footers an export takes, with `{{content}}` where the requirements go and
  placeholders for the project, title, section, baseline, date, exporter
  and signers; one that cannot be used is refused with what is wrong. A
  template chooses which fields a requirement shows and in which order,
  whether its links and open comments come along, and may mark what changed
  since a baseline as tracked changes. Shared, a template is offered in
  every project. Exporting a specification, a section, a baseline or a
  review report asks which template to use. A specification or a baseline
  also downloads as a static HTML site in a zip, with linked pages, its
  pictures and a search, that opens offline.
- Dashboards over a project's requirements (#212). A dashboard is a person's
  own until shared with everybody who may read the project; its link is its
  address. Widgets break the requirements down by status, category, type,
  owner, priority, label or an attribute, computed ones included; count or
  list what a query or a shared view keeps; and show coverage, review,
  suspect links, changes over time and quality. Every widget is read live as
  the reader, and a slice or a bar opens its requirements in the grid. A
  dashboard starts from a template, is laid out by dragging or by buttons,
  prints to PDF, and a project's administrators may show one on its home.
  The MCP tools `list_dashboards` and `get_dashboard_widget` read them.
- Computed attributes and roll-ups (#213). A project's administrators define
  an attribute by a formula over a requirement's own fields and attributes,
  with arithmetic, conditions, text and dates, and over its children or the
  requirements its links lead to with count, sum, min, max, avg, any and
  all: a priority score, a section's share of approved children, a status
  rolled up from linked tests. The editor completes names, marks what is
  wrong where it is and tries the formula on a requirement. A formula that
  would read itself round is refused with the attributes named. The worker
  works values out again whenever what they read changes, and the whole
  project after a formula changes, saying how far it has got. Computed
  values are read-only, are not revisions, are found, ordered and exported
  like any attribute, and a baseline records the values it saw.
- A requirement's trace graph (#211). From a requirement's page, "Trace graph"
  draws everything it traces to: up to what it derives from, refines,
  satisfies or verifies, and down to what derives from it, the Armature
  issues and Stator pages linked to it, the tests that verify it and the
  code that tags it, to a depth of up to six levels and across projects.
  Link types and relations narrow it. Suspect and broken links, coverage
  gaps, failing tests and outdated evidence are marked. A requirement the
  reader may not see is counted on its neighbour and never named. Choosing
  a node says what it is and centres the graph on it or opens it; every
  node is reached by keyboard, and the graph reads as a nested list too.
  Beside it, the impact list names everything a change reaches, level by
  level, and downloads as CSV. The MCP tool `get_trace_graph` reads it.
- The API contract for M11, Analysis and reporting (#242):
  `docs/api-contract-m11.md` agrees how a requirement's trace graph shows
  everything it traces to and what a change would affect, how attributes
  compute themselves from a formula over a requirement, its children and
  its links, how dashboards over a project's requirements are built,
  shared and followed into the grid, and how a specification is exported
  through a Word template and as a static HTML site. The operations it adds
  answer 501 until they are built.
- A Word document is imported as a specification (#209). A project's
  administrators upload a `.docx` into the project or below a section; its
  headings become sections, and its paragraphs, lists, tables and pictures
  become requirements, one per paragraph or one per heading, as styles, a
  numbering pattern or keywords such as shall decide. A table becomes one
  requirement or one per row, its columns mapped to fields and attributes.
  A preview shows the tree with every choice and loss before anything is
  written, and the worker imports it with its progress, as a ReqIF import
  does. Importing the same document into the same place again updates what
  the first import made as new revisions.
- Requirements that say the same thing are found (#208). While writing, a
  hint lists the requirements of the project and of library projects most
  like the draft, above a similarity the administrators set. A project page
  lists likely duplicate pairs, most alike first, narrowed by type, section
  or query. A pair is dismissed with a reason until either side is revised,
  linked with a link type, or merged: one side goes to the trash and its
  links are made again on the other. Assistants read the similar
  requirements of one over MCP.
- A glossary of terms, marked where requirements use them (#210). A
  project's editors define terms with a definition, synonyms, words not to
  use instead and a status, kept in revisions, and a project uses the
  glossaries of library projects too. The editor marks each term a
  requirement uses, in any inflection, and shows its definition; an author
  links words to a term on purpose. A word not to use is a finding of the
  writing rules. Changing a definition makes the requirements that use the
  term suspect and tells their owners, who confirm them. Terms are found by
  search and the query language's `term`, and the Word, PDF, Markdown and
  ReqIF exports carry the glossary.
- Writing rules check every requirement as it is written (#207). In
  English or German, as a project chooses, they find vague and unbounded
  words, escape clauses, open placeholders, and/or, more than one shall,
  negative statements, the passive voice without who acts, pronouns
  without a referent and sentences that fit no EARS pattern. The editor
  marks each finding as one types and says how to fix it; an author accepts
  one with a reason, which stays with the revision. Every requirement has a
  score, shown in the specification, the grid and a project quality page,
  and the query language gains `quality` and `qualityFinding`. A project's
  administrators switch rules on and off, set their severity and the types
  they apply to, and add their own words. A workflow transition can ask for
  a least score, and assistants check a draft over MCP.
- The API contract for M10, Authoring quality (#233):
  `docs/api-contract-m10.md` agrees how writing rules check a requirement
  as it is written and score it, how a project's glossary marks the terms
  requirements use and makes them suspect when a definition changes, how
  requirements that say the same thing are found, dismissed, linked or
  merged, and how a Word document is imported as a specification and
  imported again. The operations it adds answer 501 until they are built.
- Code traces to the requirements it implements and tests (#201). A source
  comment tags a requirement with `[implements CHK-R42]` or
  `[verifies CHK-R42@3]`, in words a project sets. `exciter-ci scan` uploads
  what a checkout's comments carry, replacing its branch's scan before.
  A requirement lists the files and lines tagged with it, linked at their
  commit. A tag naming an older revision is outdated, and one naming a
  requirement in the trash or none is orphaned; the project lists them and
  owners are told. Coverage can count code for implemented and verified,
  the query language gains `codeLink` and `codeOutdated`, the matrix takes
  files as columns, a trace model can end in code, and the trace check can
  fail on orphaned tags.
- Test results from CI verify requirements (#200). A pipeline uploads its
  JUnit reports with a token of the new evidence scope, through
  `exciter-ci results`, and each test case that names a requirement, as
  `[verifies CHK-R42]`, lands on it with the commit, the branch and the run.
  A requirement shows its latest result per test and branch, and a newer
  revision marks it outdated. A requirement type's coverage can count passing,
  current tests as verified. The query language gains `testResult` and
  `testOutdated`, the matrix takes tests as its columns, and a trace model
  can end in tests. `exciter-ci check`, on the trace check, fails a pipeline
  on broken chains, gaps and failing tests. The branch whose results count
  is a project setting, and baselines record the results they held.
- The API contract for M9, Verification evidence (#224):
  `docs/api-contract-m9.md` agrees how CI uploads test results and the
  requirement tags found in source code with a token of the new evidence
  scope, how evidence names the revision it was made against and turns
  outdated, how coverage, the matrix, the chains and queries count it on the
  project's counting branch, and how a trace check passes or fails a
  pipeline. The operations it adds answer 501 until they are built.
- Review sessions (#204). A project's editors send a section, a baseline's
  requirements or a query's, optionally only what changed since the last
  approval or a baseline, to approvers and reviewers as one review with a
  deadline and the share of approvers each requirement needs. Participants
  are told, find the session in their inbox and decide in the document's
  review mode, with signatures where approvals are signed; a reminder goes
  out a day before the deadline. The session page shows each requirement's
  and each person's progress; the moderator starts a new round after
  changes, which asks again only of what was revised, and closes the
  session, withdrawing what is pending and taking a baseline of what was
  approved. Its report downloads as PDF and Word.
- Field rules (#206). An attribute, or a requirement's own field such as its
  rationale, owner or priority, can be required from a status on; a select
  attribute can depend on another, offering only the choices the other's
  option allows; and a field can be changed only by some project roles in
  some statuses, which the editor, the page and the grid show read-only
  with who does. Transitions can ask for links of a type, no suspect links,
  full coverage in a dimension and resolved discussions: the transition is
  still offered, and taking it says which requirement lacks what, one by
  one in the document's bulk transitions too. The database holds the
  choices and who changes what, for every writer.
- Signed baselines and locked requirements (#205). A project's
  administrators name who signs its baselines and how many signatures
  release one, under Settings, Approval. A signer signs on the baseline's
  page with the meaning shown and the password again, or a fresh sign-in at
  the identity provider, as approvals are signed; the signature keeps the
  hash of what the baseline holds. A signed baseline keeps its name, is
  never deleted, and says so in the list, on its page, on its Word and PDF
  cover, in Markdown and in its matrix files. A status can lock its
  requirements, and an administrator locks one by hand with a reason: a
  locked requirement takes no new revision, move, link from it or trash,
  from the page, the imports or an assistant alike, says why on its page
  and in the document, and still takes comments, reviews and transitions.
- The API contract for M8, Review sessions and control (#216):
  `docs/api-contract-m8.md` agrees how baselines are signed, how
  requirements are locked by their status or by hand, how fields are
  required from a status, depend on other fields and are changed only by
  some roles, which trace a transition can ask for, and how a section, a
  baseline or a query is reviewed as one session in rounds and closed with
  a report. The operations it adds answer 501 until they are built.
- What an assistant wrote says so (#190). A revision, a change, a comment, a
  review asked for, a link or an audit entry written through a personal
  access token or a connected app carries `via`, which one and its name at
  that moment, written by the database and never by a caller. The history,
  the activity, comments, pending reviews and the audit log show "through"
  and the name, a token revoked keeps its name on what it wrote, the audit
  export names it in two last columns, and webhooks carry it on their
  actor.
- Connected apps (#189): under the account menu, beside the access tokens, a
  person sees the apps they connected through OAuth, the host each sends
  back to, whether it only reads, the projects it reaches and when it was
  last used, and disconnects one, which ends its tokens at once.
  Administrators see and disconnect everybody's under All connected apps.
  The audit log records each connection ended and who ended it, and the
  token page's Connect an assistant says how to connect an app with the
  address alone.
- Assistants connect with Exciter's address alone (#188). Exciter is the
  OAuth authorization server of its MCP endpoint: a client that meets the
  401 reads where to go, registers itself or names its metadata document,
  and sends its person to the new Connect an app page, which names the app
  and where it sends back. The person allows it, narrowed to reading or to
  projects as a token is, or denies it. The access token lasts an hour, is
  good at the MCP endpoint alone, and is renewed by a refresh token that
  turns over at each use; one used twice ends the connection. Switching
  somebody off ends their connections, and administrators can switch OAuth
  connections off on the installation page. The audit log records each
  connection and each switch.
- An assistant reads a specification whole (#187): MCP resources hand out a
  project's specification, one section of it, a requirement with what is
  below it, or a baseline as one numbered Markdown document, read as the
  caller and cut between two entries with the address to read on from.
  Three prompts start an assistant on reviewing what changed in a section,
  drafting requirements from a text, and checking one requirement's
  quality. The document also downloads as Markdown, from
  `GET /projects/{projectKey}/document.md` and
  `GET /baselines/{baselineID}/document.md`.
- Requirements and comments are read and written in Markdown (#186), by
  scripts and assistants alike. `format=markdown` answers statements,
  rationales and comment bodies as Markdown beside the editor's documents,
  and writes take `statementMarkdown`, `rationaleMarkdown` and
  `bodyMarkdown`. Tables, lists, code, links, the requirement's own pictures
  and files, and mentions come and go as they were; what would not come back
  as written is refused with a sentence. The MCP tools read and write
  Markdown unless told otherwise. `docs/markdown.md` lists the spelling.
- An assistant works around a requirement as its person does (#185): MCP
  tools ask for reviews of one requirement or many, withdraw one, read what
  waits for the caller and where an approval stands; read, answer, rewrite
  and resolve comments; find and confirm suspect links; read coverage, the
  matrix and the trace chains; and find Armature issues and Stator pages,
  link requirements to them and file issues from requirements. No tool
  decides a review.
- An assistant works with the specification as a person does in the
  document view (#184): sixteen new MCP tools read the specification with
  its changes against the previous revision, the last approval or a
  baseline, list a project's types, attributes, statuses and link types,
  move and label requirements, read their activity, move many through the
  workflow in one step, and take, read and compare baselines.
- Assistants connect over a newer MCP revision (#183). Exciter speaks
  `2025-11-25` beside `2025-06-18`, answering in the one the client asks for,
  and tells the model how keys, revisions and the specification's numbering
  work. Every tool now says what it answers, as a schema the answer is held
  to, and whether it only reads, keeps what it replaces, can be repeated and
  reaches Armature. An answer larger than 64 KiB is refused with a sentence
  asking to narrow the query or page, since a cut one would not fit its
  schema, and a call from a page of another site is refused with a token as
  well as with a cookie. `/meta` lists `mcp@1`, and `docs/mcp.md` says how
  to connect a client with a token.
- The API contract for M7, MCP integration (#182): `docs/api-contract-m7.md`
  agrees how assistants work with requirements over MCP as the people they
  act for: the current protocol, tools for reviews, comments, trace,
  baselines and the document view, requirements in Markdown, the
  specification as resources and prompts, connecting with OAuth, connected
  apps, and marking what an assistant wrote. No tool decides a review. The
  operations it adds answer 501 until they are built.
- Requirements are reviewed in the specification (#171). Review what waits
  for me narrows the document to the requirements whose review waits for
  the reader's decision, marked with what changed since their last
  approval, and each entry takes the approval or the rejection in place,
  with the signature where approvals are signed. A decided entry stays in
  view, marked, a count says how many are decided, a review a newer
  revision superseded says so, and comment threads open inside the entry.
  An author asks for the review of the picked requirements, or of what
  changed in a section, in one step, and is told which were asked for and
  which already waited. The reviews page opens each project's document in
  review mode. `POST /projects/{projectKey}/reviews` asks for up to 100,
  each on its own; spec entries carry their pending review, and
  `GET /projects/{projectKey}/spec` takes `waiting`.
- The specification shows what changed (#170). Show changes compares each
  requirement with its previous revision, its last approval or a baseline,
  and marks in place the words inserted and deleted in its title, statement
  and rationale, its attributes before and after, and whether it is new,
  unchanged or moved, with a link to its whole comparison. Only changed
  collapses the rest to their headings, next and previous change step
  between the changed ones, and against a baseline the requirements it held
  that are gone are listed. All of it is kept in the page's address.
  `GET /projects/{projectKey}/spec` takes `changes`, `baseline` and
  `onlyChanged`, and every comparison, `GET /requirements/{reqKey}/diff`'s
  too, marks the title's words as it marks the statement's.
- Requirements move through their workflow from the specification (#169). A
  requirement's status opens the transitions open to the reader, with the
  comment one asks for. Entries are picked one by one, as a range with
  Shift, or a section at once, and the bar below takes a transition every
  picked requirement has on all of them, saying which moved and why the rest
  did not. The document narrows to status categories or a status, kept in
  its address. `GET /projects/{projectKey}/transitions` reads the
  transitions of up to 100 requirements, `POST` takes one on each in its own
  transaction, and `GET /projects/{projectKey}/spec` takes `status` and
  `category`, and its entries carry their status, which they left empty.
- Requirements are edited where they are read in the specification (#168).
  Edit turns the entry itself into its editor, one entry at a time, and an
  entry adds a requirement or a section after it or first below it, written
  where it will stand. Leaving a draft with changes asks first, and a save
  over a newer revision shows what that revision changed before it is saved
  over, on the requirement's page too. The arrow keys move between entries,
  Enter or E edits one, A adds one after it, Ctrl S saves and Escape leaves,
  each step announced to screen readers. Each entry of
  `GET /projects/{projectKey}/spec` now carries its type, owner, priority and
  approval.
- The API contract for M6, Document view (#167): `docs/api-contract-m6.md`
  agrees how the specification is edited in place, narrowed by status,
  marked with what changed since the previous revision, the last approval or
  a baseline, and reviewed entry by entry, with transitions and review
  requests for many requirements at once. The operations it adds answer 501
  until they are built.
- The matrix, the chain report and a baseline's matrix download as Excel
  workbooks beside their CSV files (#154): a frozen header and key column,
  linked cells green, suspect ones amber, gaps and breaks red, a baseline's
  changes in their own colours, counts as numbers, and an autofilter;
  written without a new dependency.
- A baseline's matrix (#153). Baselines taken from now on also record
  each item's type, whether each link was suspect, each Armature link's
  status, and the Stator page links; broken and detached links are left
  out. A baseline's page shows its matrix against requirements, issues or
  pages, compared with now or another baseline with each link marked
  added, removed or changed, and downloads it as CSV. A baseline taken
  before says what it did not record.
- Matrix and chain views (#152). The matrix and the chain report save
  their setup under a name, personal or shared, as the grid's views do,
  each kind listed only where it belongs; a view naming what the project
  has since lost opens without it and says so.
- A project's trace model and its chain report (#151). An administrator
  sets the levels a project's requirements trace through, such as need,
  system, test and Armature issue, under Settings, Trace model, with the
  link types or relations that lead from each to the next. The chain
  report shows every path from the first level down, marks where one
  breaks, keeps only the broken ones when asked, shows a range of the
  levels, and downloads as CSV.
- Stator pages as matrix columns (#150). The matrix lays requirements out
  against the Stator pages they are traced to, each link named by its
  relation and marked when its page moved on, titled as the reader sees
  them in Stator, in the page, its cells and its CSV.
- Links are made, removed and confirmed from a matrix cell (#149). A cell
  opens a panel beside it with its links; whoever may link its row adds
  one of a chosen type and direction, or relation for an Armature issue,
  removes one, or confirms a suspect one. Anybody else reads them. The
  matrix and coverage follow at once.
- The matrix shows its gaps (#148). A column filter, a query and types
  over the column project, makes the columns what it keeps, linked or not;
  columns are paged as rows are; every row and column shows its count of
  links, a gap marked; and "Only gaps" keeps the rows, the columns, or
  both, without a link. The CSV carries the counts as its second line and
  third column.
- The traceability matrix has its own page (#147), `/p/<KEY>/matrix`,
  reached from Coverage. Its rows are narrowed and ordered as in the grid,
  and it counts only the link types, the direction and, for Armature issue
  columns, the relations you pick. The whole layout is in the page's
  address and in its CSV download. Each cell's links now say their id,
  kind, type and direction, and each row whether you may link it.
- The API contract for M5, Traceability (#146): `docs/api-contract-m5.md`
  agrees how the matrix gains chosen axes and link types, gaps, links made
  in its cells and Stator page columns, a project's trace model and its
  chain report, matrix and chain views, a baseline's matrix, and Excel
  files. The operations it adds answer 501 until they are built.
- Webhook envelope version 2 in the CloudEvents 1.0 shape (#133). An
  administrator chooses it per endpoint under Settings, Webhooks; such an
  endpoint is posted `application/cloudevents+json` with Exciter's
  address as its source, `dev.exciter.` types and version 1's payload as
  its data, signed as before. Version 1 stays the default. Exciter also
  reads version 2 from Armature and Stator, as they agreed to send it, and
  `/api/v1/meta` lists `webhooks@2`.
- Themes are proven to travel between Exciter, Armature and Stator (#59):
  themes exported from running Armature and Stator installations import
  here and export again unchanged, Exciter's own export is a committed file
  the other two are asked to import, a token one product adds later travels
  through the others, and the shared token names are checked against both.
- Requirement reuse and variants (#49). A project's administrators mark it a
  library; its requirements, or a section with what is below it, are reused
  in other projects as copies that follow the source's title, statement and
  rationale. When the source moves on, the copy says so, shows what
  changed, and takes the new revision on request; "Make a variant" gives it
  words of its own while it still sees its source. The query language finds
  them with `reuse`, and watchers hear when a source is revised.
- `GET /api/v1/meta` lists `requirement-list@1`, a project's requirements read
  by a query in the query language with its completion, and `webhooks@1`,
  Exciter's outbound webhooks under Stator's name, for the requirement chips
  and blocks Stator is to show on its pages (#140, #43).
- Stator pages show the requirements linked to them (#42). Once Stator offers
  remote links on pages, the worker puts each requirement on the pages it is
  linked to as the person whose change it was, keeps the title in step with
  revisions and new keys, takes it off when the link goes or the requirement
  is trashed, and leaves off a link somebody took off in Stator. The Stator
  pages panel says when a page does not show the requirement yet, and why.
- Outbound webhooks (#48), as Stator sends them: an installation's
  administrators add an address under Settings, Webhooks, with the events and
  projects it hears, and each requirement, link, review, comment or baseline
  event is posted to it signed with a secret shown once, read as the
  administrator who saved it may see it. Failed deliveries are retried and a
  failing endpoint is turned off; a delivery log, a test ping and sending
  again are at hand. See `docs/integration.md`.
- A query language for the grid (#47), Armature's: one line such as
  `owner = currentUser() AND verifiedBy IS EMPTY ORDER BY priority DESC`
  narrows and orders a project's requirements by their fields, attributes in
  quotes, trace links by type from either end, approval, coverage levels and
  suspect links. The box completes at the caret and points at the character
  a query went wrong at. Saved views keep it, and CSV export, coverage and
  the matrix narrow by it.
- Approvals as electronic signatures (#46). A project's administrators turn
  them on in its settings; then approving or rejecting a revision shows what
  the signature means, and asks for the password again, or for a fresh
  sign-in at the identity provider, every time. The signature keeps the
  meaning, the revision and the hash of its content, and how and when the
  signer proved who they are; the database refuses a decision without one.
- Export a specification, a section or a baseline as PDF and Word (#45). The
  document opens with a cover and contents, and each requirement shows its
  text, a table of its type, status, owner, priority and attributes, and its
  links; a baseline's shows each requirement as it was taken. The PDF is the
  print view printed by a render service as the reader, as in Stator; it runs
  in the compose stack, and in the chart behind `render.enabled`. Word is
  written by the api and works without it. Exports are audited.
- ReqIF import and export (#44). A project, or a section and what is below
  it, downloads as `.reqif` or as `.reqifz` with the pictures and files its
  text shows, valid against the ReqIF 1.2 schema. A ReqIF file from another
  tool imports from the project settings' ReqIF tab: its types, attributes,
  enumerations, hierarchy, relations and rich text come across, what matches
  nothing is made, and every choice and loss is shown before the worker
  writes anything. Identifiers are kept both ways, so a file that went out and
  comes back changes what it made as new revisions. See
  `docs/api-contract-backlog.md`.
- A Helm chart, `deploy/charts/exciter` (#104), with the values of Stator's:
  Postgres through CloudNativePG, one's own or bundled, Valkey, an S3 store
  with SeaweedFS for a trial, generated secrets, and the roles, migrate,
  bootstrap and seed Jobs. `armature.url` and `stator.url` connect the
  partners on the first start, and their backchannels are let through the
  outbound guard. `values-demo.yaml` installs everything in one command, and
  a release pushes the chart to `oci://ghcr.io/cloudster1/charts/exciter`.
  See `docs/deployment.md`.
- `seed bootstrap` connects Armature and Stator from
  `EXCITER_BOOTSTRAP_ARMATURE_URL` and `EXCITER_BOOTSTRAP_STATOR_URL` where
  nothing is connected yet (#104).
- One installation is one team's requirements (#84, #108). The first start
  makes it, called `EXCITER_NAME` (`Exciter` by default), and an
  administrator renames it under Settings, Installation. Everybody signed in
  works in it; keys and names, such as project keys, are unique in it.
- Releases are published from a tag (#101). `./release.sh` opens the pull
  request `release/vX.Y.Z`; merging it tags the merge commit `vX.Y.Z`, and
  the tag pushes the `backend`, `web` and `render` images to
  `ghcr.io/cloudster1/exciter-<image>` (as the version and the short commit),
  pushes the chart, publishes the GitHub release and tells Dynamo. See
  `docs/releasing.md`.
- The development realm is `dynamo`, the suite's (#102):
  `deploy/keycloak/realm.json` is the same file in Armature, Stator and
  Exciter, with each tool's client and the partners' groups.
- Exciter reads the webhook version header, `X-Armature-Webhook-Version` and
  `X-Stator-Webhook-Version`, before the body, and refuses a version it does
  not read with 422 `unknown_version`. The API document describes both
  webhook envelopes (#103).
- Connecting Armature asks its `GET /api/v1/meta` for `product: armature`, so
  it needs an Armature from 14 October 2026 on (#100).
- `GET /api/v1/meta` lists what Exciter offers partners:
  `requirement-embed@1`, `trace-links@1`, `specifications@1` and
  `theme-export@1`, described in `docs/integration.md` (#100).
- Exciter reads what the connected Armature offers from its `/meta` whenever
  the connection is saved or a token checked. When it lacks what a feature
  needs, the settings and account pages say which and to update Armature,
  issue reads answer `outdated`, linking and following its theme are refused
  with `armature_outdated`, and the link worker records why (#100).
- `GET /api/v1/meta` also answers `apiVersion` and `capabilities`, as
  Stator and Armature do, and names the build a checkout stamped: its version
  as `0.1.0`, `0.1.0+4.gabc1234` or `0.1.0+dev`, and its short commit (#99).
- Requirements linked to Stator pages (#41). A requirement's editors link
  it to the pages that specify, describe or relate to it, found by the first
  words of their titles; each page shows its title, space, path and version
  as the viewer may read it in Stator. A link turns suspect when Stator
  publishes a newer version of the page, counts in the requirement's
  suspect links, and is confirmed against the version read, with a comment.
  Stator's webhooks for published, moved and deleted pages keep the links
  current.
- Connecting Stator (#40). An administrator saves the address of the
  team's Stator under Settings, Stator, which Exciter checks answers as one,
  with the secret of Stator's webhook; each person pastes their own Stator
  token under Your account, Stator account, sealed at rest and used only for
  their own calls. Stator is reached through the same guard as Armature, and
  `EXCITER_STATOR_BACKCHANNEL` reaches it at another address inside a
  network. The stack runs a `stator-stub` beside the `armature-stub`.
- The API of the Stator integration, agreed before it is built (#39). The
  connection and each person's token, requirements linked to Stator pages,
  and Stator's webhooks are in `api/openapi.json` and answer 501 until each
  is built. `docs/api-contract-m4.md` says what each operation means, what is
  asked of Stator for links back from its pages, and divides the migration
  numbers among the issues. Stator's own API is vendored under
  `api/stator/` with `make stator-openapi REF=`.
- Coverage and the traceability matrix (#38). A project's administrators say
  which of decomposed, implemented and verified its requirements of each
  type are asked for. Coverage shows each dimension's share as a bar, and
  each requirement as full, partial, none or not asked, from its trace
  links, its Armature links and their status; a suspect link counts as
  partial. The matrix lays a page of requirements out against the
  requirements or the Armature issues they link to, and downloads whole as a
  CSV file.
- Baselines (#37). A project's administrators take a named baseline of the
  project, or of one section and everything below it: each requirement's
  revision, place and status, and its trace and Armature links, read as one
  snapshot however busy the project is. A baseline reads like the
  specification as it was, and compares with another baseline or with now,
  saying what was added, removed, revised, moved or moved to another status
  and which links came and went, each revised requirement opening its diff.
  A locked baseline keeps its name and is never deleted; nothing a baseline
  holds ever changes.
- Suspect links (#36). A link turns suspect when a requirement at an end its
  type watches gets a new revision, and an Armature link when its
  requirement does. The links panel, the specification, the grid and each
  project's Suspect links list mark them, and the grid keeps only those with
  one on request. Checking a link shows what changed since it was last
  confirmed; confirming it records who, against which revisions and why.
  The owner and watchers of the far end are told when a revision turns a
  link.
- Review and approval of revisions (#35). A project's administrators name
  who approves the requirements of each type and how many approvals it
  takes. An editor asks for the current revision to be approved; the review
  pins it and freezes its approvers, who approve or reject it from the
  requirement or from their inbox at `/reviews`. A new revision supersedes a
  pending review, and an approval counts only for the revision it named. The
  workflow condition "Approved" lets a transition through only for an
  approved current revision.
- Watching and notifications (#34). Anybody watches a requirement, or a
  whole project, and hears about new revisions, status changes and comments
  there; a mention, a reply in one's thread and a resolved thread are told
  too. Writing, revising or being made the owner of a requirement watches it,
  unless one unwatched it or switched that off. The bell in the top bar
  counts what is unread, `/notifications` lists everything, and each kind is
  switched in the app and by mail, sent at once or in an hourly or daily
  digest. The worker mails through `EXCITER_SMTP_ADDR` as
  `EXCITER_MAIL_FROM`; without an address it writes notifications alone.
- Comments and mentions (#33). Anybody who may comment on a requirement
  starts a thread on it, replies, and resolves or reopens a thread; each
  comment says which revision it was written on. Naming somebody with @
  mentions them, and an edit mentions only whom it adds. Only its author
  rewrites a comment; its author or the project's administrators delete it,
  leaving its place, and an administrator's deletion is audited. The
  specification shows each requirement's open threads.
- The API of review and control, agreed before it is built (#32). Comments
  and mentions, watching and notifications, review and approval of
  revisions, suspect links, baselines, and coverage with the traceability
  matrix are in `api/openapi.json` and answer 501 until each is built;
  `docs/api-contract-m3.md` says what each means.
- The suite (#31). `make suite-up ARMATURE_DIR= STATOR_DIR=` runs Exciter,
  Armature and Stator from their checkouts on one Keycloak realm, on fixed
  ports from 30000, connects Exciter and Stator to Armature with Armature's
  webhooks pointed at both, and writes the addresses and alice's Armature
  token to `.cache/suite.env`; `make suite-smoke` signs alice in to all three
  and watches a requirement's chip follow its issue. Nothing in Armature or
  Stator changes; `docs/suite.md` explains it.
- Following the Armature theme (#30). Whoever stored an Armature token can
  switch on "Follow my Armature theme" on the themes page: Exciter then
  shows the theme they use in Armature, kept as a private copy and fetched
  again when it changes there. A slow Armature, a refused token or a theme
  Exciter cannot use falls back to the theme they would have otherwise, and
  the page says why. Choosing a theme stops following.
- Armature's webhooks (#29). Armature tells Exciter at
  `/api/v1/armature/webhook` when a linked issue changes, signed with the
  secret stored under Settings, Armature: a requirement then shows the issue
  as it is now, its status for everyone, a move to another project, a
  deletion, and a link somebody took off the issue in Armature, which stays
  off. A replayed event changes nothing, and an envelope of a version
  Exciter does not read is refused so Armature keeps it.
- Armature issues made from requirements (#28). A project's editors pick
  requirements in the specification or the grid and create one Armature
  issue for each, in a project and type they may file in, summarized by the
  requirement's key and title and linking back to it. Each requirement is
  traced to its new issue at once, as implementing or verifying it; a round
  stops at the first issue Armature refuses and says why in its words.
- Requirements traced to Armature issues (#27). A requirement's editors trace
  it to the issues that implement, verify or relate to it, found by key or
  summary as they may see them in Armature; the requirement page shows each
  issue's summary, status and assignee live, and the status everyone may
  know without a token. The worker puts the requirement on each issue in
  Armature as the person who changed it, keeps it current through new
  revisions, keys, trash and purge, and says why when Armature refused it.
- Connecting Armature (#26). An administrator connects the installation's
  Armature under Settings, Armature, with the webhook's address, topics and
  secret; each person pastes their own Armature token under Your account,
  Armature account, sees whom it acts as, checks it and disconnects it, and
  is warned when it may only read. Tokens and the secret are sealed at rest,
  every call goes through a guard against reaching the server's own network,
  and a new address forgets every stored token. The stack runs an
  `armature-stub`, held to Armature's own document by a contract test.
- The API of the Armature integration, agreed before it is built (#25). The
  connection and each person's token, requirements traced to Armature issues,
  issues made from requirements, Armature's webhooks and the followed theme
  are in `api/openapi.json` and answer 501 until each is built.
  `docs/api-contract-m2.md` says what each operation means, how Exciter calls
  Armature as the viewer, and divides the migration numbers among the issues.
  Armature's own API is vendored under `api/armature/` with
  `make armature-openapi REF=`.
- Project templates (#24). A new project starts blank or from a System,
  Software or Test specification, which brings its requirement types, its
  attributes, the link types it uses and its starter sections; every
  template uses the default workflow.
- CSV import and export (#23). A project's administrators upload a CSV file
  under the project settings, see how each column will be read, and say
  what the words and people the project does not know mean before anything
  is written; the import then runs in the background with its progress
  shown, and refuses each row it cannot write with the line and why. A
  parent or outline column builds the tree and a links column holds
  `type:KEY`. The grid exports what it shows as CSV, and a project's export
  imports into an empty project as the same tree, content and links.
- Personal access tokens and MCP (#22). A person makes tokens under their
  account, read-only or not, limited to some projects or not, with an
  expiry, and sees each secret once; administrators see and revoke
  everybody's. A script sends one as a bearer and acts as its owner: a read-only token is refused every write, and a limited one
  reaches its projects alone, which the database holds too. `POST /mcp`
  serves assistants the tools to list projects, read, search, write, link
  and transition requirements and read their history, offering a token only
  what it may call. `GET /auth/me` names the calling token.
- Attachments (#21). A requirement takes files, picked or dropped on its
  panel, and its statement and rationale show pictures and files pasted,
  dropped or picked in the editor. Files go through the API alone, which
  shows pictures, PDFs and plain text in place and downloads the rest, and
  refuses one over `EXCITER_UPLOAD_LIMIT` (50 MB by default) with the limit.
  A file a revision shows is only taken off the list when deleted, so
  history still shows it; any other goes with its bytes, which the worker
  removes for purged requirements and deleted projects too. A copy takes
  the files and shows its own.
- Trace links (#18). A requirement links to another, in this project or any
  other the person may view, by the link types for all projects (derives,
  refines, satisfies, verifies, conflicts, relates) or a project's own. The
  links panel groups them by type and direction, finds the other end by key
  or title, and refuses a link to the requirement itself, a repeat, and a
  circle of a type that allows none, naming the requirements on the way
  round. A link records the revision of each end, is logged on both, and
  stays as a tombstone once removed. Administrators make and retire link
  types under Settings; a project turns one off or limits it to pairs of
  requirement types in its settings. A copy takes its links when asked.
- Search, the grid and saved views (#20). `/search` finds requirements across
  the projects a person may view by key, title, statement, rationale and
  text values, ignoring case and accents, with the matches marked in the
  title and a snippet, and narrows by project, status, label, owner and the
  day of the latest change. Ctrl K finds a requirement by the start of its
  key or title words. A project's grid narrows by words, kind, type, status,
  label, owner, priority and attribute conditions, orders by any column,
  shows the columns chosen and changes titles, priorities and values in
  place. A grid saves as a view, kept to its owner or shared with the
  project.
- Labels (#19). Any requirement or section carries
  labels shared by everyone, coined by typing one that nobody uses yet; the label box offers
  the labels already in use, the most used first, counting only what the
  reader may see. Adding or removing one is logged in the activity and never
  writes a revision. A label leads to the project's requirements with it,
  and on to those of every project. Copies take their original's labels, and
  the trash keeps them.
- Statuses and workflow (#17). Every project starts with a lifecycle: Draft,
  In review, Approved, Implemented and Verified, with Rejected and Obsolete
  beside them. A requirement moves only by the transitions its workflow
  offers the reader, and an approval cannot come from whoever wrote the
  latest revision. A new revision of an approved requirement sends it back to
  Draft, saying why. Project administrators define statuses, each counting as
  draft, review, approved, done or closed, and draw workflows in a graph
  designer, with conditions, validators and post-functions on each
  transition; the project follows one and a type may follow another, and a
  change of workflow asks where requirements go whose status it lacks.
- History, compare and restore (#16). A requirement's history lists its
  revisions with who wrote each, when and why. Any two compare side by side:
  changed words are marked in the statement and the rationale, and attribute
  values show before and after, options and people by name. Restoring an old
  revision writes it again as the next one, so nothing in between is lost.
  The activity merges revisions with owner, priority, place and trash changes.
- The tree and the specification (#15). Requirements and sections sit below
  one another, numbered as they read (2.3.1), and `/p/<key>/spec` reads a
  project as one numbered document beside its tree, each entry edited in
  place. Dragging in the tree moves a requirement and everything below it;
  the Move and Copy dialogs do it from the keyboard, also to another project,
  which gives new keys and keeps the old ones working. Types and values
  travel by name. The trash takes what is below a requirement along with it.
- Requirement types and attributes (#14). A project's administrators define
  its types, each with an icon and a colour, and the attributes it records:
  text, rich text, number, select, multi-select, date, person, yes or no and
  web address, each showing on some types or all of them and required or
  not. A requirement takes the project's default type unless its author picks
  another, and its values are part of what it says, so changing one writes a
  revision. What was ever used is retired rather than deleted, so history
  keeps it, and renaming an option renames it everywhere.
- Requirements (#13). An editor writes a requirement or a section with a
  title, a statement and a rationale in the shared editor, and gives it an
  owner and a priority. Its key is the project's key and its number, such as
  `CHK-R42`, and numbers are never handed out twice. A save that changes what
  it says writes the next revision, made over the revision it was opened at;
  a save over a revision somebody else saved first is refused and keeps the
  draft to save again over theirs. Owner and priority changes are logged
  without a revision. Deleted requirements wait in the project's trash until
  an editor restores them or an administrator deletes them for good. Old
  keys, after a project's key changes, still find their requirements, and
  `/r/<id>` is a permalink that never breaks. A project now opens on its
  requirements.
- Project roles (#12). A project's administrators give people, groups and
  everyone the role viewer, reviewer, editor or admin under
  `/p/<key>/permissions`, and roles add up: an editor through a group who is
  a reviewer on their own both edits and approves. A new project is its
  maker's to run and everyone's to edit. Guests are let in only by grants
  that name them or their groups, and never run a project. The access
  inspector shows, for any member, what they may do in the project and which
  grant decides it, as the database answers it. The installation's owners and
  administrators still hold every right in every project.
- Projects (#11). Owners and administrators make a
  project with a key such as `CHK`, which the form suggests from the name and
  the server keeps free. A project can be renamed, given a new key, archived
  and deleted once archived. A key it had keeps finding it, and stays taken.
  `/projects` lists them, `/p/<key>/settings` holds a project's details, key,
  archive and deletion, and the sidebar names the project the reader is in and
  switches to another.
- The API of the requirements core, agreed before it is built (#10). Projects,
  permissions, requirements and their revisions, types and attributes, the
  tree, history, workflow, trace links, labels, search, attachments, tokens,
  CSV import and templates are in `api/openapi.json` and answer 501 until each
  is built. `docs/api-contract-m1.md` says what each operation means and
  divides the migration numbers among the issues.
- A browser suite with an accessibility check on every page (#9).
  `make test-e2e` runs Playwright in its own container against the compose
  stack, and joins `./run-tests.sh` and CI, which splits it in two shards and
  merges their reports into one. axe checks every page of the shell, a new
  theme's editor, the not-found page and the sign-in page in light and dark,
  at desktop and phone widths. The specs sign in with a password and out
  again, sign in through the stack's Keycloak as alice@suite.test and land in
  the installation with the role her groups give, switch light and dark from
  the rail, import Armature's Minecraft theme, name the default theme for
  everyone and see it as another member, add, re-role and switch members off
  and on, and page through the audit log by mouse and keyboard. Each spec file
  keeps to people and project keys of its own, and the specs that change
  what everyone sees run one at a time (#83). `make test-e2e ONLY=<grep>` runs part of it. The link
  to the themes on the default theme page is underlined, which axe asked for.
- Themes (#8), in Armature's `armature-theme/1` format, so a theme exported
  from Armature or Stator imports unchanged and leaves the same way. The rail
  switches between Auto, Light and Dark; under the account menu, Themes lists
  a person's own themes and those shared with them, starts a new one blank or
  from Deep-Tech or Constellation, and edits its colours (the Clay tokens of
  Armature's `docs/design/01-tokens.md`), type, corners, shadows, cursors,
  icons, backdrop, moving picture and extra CSS with a live preview. A theme's
  pictures and fonts are kept in the object store, which the api now reaches
  through `EXCITER_S3_ENDPOINT` and its keys, making its bucket when missing.
  Administrators name the shared theme everybody sees until they choose under
  Settings, Default theme, recorded in the audit log. The database keeps a
  private theme its owner's, lets only the owner or, once shared, an
  administrator change one, and keeps each choice to the person who made it.
- Exciter has a logo and a favicon (#56). The sign-in page shows the logo, the
  sidebar shows the gopher's face beside the installation's name, and the
  browser tab, touch icon and web app manifest use the same face, which still
  reads at 16 pixels. `make branding` makes them from
  `assets/branding/logo.jpg`.
- Single sign-on through the installation's identity provider (#7). The
  sign-in page sends the reader to the provider (authorization code with
  PKCE); a person is matched by the
  provider's subject, so they are the same user in Exciter, Armature and
  Stator, and their provider groups are synced at every sign-in.
  Administrators set the provider up under Settings, Single sign-on, map
  provider groups to the admin or member role, which members follow at their
  next sign-in, and let in or turn away people the provider vouched for who
  are not members yet; every change is in the audit log.
  `make up` signs the demo installation in through the stack's Keycloak as
  alice@suite.test (an administrator) or bob@suite.test, and behind nginx
  sessions and the audit log record the client's address rather than the
  proxy's (`EXCITER_TRUSTED_PROXIES`).
- The web client's skeleton (#6). nginx serves the React app on the web port
  and passes `/api` to the api, so `make up` now opens Exciter at
  `http://localhost:<web port>`. A person signs in with a password, lands on a
  home page that says what Exciter is for, and finds their way by a rail, a
  sidebar that folds or becomes a drawer on a phone, breadcrumbs, and Ctrl K,
  which goes to any page from the keyboard. The account menu switches between
  English and German and signs out. Administrators add members (a made
  password is shown once), change their roles, switch them off after a
  confirmation, and read, filter and export the audit log. A
  session that ends sends the reader to sign in and back. `make check-web`
  and `make check-format` join the gate.
- Members and password sessions (#5). `POST /auth/login` signs a local
  account in with an argon2id password and sets an http-only session cookie;
  `/auth/me` and `/auth/logout` follow it. An administrator lists, adds,
  re-roles (admin, member, guest) and switches off members, each act in the audit log, which administrators now read, filter
  and export through `/audit`. `seed bootstrap` makes the installation and its
  owner; `make up` signs in as `admin@exciter.test`.
- The outbox, the worker and the audit log (#4). A change and the event it
  causes commit together or not at all; the worker drains the outbox, any
  number of copies at once, retries a failing event a little later each time
  and after ten attempts sets it aside with an entry in the
  audit log. The log is only ever added to, read only by owners and
  administrators, and pruned after `EXCITER_RETAIN_AUDIT` (a year by default,
  never less than a day). Its endpoints are agreed and served with #5.
- The backend skeleton (#3). The api serves `/healthz`, `/readyz`,
  `/api/v1/openapi.json` and `/api/v1/meta` from one route table that also
  writes `api/openapi.json`; reads go to the replica unless it has not
  replayed the caller's own last write. `make up` now migrates, seeds the demo
  installation and starts the api. Every table forces row level security, and
  the integration suite proves through raw SQL as `exciter_app` that the
  permission policies hold.
- A development stack (#2). `make up` starts Postgres with a streaming
  replica, Valkey, SeaweedFS, Mailpit and Keycloak on twenty ports of the
  checkout's own between 10000 and 19999, so worktrees and a Stator checkout
  run beside it; `make ports` says which. Keycloak's `dynamo` realm has a
  client for Exciter, Armature and Stator, so a person is one subject in all
  three.
- Repository tooling (#1). `./run-tests.sh` is the one gate for every layer,
  run before each push by the branch-guard hook and in CI as a matrix of jobs;
  `release.sh` cuts a version from the commit history; issue and pull request
  templates and the GitHub workflow live in `.claude/skills/gh`.

### Fixed

- Exciter puts its links on Stator pages only while Stator lists
  `page-remote-links@1` by that exact name (#275), so a later
  `page-remote-links@2` that promises something else is not taken for it.
- An installation on CloudNativePG can be administered (#271). The chart
  gives the database owner BYPASSRLS, which the permission checks it owns
  need to see anybody; without it the bootstrap stopped at the identity
  provider and no administrator could change anything walled. The
  migrations now refuse an owner that is neither a superuser nor BYPASSRLS
  and say what to grant.
- A project made from the Test specification template takes test cases
  straight away (#263): its "Test case" type is a type of test cases, and
  the template no longer adds Preconditions and Expected result attributes,
  which a test case now holds itself.
