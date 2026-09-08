---
name: review-policy-protobuf
description: >-
  Reviews policy protobuf definitions under proto/policy/ in
  GoogleCloudPlatform/opentelemetry-operations-collector. Focuses on core
  control-plane design principles: declarative-friendly modeling, matcher orthogonality,
  normative evaluation semantics, invalid state unrepresentability, and package hygiene
  not covered by standard linters. Use when reviewing, authoring, or updating policy protobufs.
---

# Policy Protobuf Review Guide

This skill guides the design and review of policy Protocol Buffers in `GoogleCloudPlatform/opentelemetry-operations-collector` (`proto/policy/`).

## Scope & Philosophy

- **Do NOT duplicate CI checks**: Linters (`buf lint`, `api-linter`, `make test-protos`) enforce syntax, formatting, and naming conventions. Focus strictly on architectural and semantic design aspects linters cannot evaluate.
- **Do NOT suppress linter warnings**: Never use inline suppressions (e.g. `-- api-linter: ...=disabled --`). Linter failures usually point to schema design anti-patterns (such as naked booleans in selectors); fix the underlying design instead.
- **Follow existing patterns**: Reuse conventions and types already established in `proto/policy/v1alpha1/` (e.g. `common_policy_types.proto`) rather than inventing novel mechanisms for matchers, string representations, or value coercion.
- **Broad Applicability**: Applies to existing filter policies and future policy types (e.g. transformations, routing, sampling).
- **Standards**: References [Google AIP-122](https://google.aip.dev/122), [Google AIP-128](https://google.aip.dev/128), and [Protobuf Best Practices](https://protobuf.dev/programming-guides/dos-donts/).

---

## Core Review Principles

### 1. Declarative & Safe-by-Default Modeling
- **Model Intent, Not Imperative Verbs**: Policies represent desired state, not point-in-time actions.
- **Preserve Optionality (AIP-128)**: Use `optional` on fields where an unconfigured state has distinct meaning from the default/zero value (e.g. `optional Action action = 2;`). This prevents spurious diffs in declarative tooling (KRM, Terraform, GitOps).
- **Guard Against Vacuous Matching**: An empty condition list (`matches: []`) must never match everything by default when paired with destructive actions (like drop). Require $\ge 1$ matcher, and ensure invalid or unconfigured policies fail open.

### 2. Matcher Architecture & Orthogonality
- **Separate Target from Predicate**: The target selector identifies *what* to inspect; the predicate defines *how* to evaluate it. Never embed expected values or enum variants inside the target selector.
- **Universal Over Dedicated Predicates**: Evaluate typed targets against universal predicates (`exact`, `regex`) using canonical uppercase string representations rather than adding one-off enum variants to the predicate `oneof`. This avoids predicate bloat, prevents target/predicate mismatches, and allows multi-value matching (`regex: "A|B"`).

### 3. Schema Hygiene & Unrepresentable Invalid States
- **Use `google.protobuf.Empty` for Presence**: In a `oneof`, use `google.protobuf.Empty` for existence checks instead of `bool exists`, and pair with an orthogonal `bool negate`. This prevents ambiguous boolean matrices (`exists: false, negate: true`).
- **No Naked Booleans in Selectors**: Group field targets into strongly typed enums rather than using `bool` variants in a selector `oneof`.
- **Enum Discipline**: Every enum must define tag `0` as `{NAME}_UNSPECIFIED`. Never use raw booleans for multi-state concepts.

### 4. Normative Evaluation Semantics & Precedence
- **Document Default Disposition**: Explicitly state the behavior when no policy matches (e.g. default allow / pass-through).
- **Order-Independent Precedence**: Define deterministic tie-breaking when multiple policies conflict (e.g. `ACTION_KEEP` overrides `ACTION_DROP`) to ensure commutative, race-free evaluation across distributed configs.
- **Exemptions vs. Allowlists**: `ACTION_KEEP` must act as an exemption so policies compose safely. To prune non-matching data, authors must configure `ACTION_DROP` with `negate: true`.

### 5. Package Hygiene & Dependency Decoupling
- **Package-Scoped Symbol Vigilance**: Protobuf and Go enum symbols are scoped to the package. Hoist shared types (`Action`, `ScopeField`) to `common_policy_types.proto` immediately to avoid duplicate symbol collisions across PRs.
- **Zero Horizontal Imports**: Policy protos must never import sibling policy protos; shared types flow strictly from common modules.

### 6. Granularity & Degenerate Structure Pruning
- **Granularity Rules**: For hierarchical or grouped telemetry (e.g. metrics vs. data points), explicitly define what triggers a shift in evaluation granularity.
- **Prune Empty Containers**: When all child elements are dropped, the parent container and any empty wrappers must be completely pruned rather than emitted as empty shells.
- **Disclose Stateless Boundaries**: If evaluation is stateless per-unit (e.g. per-span without trace reassembly), document operational side effects (e.g. orphan child spans).
