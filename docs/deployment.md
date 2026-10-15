# Deployment

Exciter runs from Docker Compose for development (`make stack-up`) and from
the Helm chart in `deploy/charts/exciter` anywhere else. The chart is Stator's,
so the three tools of the suite install alike and Dynamo's umbrella chart can
set the same values on each. `values.yaml` describes every setting; this page
says how they fit together.

## A look at it

```sh
helm install demo deploy/charts/exciter -f deploy/charts/exciter/values-demo.yaml
```

The demo brings its own Postgres, Valkey and SeaweedFS, makes the installation
and an administrator, and prints in its notes how to read the administrator's
password. It keeps no backups and serves plain HTTP: it is for a look, not for
work.

## A real installation

Three things come from outside the chart:

- **Postgres.** Either a CloudNativePG cluster the chart renders
  (`cnpg.enabled`, with the operator already installed), whose `-rw` service
  takes writes and whose `-ro` service takes reads, or a database of your own
  at `database.host`, with read replicas at `database.replicaHosts`. The roles
  Job makes `exciter_app` and `exciter_admin`, the migrate Job runs the
  migrations as the owner before every upgrade.
- **Valkey**, at `valkey.host`. More than one api pod with read replicas needs
  it, and the chart refuses that layout without it.
- **An S3 bucket**, under `s3`, for attachments and theme files. Without one
  an upload is refused, saying so.

Passwords and the key that seals stored secrets are made once by the secrets
Job, in Secrets that outlive the release. `jobs.bootstrap.enabled` makes the
installation (`installation.name`), its first administrator and, when named,
the identity provider and members.

## In the suite

Dynamo installs Exciter beside Armature and Stator and signs everyone in
through one Keycloak realm. The values that wire Exciter in:

| Value | What it does |
| --- | --- |
| `bootstrap.oidc.issuer`, `clientId`, `clientSecret` | Sign-in through the realm, such as `https://sso.example.com/realms/dynamo`; the client secret is read from a Secret. |
| `auth.oidcBackchannel` | Where this release reaches Keycloak when that is not the address the browser uses. |
| `armature.url`, `stator.url` | The partners as people open them. The bootstrap Job connects each where nothing is connected yet, before it answers too, and leaves a connection an administrator changed alone. |
| `armature.backchannel`, `stator.backchannel` | Where this release reaches each partner inside the cluster. |

Every host a backchannel reaches is let through the outbound guard on its own;
`network.outboundAllow` names any other. A partner's webhook secret is shown
by that partner when its webhook is added, so it is pasted under Settings,
Armature and Settings, Stator, once both run.

## Tests

`tests/test-helm.sh`, in the shell layer of `./run-tests.sh`, lints and
renders the chart for each database layout, checks the partners and the
bootstrap, refuses what the chart must refuse, and compares two renders with
the golden files in `tests/helm`. After a change to the chart, review the
difference it prints and rewrite them with `UPDATE=1 tests/test-helm.sh`.

`make test-chart-install` installs the chart into a throwaway kind cluster
with the images built from the checkout, signs the administrator in, checks
the partners are connected, and upgrades it with a second api pod while
watching the health check. CI runs it (`chart-install.yml`) when anything the
chart installs changes.
