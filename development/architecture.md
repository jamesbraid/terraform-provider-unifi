# Provider architecture

This document explains how the provider is built: the shared resource engine
(`internal/resourcekit`, "the kit"), the per-surface descriptors that
configure it, how the schema packages those descriptors depend on get
generated, and what holds the whole thing honest in CI. It assumes you can
read Go and know the Terraform plugin framework's Create/Read/Update/Delete
shape; it does not assume you have seen this repository before.

If you are adding support for a new controller release, read
[new-controller.md](new-controller.md) instead — it is the playbook. This
document is the reference for what the pieces are and why they are shaped the
way they are.

## The problem the kit solves

Before the kit, every managed resource was a hand-written Go file implementing
`resource.Resource` directly: read the plan, resolve a timeout, resolve the
site, call the SDK client, write the state back, handle import. `dns_record`'s
version of that was 717 lines, most of it the same boilerplate every other
resource also wrote, once per resource.

`internal/resourcekit` holds that boilerplate once. A managed resource is now
a `Spec[M, S]` — model type `M`, SDK struct type `S` — plus a `Backend[S]`
bound to a real client, wired into `resourcekit.Resource[M, S]`, which
implements `resource.Resource` generically. What a surface author writes is
the list of `Field`s that map `M`'s attributes onto `S`'s, plus whatever hooks
and backend closures that surface actually needs. Twenty-one of the
provider's managed resources are served this way today (one of them,
`unifi_account`, by embedding another surface's kit resource rather than
declaring its own); the rest — settings, BGP, dynamic DNS, and a handful
of others — are still hand-written, each for a reason recorded in its own
file.

## Spec and Backend: the two halves of a resource

A kit-served resource is composed from two generic structs, both in
`internal/resourcekit/resource.go`:

- **`Backend[S]`** is the SDK client, reached through closures rather than an
  interface the SDK would have to satisfy. `Create`, `Read`, `Update`,
  `Delete`, `List`, `GetID`, `SetID` are functions bound to real methods on
  `*unifi.ApiClient` — `client.CreateDNSRecord`, `client.GetDNSRecord`, and so
  on. The method names are the one part of a resource the mapping artifact
  does not carry: `UpdateDNSRecordFields` is a different method from
  `UpdateDNSRecord`, and choosing between them is a decision about what the
  provider is allowed to overwrite, not a naming convention a generator can
  infer. Binding it as a closure means a wrong or renamed method fails at
  compile time, in the file that got it wrong, instead of at a runtime call
  the type system had no way to check.

- **`Spec[M, S]`** is everything about one resource that varies: its type
  name, its `Field` list, its hooks, and the accessors for the three
  attributes every managed surface carries but no policy declares as a field
  — `ID`, `Site`, `Timeouts`. These are closures for the same reason
  `Backend`'s methods are: an interface with setters needs pointer receivers,
  which would force a second type parameter through every generic signature
  in the package. A closure costs three lines per resource and is checked the
  same way the fields are.

**Create has two shapes, and exactly one applies per surface.** `Backend.Create`
POSTs a whole new object — correct for every surface except one, because the
controller holds nothing yet, so an attribute the plan left unset takes a
controller default rather than overwriting a live value. `unifi_device` is the
exception: a device is not created by the provider, it is *adopted* — the
object already exists, fully configured, before Terraform ever names it — so
its create is `Backend.CreateFields`, a masked PATCH built the same way an
update is. Exactly one of the two may be set; the kit refuses at build time
(via a panic-safe error, not a compile error, since the choice is per-instance)
if a descriptor supplies both or neither.

