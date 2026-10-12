# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- The backend skeleton (#3). The api serves `/healthz`, `/readyz`,
  `/api/v1/openapi.json` and `/api/v1/meta` from one route table that also
  writes `api/openapi.json`; reads go to the replica unless it has not
  replayed the caller's own last write. `make up` now migrates, seeds the demo
  organization and starts the api. Every table forces row level security, and
  the integration suite proves through raw SQL as `exciter_app` that one
  organization cannot read another's rows.
- A development stack (#2). `make up` starts Postgres with a streaming
  replica, Valkey, SeaweedFS, Mailpit and Keycloak on twenty ports of the
  checkout's own between 10000 and 19999, so worktrees and a Stator checkout
  run beside it; `make ports` says which. Keycloak's `suite-dev` realm has a
  client for Exciter, Armature and Stator, so a person is one subject in all
  three.
- Repository tooling (#1). `./run-tests.sh` is the one gate for every layer,
  run before each push by the branch-guard hook and in CI as a matrix of jobs;
  `release.sh` cuts a version from the commit history; issue and pull request
  templates and the GitHub workflow live in `.claude/skills/gh`.
