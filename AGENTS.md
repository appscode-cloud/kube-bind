# AGENTS.md

This file provides guidance to coding agents (e.g. Claude Code, claude.ai/code) when working with code in this repository.

## Repository purpose

Go module `go.bytebuilders.dev/kube-bind` — the AppsCode fork of [kube-bind/kube-bind](https://github.com/kube-bind/kube-bind), adapted to AppsCode's Kubernetes-native services. kube-bind lets a service provider expose Kubernetes APIs that a consumer cluster can "bind" into — service-CRDs surface in the consumer cluster while the controllers run in the provider cluster.

Original project by Dr. Stefan Schimanski (`sttts`). This fork tracks upstream and carries AppsCode patches.

Three binaries:
- `example-backend` — reference provider backend.
- `konnector` — the per-consumer-cluster agent that drives the bind.
- `kubectl-connect` — `kubectl` plugin that initiates a bind from the consumer side.

## Architecture

- `cmd/`:
  - `example-backend/` — reference provider implementation.
  - `konnector/` — agent that runs on the consumer cluster.
  - `kubectl-connect/` — kubectl plugin for end users.
- `apis/kubebind/` — Kubernetes API types (`APIServiceBinding`, etc.).
- `client/` — generated typed clientset.
- `crds/` — generated CRD YAMLs.
- `pkg/`:
  - `bootstrap/` — provider/consumer bootstrap helpers.
  - `committer/` — status committer used by the konnector.
  - `indexers/` — shared informer indexes.
  - `konnector/` — konnector implementation.
  - `kubectl/` — kubectl-plugin helpers.
  - `template/` — manifest templates.
  - `version/` — version helpers.
- `contrib/` — community helpers / examples.
- `guides/`, `docs/` — usage and architecture docs.
- `Dockerfile.in` (PROD, distroless), `Dockerfile.dbg` (debian), `Dockerfile.ubi` (Red Hat certified).
- `hack/`, `Makefile` — AppsCode build harness.
- `vendor/` — checked-in deps.

## Common commands

- `make ci` — full CI pipeline.
- `make build` / `make all-build` — host or all-platform build.
- `make gen` — regenerate clientset + manifests after API type changes.
- `make fmt`, `make lint`, `make unit-tests` / `make test` — standard.
- `make verify` — codegen + module-tidy verification.
- `make container` / `make push` / `make release` — image build/publish flow.

## Conventions

- Module path is `go.bytebuilders.dev/kube-bind` (vanity URL, **AppsCode-forked**). Imports must use that.
- Upstream is `github.com/kube-bind/kube-bind` — prefer rebasing onto upstream over diverging where possible; isolate AppsCode-only patches so they replay cleanly.
- License: see `LICENSE.md`. Sign off commits (`git commit -s`); contributions follow the DCO (`DCO`).
- Three binaries; keep `cmd/<name>/` self-contained per binary.
- Three Dockerfiles, one binary each — keep `Dockerfile.in`, `Dockerfile.dbg`, and `Dockerfile.ubi` in sync.
- Do not hand-edit `zz_generated.*.go`, generated client/`, or `crds/*.yaml` — change `apis/kubebind/*` and re-run `make gen`.