**Update has only one shape.** `Backend.UpdateFields` is a field-masked
write — the model this provider is built around, see below — and every
kit descriptor sets it. The kit once had a whole-object alternative,
`Backend.Update`, fetching the object fresh and applying only the masked
fields onto *that* so the same safety a field-masked write gives was
achieved by fetch-then-patch instead of by the wire encoding; no
registered surface ever set it, so it was removed rather than kept as
capability nothing used. Seven surfaces implement `resource.Resource` by
hand, entirely outside `Backend`: five of them (`BGPConfig`,
`PowerSupervisor`, `Setting`, `Site`, `WireGuardPeer`) have no
`Update<T>Fields` method on their SDK type at all, so a masked write
isn't an option. `DynamicDNS` does have `UpdateDynamicDNSFields` — its
hand-written resource just doesn't call it, calling the whole-object
`UpdateDynamicDNS` instead. `WAN` is the seventh, and the odd one out: it
shares `unifi.Network` with four kit-served surfaces, and its
hand-written `Update` does call `UpdateNetworkFields` against its own
hand-rolled field mask — a masked write built outside the kit's generic
machinery, not a whole-object one. See "The masked write model" below.

## Field: the mapping unit

Every attribute a descriptor maps implements `Field[M, S]`:

```go
type Field[M any, S any] interface {
    WireName() string
    ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics
    ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics
    SetInPlan(plan *M) bool
    CopyPlanToState(plan, state *M)
}
```

