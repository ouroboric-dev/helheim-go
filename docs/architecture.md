---
title: helheim-go architecture
status: draft
date: 2026-10-17
related: [https://github.com/ouroboric-dev/yggdrasil/blob/main/docs/2-engineering/specs/2026-10-11-yggdrasil-v0-spec.md]
style: reference
---

# helheim-go architecture

The Helheim realm's Go flavor: prompts, devcontainer stack layer and services, templates and checks
for backends.

Helheim is the realm of backends, and this is its Go flavor (realm leaf naming, Yggdrasil change
proposal `2026-10-17-realm-leaf-naming`).

## Responsibilities

- Holds the recipe of a Go backend leaf (spec §9, as changed by realm leaf naming): the `implementer`
  prompt, the Go devcontainer stack layer with the services an API needs (Postgres, SeaweedFS), the
  api leaf template and its checks, under the role names every build realm shares.
- Holds no graph in v0. A run takes its graph from its lead realm (`midgard-react` for the client
  gallery); a backend-only product would add one here later.
- Its templates are copied into products, so they are MIT-licensed (`templates/`).

## Depends on

- Gungnir's schemas.

## Source

The v0 engineering spec, §2.1, §9, and the change proposal realm leaf naming. When this page and the
spec differ, the spec wins.
