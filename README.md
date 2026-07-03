# devedge-terraform-sdk

The open-core, mechanism-only **Terraform SDK** for devedge — the
**Terraform mirror of
[`devedge-cli-sdk`](https://github.com/infobloxopen/devedge-cli-sdk)** (and of
[`devedge-sdk`](https://github.com/infobloxopen/devedge-sdk) /
[`devedge-ufe-sdk`](https://github.com/infobloxopen/devedge-ufe-sdk)). It is
small, public, and carries **no proprietary dependencies**.

It provides two things:

- **`tfkit`** — the runtime library a generated Terraform provider imports.
- **`tfgen`** — a generator that turns an **enriched OpenAPI v3** spec into a
  [`terraform-plugin-framework`](https://github.com/hashicorp/terraform-plugin-framework)
  provider with correct schema semantics and CRUD glue.

The generated provider imports `tfkit` exactly as an apx-generated Go client
imports `oapi-codegen/runtime`, an Angular client imports `@angular`, and a
generated CLI imports `clikit`. The runtime is the stable seam; the generated
code is disposable.

## The seam is public; the proprietary implementation binds on top privately

This repo follows the same governance principle as `devedge-sdk`: **the seam is
public; any product-specific implementation is a private binding.** In
`devedge-sdk` the authorization *seam* is the public `authz.Authorizer`
interface; a concrete decision point — say, an OPA-backed authorizer — binds to
it from a separate private package, and nothing about that engine leaks into the
public seam.

The Terraform SDK works the same way. Everything here is **mechanism, not
policy**:

- The **contract seam** is the enriched OpenAPI spec `tfgen` consumes. The set
  of resources, fields, and behaviors is the service's own contract, not baked
  in here.
- The **auth seam** is the provider's `endpoint`/`token` configuration and the
  `tfkit.Client` bearer transport. A provider-specific auth binding (Okta, PDS,
  a device-grant helper) is a separate **private** overlay that supplies a
  token; no identity provider is named or hardwired here.
- The **provider composition seam** is `tfkit.NewProvider` + a generated
  `Resources()` registration. The set of resources a provider serves is
  generated from the contract, never hardcoded in the runtime.

Nothing product-specific lives in this repository — no identity-provider names,
no service catalog, no auth presets, no scaffolding of a provider *repo* (that
belongs to `de terraform new`). Those bind on top, privately, the same way a
private authorizer binds to `authz.Authorizer` in `devedge-sdk`.

## Why this exists — and the one fact that shapes it

A service in the devedge ecosystem already publishes an **enriched OpenAPI
contract** (WS-024 P0a): native `required`/`readOnly`/`writeOnly`/`enum` plus
`x-aip-*` extensions carrying resource identity, methods, pagination,
references, and field behavior. That contract is enough to project an
out-of-the-box Terraform provider.

The load-bearing fact:

> HashiCorp's `tfplugingen-openapi` **ignores** `field_behavior` and the `x-*`
> extensions. It infers `required`/`computed` only from request-vs-response body
> membership — so it gets the semantics wrong.

So `tfgen` does **not** use it. Instead it **emits the HashiCorp Provider Code
Specification JSON directly**, using the
[`terraform-plugin-codegen-spec`](https://github.com/hashicorp/terraform-plugin-codegen-spec)
Go bindings, setting the enriched semantics itself. It then runs the pinned
[`tfplugingen-framework`](https://github.com/hashicorp/terraform-plugin-codegen-framework)
on that JSON to emit schema+models, and finally templates the CRUD glue.

## How field behavior maps to Terraform schema

| Contract signal | Terraform schema |
|---|---|
| native `required` / `field_behavior: REQUIRED` | `Required` attribute |
| `readOnly` / `OUTPUT_ONLY` | `Computed` attribute (+ `UseStateForUnknown` plan modifier) |
| `IMMUTABLE` | disposition kept, plus a `RequiresReplace()` plan modifier |
| `writeOnly` / secret / `INPUT_ONLY` | `Sensitive` attribute; excluded from response-apply so the plan value is preserved |
| `enum` / `allowed_values` | `stringvalidator.OneOf(...)` validator |
| storage `not_null` | **not** mapped to required (a NOT NULL column is not a client-required field) |

The mapping lives in the framework-free `tfkit/behavior` package, so the
generator can depend on it without pulling the Terraform runtime into its
dependency graph.

## Packages

| Import | Purpose | Runtime deps |
|---|---|---|
| `tfkit` | Provider runtime: authed `Client` (bearer + JSON CRUD helpers), a reusable `provider.Provider` base (`NewProvider`), model↔wire mapping helpers, `ImportStatePassthroughID`, and an LRO poll. | `terraform-plugin-framework` |
| `tfkit/behavior` | Framework-free `field_behavior`→schema semantics (the single source of truth). | none |
| `cmd/tfgen` | Generator CLI: enriched OpenAPI v3 → provider code spec → schema+models → CRUD glue. | `getkin/kin-openapi`, `terraform-plugin-codegen-spec` |
| `cmd/tfkit-doctor` | Diagnostics binary (Go toolchain, tfkit version, pinned generator). | (tfkit) |

The generation logic lives in `internal/gen` (unit-testable; its spec-emit path
is free of the Terraform runtime); it is not a public import surface.

> **Dependency-graph note.** `terraform-plugin-framework` (and the provider
> serving stack) is heavy. That weight is intentional and **isolated to this
> module** — a service repo does not import it. Keep it that way.

## Getting started

The fastest path to a working provider is to scaffold one with the devedge CLI:

```bash
de terraform new --provider toy --spec ./openapi/toy.openapi.yaml
de terraform add --resource widget --spec ./openapi/toy.openapi.yaml
```

`de terraform add` runs `tfgen` against a service's enriched OpenAPI spec and
wires the generated resources into a provider that composes `tfkit`. It ships in
the [`devedge`](https://github.com/infobloxopen/devedge) CLI, the same tool that
scaffolds backend services (`de new service`), micro-frontends (`de ufe new`),
and CLIs (`de cli add`).

To wire a provider by hand instead, follow the quickstart below.

## Quickstart

### Generate the provider internals

```bash
go run github.com/infobloxopen/devedge-terraform-sdk/cmd/tfgen \
  --input ./openapi/toy.openapi.yaml \
  --output ./internal/provider \
  --module github.com/acme/terraform-provider-toy \
  --provider toy
```

This emits, into `./internal/provider`:

- `provider_code_spec.json` — the enriched Provider Code Specification.
- `<resource>_resource_gen.go` — schema + model (from `tfplugingen-framework`).
- `<resource>_resource.go` — the CRUD glue (a `resource.Resource` impl).
- `resources_gen.go` — the `Resources()` registration.

### Compose the provider (the small hand-written seam)

```go
package provider

import (
    "github.com/hashicorp/terraform-plugin-framework/provider"
    "github.com/infobloxopen/devedge-terraform-sdk/tfkit"
)

func New(version string) func() provider.Provider {
    return func() provider.Provider {
        return tfkit.NewProvider(tfkit.ProviderConfig{
            TypeName:  "toy",
            Version:   version,
            Resources: Resources(), // generated
        })
    }
}
```

```console
$ TOY_ENDPOINT=https://api.example.com TOY_TOKEN=… terraform plan
```

See [`examples/terraform-provider-toy`](examples/terraform-provider-toy) for a
complete, buildable provider with committed generated output, a schema-assertion
test, and a CRUD test that drives `Create`/`Read` against a fake service through
`tfkit`.

## Development

```sh
go build ./...
go vet ./...
go test ./...
gofmt -l .
```

Go 1.23+. `terraform-plugin-framework` is a deliberately heavy but isolated
dependency; the generator's spec-emit path stays free of it.

## License

[Apache-2.0](./LICENSE).