`SetInPlan` is the interface's load-bearing method, because *presence* is the
concept a create and an update share. A create sends every field the plan set
(everything else takes a controller default); an update sends only the fields
the plan set, named explicitly in a mask (everything else keeps the
controller's current value). One predicate answers both questions, so a field
kind that gets presence right is right on both write paths at once, and one
that gets it wrong is wrong on both.

Every field kind is a small, closure-based struct rather than a shared kind
driven by reflection or struct tags. That was a deliberate rejection of the
easier option: the plugin framework will happily decode a plan into a
`types.Object` and let the mapping be a table of strings, but that moves a
field's type and name from the compiler to runtime, in the one place a wrong
mapping should be least survivable. A closure that reaches an SDK struct field
is checked when the descriptor is built — a wrong field name does not
compile — where a string-keyed reflection map would fail, if at all, on the
first live read.

### What a zero means: `ElideZero`

Many fields carry an `Elide ElideZero`. This is not formatting; it decides
what an all-zero read from the controller means. An optional attribute the
practitioner never set comes back from the controller as a Go zero value —
`""`, `0`, an empty slice — indistinguishable on the wire from a
practitioner-supplied zero. `NullZero` says "treat that zero as absence" (null
in state, matching a configuration that never mentioned the attribute);
`KeepZero` says "a zero here is a real value" (required attributes, and
attributes with a static default). Getting this wrong produces either a
permanent diff (a zero the practitioner never asked for keeps reappearing) or
a silently discarded configuration (a real zero gets read back as null). The
generated schema carries the fact this decision is derived from —
`computed_optional_required` — which is why `ElideProblems` (see "The
validation story") can check a descriptor's claim against the schema rather
than trusting the author to have transcribed it correctly.

## Field kinds

Each kind below exists because one surface's SDK shape could not be expressed
by the others. They live in `internal/resourcekit/field.go`,
`conditional_field.go`, `object_field.go`, and `scattered_object_field.go`.

**Scalars** — `StringField`, `BoolField`, `Int64Field` map a plain Terraform
scalar to a plain Go scalar. `BoolField` deliberately has no `Elide`: a `false`
read back from the controller is a value, not an absence, so nulling it on
zero would fight a configuration that legitimately set it. `StringField` and
`Int64Field` accept a `WriteWhen` predicate that suppresses both the write and
the field's presence in the update mask — a field can be silenced entirely
under some other attribute's condition, and the predicate gates the mask as
well as the write so a suppressed field can never be named on the wire
carrying a stale value.

**Pointer scalars** — `Int64PtrField` and `BoolPtrField` map to `*int64` /
`*bool` on the SDK side, for a field where the SDK itself distinguishes three
states (absent, present-and-zero, present-and-set) that a plain scalar cannot
represent. `Int64PtrField` also carries `OmitZero`, a *write* rule distinct
from `Elide`'s *read* rule: some controller fields reject a literal zero
outright (a DH-group enum, a route distance), so a field that opts into
`OmitZero` sends nothing rather than a pointer to zero, including for an
*unknown* plan value that would otherwise collapse to the same zero pointer.

**Durations** — `DurationField` and `DurationPtrField` map a
`timetypes.GoDuration` string to an integer count of some unit (seconds,
typically), via the shared conversion in `unifi/util`. The unit is the one
fact no generated or policy artifact carries, so it is declared per field.

**Collections** — `StringListField` and `StringSetField` map a Terraform list
or set of strings to a `[]string`. The choice between them is not
interchangeable: a set is compared by membership, so a controller returning
group members in a different order than the practitioner wrote produces no
diff; rendering the same data as a list makes reordering a permanent,
un-suppressible diff. Both kinds explicitly allocate an empty slice rather
than leaving it nil when the model has no value, because a nil slice and an
empty one serialize differently to the controller for a field without
`omitempty` — "absent" and "present but empty" are different requests.
`StringSetField` additionally accepts `KeepPrior`, for the case where the
set's element type carries semantic equality (MAC addresses, for instance)
that the framework's set membership check does not consult — without it, the
controller's own spelling of a value silently overwrites the practitioner's
equivalent one, producing a diff nothing can settle.

**`ReadOnly[M, S](inner Field[M, S])`** wraps any field the controller owns:
read from the API, never sent, and never named in an update mask. It is a
decorator rather than a per-kind flag, so read-only-ness is one
implementation instead of one per kind, and a kind added later inherits it for
free.

**Custom string types** — `StringLikeField` and `StringLikePtrField` map any
type satisfying `basetypes.StringValuable` (`iptypes.IPAddress`,
`hwtypes.MACAddress`, and similar) to a plain or pointer SDK string. Folding
these into `StringField` was rejected because they answer a genuinely
different question: `StringLikeField` still elides on the read side the way
`StringField` does, while `StringLikePtrField` treats a nil pointer *and* a
pointer to `""` as the same absence, because some controller fields reject an
empty string outright and `StringLikePtrField`'s `ToSDK` therefore never
produces one — there is no third state left for `Elide` to distinguish.
`StringLikeField` also carries `WriteWhen`, needed where an SDK accessor
dispatches on a sibling attribute (a route that is either an interface route
or a next-hop route, and only ever writes one of the two SDK fields).

**Nested objects backed by an SDK struct** — `ObjectField` (a single nested
object) and `ObjectListField` (a list of them) carry a `types.Object` /
`types.List` on the model side and a `*E` / `[]E` on the SDK side, where `E` is
a real generated SDK struct. The kind does not map the object's members
itself — the descriptor supplies `Encode`/`Decode` for that, for the same
reason top-level fields are individually typed rather than reflected: a
nested object's members need the same per-field judgment calls (elision,
pointer-vs-value, derived values) that top-level fields get. What the kind
*does* provide is a check no descriptor can perform on itself:
`NestedProblems` walks the SDK struct's own JSON tags and reports any member
without `omitempty` that the object's declared attribute types do not cover
and the descriptor has not explicitly named as `Unmodelled`. A field mask
names top-level keys only — there is no way to say "send `source.zone_id` but
not `source.match_mac`" — so masking the parent sends every member the model
does not carry as that member's Go zero, on every apply.

**Nested objects with no SDK struct** — `ScatteredObjectField` is for a model
object whose members are several unrelated flat fields on the SDK type, with
no struct grouping them (`vpn_client`'s `wireguard` spans ten sibling fields
on `Network`, related only by the Terraform schema). Its `Wires []string`
names every SDK field it spans, and *all* of them join the update mask when
the object is set — naming a subset would write a subset while the apply
reports success. Two further fields refine that: `ConditionalWires` names a
wire `Encode` writes only sometimes (with the predicate for when), so the mask
does not carry a wire go-unifi would then zero out; `ReadOnlyWires` names a
wire that is decoded but never encoded (a controller-issued key the API
rejects on write), so it stays out of the mask while still being declared and
checked against the SDK's JSON tags. Both are backed by their own conformance
checks (`ConditionalWireProblems`, `MaskedZeroProblems`) — see "The validation
story."

**Write-only has no kind of its own.** A generic `WriteOnlyStringField` for a
`terraform-plugin-framework@v1.19` `WriteOnly` schema attribute was built and
then removed: no descriptor ever set it. Terraform nulls a write-only
attribute in every plan it persists to disk, on both the plan and apply RPCs,
so by the time `Create` or `Update` run, the plan carries null for it no
matter what the practitioner wrote — only `req.Config`, re-evaluated fresh at
apply, still holds the value. The provider's three write-only attributes —
`wlan`'s `passphrase_wo`, `vpn_client`'s `wireguard.private_key_wo`, and
`site_to_site_vpn`'s `pre_shared_key_wo` — each read config directly in a
hand-written `BeforeSend` hook and stash the value onto the working model
before the rest of the write path runs, then null it back out of the model
in `AfterReceive` so it never survives into recorded state. A shared kind
would only have centralized the null-and-stash boilerplate, not the
judgment call each surface makes about *what* to stash the value onto — so
each of the three still owns its own hook.

## The three hooks

Most surfaces need none of this: their wire form is a pure function of their
own attributes, expressible entirely as a `Field` list. Three surfaces are
not, and rather than let each invent its own escape hatch, `Spec` declares
three hook points, all optional:

```go
Prefetch     func(ctx context.Context, site string) (any, diag.Diagnostics)
BeforeSend   func(ctx context.Context, config, effective *M, sdk *S, prefetched any) diag.Diagnostics
AfterReceive func(ctx context.Context, sdk *S, model *M, prior M, prefetched any) diag.Diagnostics
```

`port_profile` is why these exist. The practitioner declares which VLANs
*are* tagged; the controller only accepts which are *excluded*. Computing the
complement needs the site's whole network inventory — an object this
resource does not own and has to fetch separately, and a decision about
meaning rather than about mapping, so it is not something a generator should
ever emit. `Prefetch` runs once, before the SDK object is built, and hands its
result to the other two hooks.

`BeforeSend` takes *two* models, `config` and `effective`, because they answer
different questions. `config` is what the practitioner literally wrote — used
by a hook that must reject an illegal combination the practitioner actually
typed (e.g. rejecting `tagged_networkconf_ids` set together with
`excluded_networkconf_ids`). `effective` is the model the SDK object was just
built *from* — the plan on create, the state-with-plan-applied on update —
used by a hook that *derives* a value and needs to see everything the object
will carry, not only what changed.

`AfterReceive` takes `prior`, the model as it stood before this read
overwrote it (the plan on create, the state on read, the state-with-plan on
update). Without it, an attribute no `Field` claims cannot be reconstructed on
refresh — `unifi_device`'s `port_override` is rebuilt from the *managed* port
set carried in `prior`, not from every port the switch happens to report — and
an attribute a `Field` *does* decode has already been overwritten in `model`
by the time `AfterReceive` runs, so `prior` is the only place a hook can still
see what was there before. The clearest case this closed:
`vpn_client`'s `wireguard` block is written from a config file the provider
parses once; without `prior`, every subsequent refresh forgot which mode the
practitioner had configured and produced an apply Terraform refuses outright
rather than a diff.

A fourth, narrower hook, `BeforeDelete`, decides whether destroying the
*resource* destroys the *object*. `unifi_device` is the case: a device is
physical hardware, so "delete" can only mean "un-adopt," and that has to be
opt-in (`forget_on_destroy`) rather than automatic — without this hook every
`terraform destroy` targeting a device would forget real hardware whether the
configuration asked for it or not.

## The masked write model

The update path is built around one question: for this apply, exactly which
SDK fields is the provider allowed to touch?

`Spec.WireFields(plan)` answers it. It walks every `Field`, keeps the ones
`SetInPlan` reports true for, and collects each one's wire name(s) — a
`ScatteredObjectField` contributes every wire it spans, everything else
contributes one. `Spec.AlwaysWire` adds names unconditionally: some attributes
are derived by `BeforeSend` from a *different* attribute entirely (again,
`port_profile`), so nothing in the plan puts them in the mask on their own,
and without forcing them in, an update could silently write nothing for an
attribute the practitioner did change. An empty result is refused outright —
a masked update naming no field is a no-op patch, and the kit treats that
as a descriptor defect rather than sending it.

`Spec.UnwritableWires` exists because one SDK type, `unifi.Network`, carries
all seven of the controller's network purposes across five surfaces (`wan`,
`vpn_client`, `vpn_server`, `site_to_site_vpn`, and plain networks), and
which of its 263 exported fields the encoder will actually emit depends on
which purpose is set. A field a descriptor declares but the *current*
object's encoder will not emit is not a "send zero" situation (which is what
an omitted mask name would normally mean) — go-unifi hard-errors on a masked
name it never emits at all. This
hook reports what to drop before the mask reaches the wire, and the kit
subtracts, so a purpose-mismatched field never reaches the controller as an
error the practitioner cannot act on.

