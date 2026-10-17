# Security

## Reporting a vulnerability

Report it privately through
[GitHub's private vulnerability reporting](https://github.com/Cloudster1/Exciter/security/advisories/new),
never in a public issue or pull request. Say what is affected, how to
reproduce it, and what an attacker gains. You hear back within a week, and
are told when a fix is released and whether you want to be credited.

A problem in Armature or Stator belongs to
[Armature](https://github.com/Cloudster1/Armature/security) or
[Stator](https://github.com/praetorianer777/Stator/security); report it there.

## Supported versions

Fixes go into the latest release. Exciter is before 1.0.0, so upgrade to the
newest release to receive them.

## Development credentials

The Keycloak realm in `deploy/keycloak/realm.json`, the passwords
`<name> password`, the client secrets `<client>-dev-secret` and the defaults in
`deploy/docker-compose.yml` are for development only and published on
purpose. They are not a vulnerability; an installation never uses them, as
[`docs/deployment.md`](docs/deployment.md) describes.
