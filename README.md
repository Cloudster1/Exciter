# Exciter

**Requirements management**, self-hosted, beside
[Armature](https://github.com/Cloudster1/Armature) and
[Stator](https://github.com/praetorianer777/Stator). Write requirements as a
numbered specification, keep every revision, trace them to each other, to the
Armature issues that implement and verify them and to the Stator pages that
explain them, review and approve them, and freeze what was agreed in
baselines. Same look, same themes, same single sign-on as its siblings.

Exciter is at the start: the plan is in the
[issues](https://github.com/Cloudster1/Exciter/issues) and their milestones,
from `M0 Foundation` to `M4 Stator integration`. What exists is described in
[`docs/architecture.md`](docs/architecture.md) and argued in
[`docs/decisions.md`](docs/decisions.md).

## Working on it

Every toolchain runs in a container, driven by the `Makefile`; Docker and
make are the only requirements. [`CLAUDE.md`](CLAUDE.md) holds the rules.

```sh
make up                 # the stack: Postgres primary and replica, Valkey, SeaweedFS, Mailpit, Keycloak
make ports              # this checkout's compose project and its twenty ports
make down               # stop (make clean also drops the volumes)
./run-tests.sh          # the whole gate, as CI and the push hook run it
./run-tests.sh --list   # its layers
make help               # every target
```

Every port is bound to `127.0.0.1` and derived from the checkout's path, so
worktrees, Armature and a Stator checkout run side by side; the ports are also
written to `.cache/stack.env`. Keycloak's realm `suite-dev` (admin console
`admin` / `admin`) holds alice, bob, carol and dave, each with the password
`<name> password`, and a client for each of Exciter, Armature and Stator.

## Releasing

`./release.sh` works out the next version from the Conventional Commits since
the last tag, writes `VERSION` and `CHANGELOG.md`, commits and tags.
`./release.sh --dry-run` shows what it would do. Pushing the tag publishes the
GitHub release.

## License

Apache-2.0, see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