Six of the seven hand-written surfaces don't go through this path at all:
each builds and sends its own whole object in its own `Update` method,
with no mask and no kit involved. `WAN` is the exception — its `Update`
does mask, but by its own hand-rolled logic rather than the kit's.

**Plan wins over response, for anything the plan set.** After a create or
update returns, `Spec.ApplyPlanToState` copies every `Field`-covered attribute
the plan set back over the response — the response only fills in attributes
the plan left null or unknown. This exists because a controller's response is
frequently *silent*: a VLAN-only network's encoder omits the large majority of
that surface's wire fields, so trusting the response wholesale would read
back null or false for values the practitioner explicitly configured, and
Terraform correctly refuses that as an inconsistent result.
`copyUncoveredPlanValues` extends the same rule to attributes *no* `Field`
claims at all — an attribute entirely owned by a hook (network's `vlan`,
derived by `BeforeSend` into two different wire fields) would otherwise never
have its plan value copied anywhere, because the generic `Fields` walk simply
never visits it.

## Import routing

Every kit-served resource shares one `ImportState` implementation, because
every one is scoped by site and identified by an opaque controller ID. It
accepts three forms:

- **`"site:id"` or `"id"`** — the baseline. A prefix before the colon sets
  `site`; what remains is checked against `^[0-9a-f]{24}$` to decide whether it
  looks like a controller ID at all.
