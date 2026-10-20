# Exciter, Armature and Stator together

`make suite-up` runs the three products side by side on one Keycloak, connected to each other, for trying the
integration end to end (#31). It is for development on one machine: every port is bound to `127.0.0.1`, and every
password is a development one.

```sh
make suite-up ARMATURE_DIR=~/repos/Armature STATOR_DIR=~/repos/Stator
make suite-smoke                      # optional: sign in to all three and watch a chip follow Armature
make suite-down ARMATURE_DIR=... STATOR_DIR=...    # stop, keeping the data
make suite-clean ARMATURE_DIR=... STATOR_DIR=...   # stop and forget everything
```

`ARMATURE_DIR` and `STATOR_DIR` are checkouts of `Cloudster1/Armature` and `praetorianer777/Stator`. Each product is
built from its own checkout with its own `deploy/docker-compose.yml`, and an override from `deploy/suite/` in this
repository changes what the suite needs. Nothing in Armature or Stator changes.

## Where everything is

| What | Address | Project |
|---|---|---|
| Keycloak, realm `dynamo` | http://localhost:30000 (admin / admin) | `suite-exciter` |
| Exciter | http://localhost:30010 | `suite-exciter` |
| Armature | http://localhost:30020, its api on 30021 | `suite-armature` |
| Stator | http://localhost:30040 | `suite-stator` |

Each product keeps its own order of ports from its first one: Exciter's api is 30011 and its Postgres 30014,
Armature's Postgres 30024, Stator's 30044. The ports from 30000 stay clear of the stacks the checkouts run on their
own, Exciter's from 10000 and Stator's from 20000, and of the kernel's ephemeral ports from 32768. The compose
projects are named `suite-*`, so a product's own stack, which another session may be running, is left alone.

## Who signs in

The realm is the suite's, `deploy/keycloak/realm.json`, the same file in all three repositories, with a client for
each product. Exciter's and Stator's clients
require PKCE; Armature's does not, as Armature's sign-in sends no PKCE challenge yet.

| Person | Password | Exciter | Armature | Stator |
|---|---|---|---|---|
| alice@suite.test | alice password | administrator | administrator | administrator |
| bob@suite.test | bob password | member | member | member |
| carol, dave | carol password, dave password | asks to join | asks to join | asks to join |

Each product also keeps its own administrator with a password: `admin@exciter.test` / `exciter admin password`,
`ada@armature.test` / `demo password please` (Armature's seed, with its demo projects) and `admin@stator.test` /
`stator admin password`. Signing in to one product signs alice in to the others without the form again: that is the
single sign-on.

## How the three meet

- **One network.** `exciter-suite`, made by `suite-up`, joins Exciter's Keycloak and each product's api and worker,
  reached by the names `suite-keycloak`, `suite-exciter-api`, `suite-armature-api` and `suite-stator-api`. No web
  joins it: compose names every service by its own name on each network it joins, so a web proxying to `api` would
  reach any product's.
- **One identity provider.** Exciter's Keycloak answers on 30000. Armature and Stator leave their own Keycloak out,
  name the suite's issuer, and reach it inside the network through their `*_OIDC_BACKCHANNEL` settings, as Exciter
  does.
- **Armature, as its seed made it.** The override leaves out Git hosting, PDF export and mail over TLS. `suite-seed`
  then points Armature's identity provider at the suite's realm and makes alice and bob members with the realm's
  passwords, so the realm signs them in.
- **Connections to Armature.** Exciter and Stator connect to `http://localhost:30020`, the address people open, and
  reach it inside the network through `EXCITER_ARMATURE_BACKCHANNEL` and `STATOR_ARMATURE_BACKCHANNEL`, which the
  overrides add beside each product's stub.
- **Armature's webhooks.** Armature has an endpoint for each product, posting to its api inside the network
  (`http://suite-exciter-api:8080/api/v1/armature/webhook`); each product stores the endpoint's secret. Exciter
  checks the signature, not the address it was posted to.
- **Alice's Armature token.** `suite-seed` makes one and writes it to `.cache/suite.env`, with the addresses. Paste
  it under Your account, Armature account in Stator to see issues as alice there.
- **Connected at sign-in (#274).** `suite-seed` registers Exciter as a connected app of Armature, named by the
  realm's `exciter` client, stores its client id and secret on Exciter's Armature connection, and connects Exciter
  to Stator, the same way once Stator lists `oauth.token-exchange@1`. Signing in to Exciter then connects alice to
  Armature without a token; Stator follows when its exchange ships (praetorianer777/Stator#461).

`make suite-seed` runs these steps again on a running suite; each finds what is already there and leaves it.

## Memory

The three stacks start 28 containers, which took about 2.2 GB once settled when this was written. Stop the
products' own stacks first if memory is short, and the suite with `make suite-down` when done; `docker stats` shows
what each takes.

## When Armature or Stator change

The overrides name services and settings of the other products' compose files. When one of those changes, the
suite stops at the step that needs it, and the override follows. A change the suite needs in Armature or Stator
itself goes through an issue in their repository.
