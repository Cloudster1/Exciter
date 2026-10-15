# Exciter

**Requirements management**, self-hosted, beside
[Armature](https://github.com/Cloudster1/Armature) and
[Stator](https://github.com/praetorianer777/Stator). Write requirements as a
numbered specification, keep every revision, trace them to each other, to the
Armature issues that implement and verify them and to the Stator pages that
explain them, review and approve them, and freeze what was agreed in
baselines. Same look, same themes, same single sign-on as its siblings.

An installation serves one team: everybody signed in works in it, and
settings such as the theme, the link types and the Armature connection are
for everyone. People who must not see each other's work run an installation
each.

Exciter is at the start: the plan is in the
[issues](https://github.com/Cloudster1/Exciter/issues) and their milestones,
from `M0 Foundation` to `M4 Stator integration`. What exists is described in
[`docs/architecture.md`](docs/architecture.md) and argued in
[`docs/decisions.md`](docs/decisions.md).

## Working on it

Every toolchain runs in a container, driven by the `Makefile`; Docker and
make are the only requirements. [`CLAUDE.md`](CLAUDE.md) holds the rules.

```sh
make up                 # the stack: Postgres with a replica, Valkey, SeaweedFS, Mailpit, Keycloak, the api, the web client
make ports              # this checkout's compose project and its twenty ports
make down               # stop (make clean also drops the volumes)
make web-dev            # the web client with hot reload, against the running stack's api
./run-tests.sh          # the whole gate, as CI and the push hook run it
./run-tests.sh --list   # its layers
make test-e2e ONLY=themes   # some of the browser suite against the running stack (make stack-up first)
make e2e-report         # the last browser run's report, traces included
make help               # every target
```

The stack's installation signs in through its Keycloak: at the web address
`make up` prints, choose "Continue with single sign-on" and sign in as
`alice` (alice@suite.test, an administrator) with the password
`alice password`, or as `bob`, a member. Its owner `admin@exciter.test` signs
in with the password `exciter admin password`. `EXCITER_NAME` sets what a
first start calls the installation.

Every port is bound to `127.0.0.1` and derived from the checkout's path, so
worktrees, Armature and a Stator checkout run side by side; the ports are also
written to `.cache/stack.env`. Keycloak's realm `suite-dev` (admin console
`admin` / `admin`) holds alice, bob, carol and dave, each with the password
`<name> password`, and a client for each of Exciter, Armature and Stator.

To try the integration end to end, `make suite-up ARMATURE_DIR=<checkout>
STATOR_DIR=<checkout>` runs Exciter, Armature and Stator side by side on that
one realm, connected to each other; [docs/suite.md](docs/suite.md) says where
everything is.

## Installing

The Helm chart `deploy/charts/exciter` installs Exciter on Kubernetes, alone
or in the suite beside Armature and Stator; `values-demo.yaml` brings
everything up in one command. See [docs/deployment.md](docs/deployment.md).

## Releasing

`./release.sh` works out the next version from the Conventional Commits since
the last tag and opens the pull request `release/vX.Y.Z` that bumps every file
carrying the version and the changelog; `./release.sh --dry-run` shows what it
would do. Merging it tags the merge commit `vX.Y.Z`, and the tag publishes the
images to `ghcr.io/cloudster1/exciter-<image>` and the GitHub release. See
[docs/releasing.md](docs/releasing.md).

## License

Apache-2.0, see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

The logo and the icons show the Go gopher, designed by
[Renee French](https://reneefrench.blogspot.com/) and licensed under
[Creative Commons Attribution 4.0](https://creativecommons.org/licenses/by/4.0/);
see [`NOTICE`](NOTICE).