- **Import by name** — a surface that supplies both `Spec.Name` and
  `Backend.ReadByName` accepts a human handle instead of an ID: an explicit
  `"name="` prefix, or any handle that does not match the ID shape, is routed
  onto the name attribute and resolved on the resource's first read.
  `unifi_network`'s `name=Test VLAN` and `unifi_wlan`'s bare SSID both work
  this way. Without both members set, every handle routes to `id` exactly as
  before — the two exist as a pair because routing without a resolver strands
  the handle on an attribute nothing reads.
- **Import blocks with an identity** (Terraform 1.12+, `import { identity =
  {...} }`) — core hands the handle through `req.Identity` instead of
  `req.ID`. Every managed surface but two declares a one-attribute identity
  schema (`id`, `RequiredForImport`), and `ImportState` reads it as a
  fallback when `req.ID` is empty; `unifi_bgp` and `unifi_setting` don't
  implement `resource.ResourceWithIdentity` at all, so those two import by
  `site:id`/`id` only. The identity is also *set* on the import response itself,
  not only in the following `Read` — leaving it null there produces a
  confusing "Missing Resource Identity After Read" instead of a clean
  not-found when the imported handle names nothing.

## List resources

25 of the provider's 28 managed resources also serve a list resource
(`list.ListResource`). Only the 20 kit-served surfaces configure theirs
through `ListSpec[S]` in `list.go`; the other five (`dynamic_dns`, `wan`,
`power_supervisor`, `site`, `wireguard_peer`) implement `list.ListResource`
by hand, the same way they implement `resource.Resource`. Because every
list surface in the estate declares the same shape — an optional site, and
a repeated `filter { name = ...; value = ... }` block — `ListConfig`/
`ListFilter` are shared types rather than generated per surface.
What a descriptor supplies is `ConfigSchema` (the generated list schema, one
function per surface, passed rather than derived because the kit cannot name
a per-package function generically), `DisplayName` (which field identifies one
result to a human), and `Filters` (a name → string-rendering function per
filterable attribute — rendered as strings on both sides, because a `filter`
block's `value` is always a string on the wire regardless of the underlying
attribute's type).

## The generated-schema relationship

A descriptor's `Fields` list is hand-written, but the schema those fields are
checked against, and the Go model type they're bound to, come from a
generator. The pipeline, per surface:

1. **`cmd/sdk-bootstrap`** derives a *bootstrap*: which fields a resource's
   go-unifi SDK struct carries and what shape each one is, read directly from
   the type checker against the go-unifi version `go.mod` resolves — the
   bootstrap records that module, version and commit — rather than
   transcribed by hand. This closes off an entire class of drift — a
   bootstrap can no longer describe a field as something it is not, because
   it is generated from the struct's own declaration.
2. **`provider-codegen/policy/<surface>.json`** is the human-judgment half:
   which SDK field a released Terraform attribute maps to (where the names
   differ, or the mapping is not 1:1), which fields the provider deliberately
   does not expose, and the schema behavior — validators, plan modifiers,
   defaults, custom types — that a structural comparison can never infer.
3. **`cmd/provider-spec-compiler`** merges a surface's bootstrap and policy
   into a `provider-code-spec.json` — the format
   `terraform-plugin-codegen-framework`'s `tfplugingen-framework generate`
   consumes to emit Go under `internal/generated/<kind>_<surface>/`. That
   tool's own subcommands are only `all`, `provider`, `resources`, and
   `data-sources` (`tfplugingen-framework generate --help` shows the current
   set, checked against this worktree's pinned version) — there is no `list`
   or `action` subcommand, even though the code-spec format it reads carries
   its own `listresources` and `actions` members.
4. Two of this repo's own tools cover exactly that gap: `cmd/list-resource-gen`
   and `cmd/action-gen` read a code-spec's `listresources`/`actions` members
   and emit the Go the upstream generator has no subcommand for at all. A
   further handful patch what the upstream generator does emit, imperfectly:
   `cmd/nested-type-dedup` resolves a name collision the generator produces
   when two nested blocks share an attribute name; `cmd/nested-custom-type-strip`
   and `cmd/generated-value-strip` remove generated bindings and value-layer
   code nothing in the runtime calls (every nested object here is handled as
   a plain `types.Object`, never the generator's own `<X>Value` type — see
   `ObjectField`'s doc comment for why that door is closed); `cmd/metadata-contract-gen`
   freezes the practitioner-visible type name for each generated surface.

A descriptor then binds one generated schema function and one generated model
struct into a `Spec` (see `dns_record_descriptor.go` for the shortest
complete example) — the model's `tfsdk` tags are what the framework reflects
on to move data in and out, and the schema function is what `Resource.Schema`
serves.

`go generate ./...` re-runs this whole pipeline and is expected to reproduce
`internal/generated/*` exactly; CI's `generate` job runs it and fails the
build on any diff, so the committed generated code can never silently drift
from what the generator would produce today.

## The validation story

Several independent things hold this architecture honest, at different
layers:

- **Conformance instruments** (`internal/resourcekit/*_check.go`) ask
  structural questions of a `Spec` that nothing else checks. `WireNameProblems`
  compares every field's declared `Wire` name against the SDK struct's own
  JSON tag by reflection — the wire name is hand-transcribed, and a typo here
  does not fail to compile, it silently masks a field the controller does not
  have. `ElideProblems` compares a field's `Elide` claim against what the
  generated schema says the attribute's computed/optional/required
  disposition actually is. `ZeroReadProblems` runs every field's `ToModel`
  against a fully zeroed SDK object — the one input every surface must
  tolerate, since the controller omits whatever is unset — and reports what
  panics or produces a value its own type rejects. `NestedProblems` (see
  `ObjectField`, above) and `MaskedZeroProblems`/`ConditionalWireProblems`
  (see `ScatteredObjectField`, above) check the two nested-object mask hazards.
  Each check ships with its own positive control: a test that runs the check
  against a deliberately broken probe and confirms it actually fires, because
  a check that never fails is indistinguishable from one that was never
  wired in. Every descriptor's tests in `unifi/` invoke these against its own
  `Spec`.
- **The golden schema snapshot** (`unifi/schema_snapshot_test.go`,
  `unifi/testdata/schema-snapshot.json`) serves every resource, data source,
  list resource, and action in-process and diffs the result — including
  `Default`s, plan modifiers, validators and custom types, none of which the
  wire protocol itself can express and which older baseline-style
  comparisons therefore could not see either — against a committed
  snapshot. A schema change fails until the snapshot is regenerated
  (`UPDATE_SCHEMA_SNAPSHOT=1 go test ./unifi/ -run TestProviderSchemaSnapshot`)
  and the diff to that file reviewed and committed alongside it, so an
  intended schema change is a visible diff and an unintended one is a
  failing test.
- **Write-path classification** (`unifi/write_paths_test.go`) reads every
  kit surface's `Backend` and asserts, per surface, that exactly one of
  `Create`/`CreateFields` is set — and records which. This is what pins
  "only `unifi_device`'s create is unmasked" as a checked fact rather than
  a claim a reader has to take on faith. Update needs no such check: every
  kit surface's `Backend` has only `UpdateFields` to set.
- **The unfailable-test inventory** (`internal/testaudit`) parses every `_test.go`
  file in the module and flags a test that structurally cannot fail: an empty
  table-driven case, an unconditional `t.Skip`, or a populated table whose
  body asserts nothing. A test with a name describing a real behavior and no
  way to fail is worse than no test at all — it reads as coverage to a
  reviewer and is not.
- **The acceptance suite** (`internal/controllertest`, and every
  `_test.go` behind the `acceptance` build tag) runs the real provider against
  a pinned, emulated UniFi controller. It is the only layer that can observe
  what the controller actually does with a given write — which of the
  guarantees above hold only on paper until something round-trips through a
  live API. See [new-controller.md](new-controller.md) for how to run it and
  what pinning the emulator image buys.

Together, these mean a descriptor's internal consistency (does it agree with
the SDK, the schema, and itself) is checked without a controller at all, and
only the surface's actual *behavior* against the API needs the acceptance
suite — which is also why the unit and conformance suites run on every
change while the acceptance suite is reserved for a real controller boot.

## Extending the kit

Adding a **surface**: follow [new-controller.md](new-controller.md)'s
generation steps to get a schema and model, then write a descriptor —
`<surface>_descriptor.go` in `unifi/` — following the shortest existing one
(`dns_record_descriptor.go`) as a template, and let the conformance
instruments in "The validation story" tell you what you got wrong before an
acceptance test would.

Adding a **field kind**: only after checking whether an existing kind already
covers the shape (`internal/resourcekit`'s own comments call this out
explicitly). A new kind should live beside the others in
`internal/resourcekit`, implement the four `Field` methods, and be taught to
`ElideProblems`, `ZeroReadProblems`, and `WireNameProblems` if its zero-value
or wire-name semantics differ from what those checks assume by default —
`StringLikePtrField`'s addition to `elideExempt` is the template for that.
