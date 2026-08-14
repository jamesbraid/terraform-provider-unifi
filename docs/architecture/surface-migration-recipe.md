# Migrating a surface to the generated schema

Surfaces compile from a catalog and a policy one at a time, and the ledger is
the count — this document is the method, not the tally, because a number
written here is wrong by the next landing.

Most of it describes a **managed resource**. List resources and actions are not
projections of an SDK struct and diverge from step 2 onward; see *The other
surface kinds*.

It was assembled from the commits that did the first several (`d78e7a56`
onward) so the rest do not have to rediscover it.

Everything here was learned by something going wrong. Where a step exists
because of a specific failure, the failure is named — a rule whose reason is
missing gets dropped by the next person who finds it inconvenient.

## The order

Two orderings are load-bearing and the rest is convenience.

**Step 1 must come first.** An inventory taken before the migration and
unchanged afterwards proves behaviour was preserved; taken afterwards it proves
only that the new schema matches itself.

**Step 2 must come before step 4** — read the Go before writing the *policy*.
That is the constraint `firewall_policy` violated, at a cost of nine validators.
It is not a constraint about the bootstrap: steps 2 and 3 are independent, and
`wlan` was derived before its schema was read, deliberately, to size the
omission set before committing to the surface.

**Steps 5 and 7 are one commit**, for the reason below.

1. **Take the behaviour inventory first, before touching anything.**
2. **Read the hand-written schema.** Not the released JSON — the Go. The JSON
   cannot show you validators, plan modifiers, defaults or custom types.
3. **Derive the bootstrap from the SDK.** Do not write one.
4. **Write the policy, transcribing behaviour** — validators, plan modifiers,
   defaults, descriptions — rather than describing shape alone.
5. **Move the state to `generated_shadow` with its `migration` reason.**
6. **Regenerate**, and commit the generated Go.
7. **Re-cut the five wave receipts in the same commit as the state change.**
8. **Rewire the resource to serve the generated schema.**

**Step 8 destroys the input to step 4.** `cmd/schema-behaviour` reads the
hand-written schema out of the resource, and step 8 replaces it. Once rewired,
re-deriving the behaviour half writes NOTHING and says so — *already serves a
generated schema* — and a policy rebuilt at that point silently loses every
validator, plan modifier, default and custom type it had. `vpn_client` lost
eleven that way. The behaviour golden refuses all of them, so it fails loudly,
but the cheaper habit is to **re-derive before rewiring, or revert the rewire to
re-derive.**

### Why the inventory cannot be taken later

It costs nothing to take early and cannot be reconstructed afterwards, because
by then the hand-written schema is gone. In practice it is free: the golden is
already committed, so a migration that preserves behaviour leaves `git status`
clean and needs no separate step.

### Why the state stops at `generated_shadow`

Admission wants a receipt from a campaign run that diffs the generated resource
against the hand-written one, and that run has not happened when the schema
first compiles. `generated_shadow` is a waypoint on the designed path rather
than limbo — **but only when someone says so**, which is why the overlay
requires a `migration` reason in prose rather than a flag. A flag is something
set to pass a gate. A surface left in limbo by accident is exactly what the
wave checkpoint exists to catch.

### Why receipts move in the same commit

The five wave receipts pin the ledger digest, so a state change moves them by
construction. Splitting them into a follow-up commit leaves a receipt
describing a ledger that no longer exists — and a receipt records what was
true, not what is convenient to record.

## What the policy has to carry

A policy that describes which attributes exist, their types and their
dispositions **is not a description of the schema**. A schema also says what
values are accepted, what happens when a value is absent, and when a computed
value may be assumed unchanged.

`firewall_policy` was migrated with shape alone and lost **nine validators,
thirteen plan modifiers and nine defaults**. The plan modifiers and defaults
had tests and went red. **The validators did not — seven enum constraints on
`action`, `protocol`, `matching_target` and `ip_version` disappeared with the
whole suite green**, and were found only by reading the old schema beside the
new one by hand.

Transcribe descriptions rather than writing them. The released text is
authoritative because it is published; two descriptions were wrong for no
reason other than having been retyped.

**And transcribe `sensitive` with them.** Reading dispositions and descriptions
out of the released contract and stopping there dropped three of `vpn_client`'s
secret flags — `peer.public_key`, `preshared_key` and `configuration.content`.
The attributes keep their names, types and descriptions, so **nothing except the
projection referee can see that three secrets stopped being marked secret**, and
the compiler's own secret guard does not reach them: it checks the catalog's
secret candidates, which are the `x_`-prefixed SDK fields, not what the released
schema chose to hide.

**Restore behaviour you disagree with.** Several of `firewall_policy`'s
defaults are arguable — the optional-plus-computed inventory argues against
them. Whether they should exist is a question for a live controller, and
dropping them during a migration answers it by accident. **A migration changes
how the schema is produced, not what it says.**

## Renames

Nine of `wlan`'s ten renames fail loudly if guessed: the name simply is not
there. **Derive them from the resource's model-to-SDK assignments, never from
the names.** `minrate_ng_data_rate_kbps` is the 2G rate and `minrate_na` is 5G,
which is backwards from how they read, and no inspection recovers that.

**And one rename does not fail loudly, which is why this is its own section.**
`wlan`'s `schedule` block is built from `schedule_with_duration`. The SDK *also*
has a `schedule` — an array of encoded strings, the legacy form the provider
stopped using. **Matching by name finds a real field, of a plausible type, and
it is the wrong one.** It was caught only because the compiler refuses a
cardinality change, and that guard was written for something else entirely.

`cmd/policy-scaffold` maps what a name can tell you and **declines to guess a
rename**, reporting what it could not place. Do not improve it into a guesser.

### `internal/renamecheck` reports a floor, not a ceiling

It is the right source — it reads the conversion code rather than the names —
but **read its silence as "not resolved", never as "not there".** Across the
estate it resolves 625 conversions and declines 682, and it now says so on every
run.

Two limits, and they compound. It resolves an expression naming **exactly one**
attribute, so a value staged through a local — which is how every many-into-one
conversion here is written — yields nothing. And `composite` walks literals of
**go-unifi structs only**, so a MODEL built with a literal is never visited at
all: `vpn_server`'s `wireguard.public_key` is absent from its output for that
reason, and building a nested object with a literal is the ordinary style.

The second limit cannot even be reported, because nothing is visited. **So for a
binding renamecheck does not list, read the conversion code — do not fall back
to the scaffold**, which matches on names and is the known-wrong source.

That was invisible until recently: `Unread` was declared, sorted, and never
appended to, so every run reported zero unreadable conversions however much it
had skipped, and the claim count read as coverage over a denominator nobody
could see.

### The rename tell

A wrong rename appears in the behaviour inventory as **a removal and an addition
in the same run**. Every other failure the loop reports is one fact with one
message, so the pair reads as two unrelated problems — someone chases the
removal, restores the behaviour under the wrong name, and the addition persists.
**It is the only place the convergent loop misleads.**

### Know the target count before you start

`grep '^unifi_<surface>\.' unifi/testdata/schema_behaviour.txt` gives the exact
number of behaviours to account for — 83 for `wlan`, 46 for `firewall_policy`.
Knowing it up front makes "am I finished" arithmetic rather than judgement,
which is the difference that matters on a wide surface.

## The four traps

Each of these is invisible to at least one gate, and each has bitten exactly
once so far.

### 1. A `PriorSchema`-building state upgrader

**Exactly one surface has one: `firewall_policy`.** Its v0 upgrader derives the
prior schema from the live one and swaps a port back to an integer. That worked
while the schema was hand-written and plain. The generated schema wrapped both
endpoints in a **custom object type**, and a custom type overrides the attribute
map — so replacing the attribute left the prior schema still calling the port a
string.

**Nothing would have failed.** v0 state would have failed to decode during
someone's plan, months later, on a path no test exercises. Check for an upgrader
that builds a `PriorSchema` before you migrate, and if one exists, assert the
prior type directly in a test — `firewall_policy_resource_test.go` does this by
reaching for `attr.TypeWithAttributeTypes` rather than comparing a type name.

### 2. Blocks

**Three surfaces carry blocks: `device` (`port_override`), `radius_profile`
(`acct_server`, `auth_server`), `wlan` (`schedule`).**

A repeated object can be written as a block or as a nested attribute and the SDK
says `[]T` either way, so **the policy must declare which** — the same decision
`list` and `set` already require, for the same reason. Blocks are emitted under
their own specification member, because Terraform treats the two differently and
configuration written for one does not parse as the other.

**A block has no `computed_optional_required`.** Its presence is expressed by how
many times it appears in configuration, and the specification's JSON schema
forbids the member. The Go type in the same library carries it anyway, so a
policy that sets one produces a document that type-checks, marshals, and
satisfies every assertion the compiler makes about its own output — and is then
refused by the generator with a **JSON path instead of a field name**. If you
see a schema-validation error pointing at a path inside a block, this is why.

That disagreement between the library's type and its schema is also why block
support was verified by generating real Go rather than by inspecting the emitted
document. The first attempt produced a specification the compiler was perfectly
happy with and the generator would not parse.

### 3. Custom types

`power_supervisor` declares `timetypes.GoDurationType` on three attributes and
`hwtypes.MACAddressType` on a fourth. **These stay strings on the wire**, so the
projection test cannot see them. Dropping one stops a value being parsed and
validated with nothing going red.

Custom types also override an attribute map, which is what broke the state
upgrader above — so an inventory of type *names* would not have caught that
either. Both facts are guarded now, separately, because they fail in different
ways.

### 4. A surface that fronts TWO SDK structs

**Exactly one does: `unifi_client_list`.** Its `clients` element carries 42
attributes and the `Client` struct carries 13. The other 29 — `ap_mac`, `bssid`,
`rssi`, `rx_bytes`, `uptime` and the rest — come from `ClientInfo`, fetched by
two further calls and joined on user ID.

**A bootstrap describes ONE struct, so those 29 attributes are not observed at
all**, and exactly-once accounting is silent about a field it never saw. The
policy would have to call each of them `invented`, which compiles, reads as
"the provider computes this", and is false: they come off the wire.

Count the released attributes against the struct's fields before sizing a
surface. A large gap is usually omissions, which are fine; a gap in the other
direction — more attributes than fields — means a second source, and the shape
of this method does not reach it yet.

## At scale, what degrades is readability, not correctness

`wlan` omits 54 SDK fields against 55 released attributes. `network` omits 196
of 263 and its data source 185. Nobody reads those lines, and for a while this
section said that made the protection against a *wrong* omission weaker. **That
was wrong, and the distinction matters because it changes what a large surface
costs to land.**

**An omission is not a decision.** A field is omitted if and only if the released
schema does not expose it, and the released schema is the contract. So the
omission set is DERIVED from the contract rather than chosen — and a wrong one is
caught mechanically: the built schema would be missing an attribute the baseline
has, and **the projection referee names it**. Two hundred omissions are one
decision, *expose exactly what shipped*, applied two hundred times and verified
by comparison.

**What human review would add is a different question and not this method's job.**
It would find fields that *should* be exposed and are not in the released schema
either. That is a pre-existing gap in the provider, not a migration defect, and
it deserves its own pass across all sixty-seven surfaces rather than being
smuggled into whichever one happens to be large.

So say the accurate thing in the ledger rather than the cautious one: the
omission set is derived from the contract and verified by the projection referee,
it was not hand-reviewed, and hand-review would not have added a correctness
guarantee. **What a wide surface genuinely costs is that its policy is unreadable
in review — which is a reason to trust the referees, not to distrust the
result.**

## Why there are two referees

Terraform's schema protocol carries **types, dispositions, descriptions and
deprecation, and nothing else.** Validators, plan modifiers and defaults never
appear in it — the released v0.101.2 baseline contains no such key anywhere in
the document.

So a comparison against a released schema, however thorough, is structurally
incapable of seeing them. That is not a gap to be closed by widening the first
referee; it is a second thing that needs its own referee. The behaviour
inventory walks every registered resource at every depth and pins validators,
plan modifiers, defaults and custom types, carrying **each validator's own
description** — so changing an enum member changes a line, rather than leaving
the count intact.

Both referees must read **`Schema.Blocks` as well as `Schema.Attributes`** —
they are different maps, and for a while neither gate read the second. The
projection test built its comparison from block *attributes* and logged
`block_types` as uncovered on every run. That was survivable while every surface
was hand-written and nothing was removing blocks. It stopped being survivable
the moment a surface could be generated, because the compiler emits nothing it
cannot describe.

### Being named in a log is not coverage

The accounting test that says every fact a code specification can carry is
watched used to accept prose. `blocks` justified itself by pointing at the
projection test naming it on every run — true, printed on every run, and not
coverage. Migrating `wlan` would still have deleted its `schedule` block.

**Two of the six keys recorded as needing no comparison turned out to be live
hazards** — `custom_type`, which broke a state upgrader and left sixty-eight
declarations unguarded across the estate, and `blocks` — on a list whose whole
purpose was to say these are fine. An entry must now name a test that fails,
record that a wrong value cannot build, or say the key is unused here.

The question to ask of any such entry is *what would go wrong if this changed
and nothing noticed*, not whether the stated reason sounds right.

## The other surface kinds

Everything above describes a **managed resource**, whose schema is a projection
of an SDK struct. Two kinds are not, and the steps that assume a struct do not
transfer.

### List resources

A list resource's config schema is a **query**, not a projection: which site to
look in, and which filters to apply. **No attribute comes from the wire.** All
twenty-five are one optional `site` string and one `filter` block of two
required strings, with three exceptions — `unifi_site` takes no site selector,
`unifi_client` adds a `group`, and `unifi_wireguard_peer` requires a
`network_id`.

**Nothing upstream generates them.** `terraform-plugin-codegen-spec` v0.2.0
carries exactly `DataSources`, `Provider`, `Resources` and `Version`, and
`tfplugingen-framework` v0.4.1 offers `all`, `data-sources`, `provider` and
`resources`. There is no member to emit into and no step to call, at any
published version. Both halves are ours: the `listresources` member is added in
`internal/providercompiler`, and `cmd/list-resource-gen` renders it into Go.

**Derive the policy from the released schema, not from the Go.** This is the
one instruction that differs from step 2 above, and the reason is not
convenience. The golden a generated schema is checked against was itself cut
from the provider. Deriving the policy from the same source would mean the
policy and its check came from one place, and **a transcription mistake would
agree with itself**. `cmd/list-policy-scaffold` reads
`provider-contracts/schema/terraform-1.15.8.json` — the contract — so the two
can disagree, which is the only reason their agreement means anything. It
produced sixteen policies with no hand editing, and the three outliers fell out
on their own; an outlier table written by hand from attribute names had
`wireguard_peer.network_id` wrong.

Shape of the policy: **every SDK field of the listed type omitted by name**, the
site selector `provider_owned` with `generated: true`, and the filter block a
**grouping whose members are all `invented`**, declared as `list_nested_block`.
Groupings could not be blocks before this; blocks were only routed from observed
fields, and a filter block has no observed field behind it. Emitted under
`attributes` instead, the generator accepts it and renders a nested attribute —
**the same data with different configuration syntax, and every practitioner's
HCL breaks.**

**A filter member is a KEY, not a field.** `filter.name` holds the *name of the
field to select on*; the sibling `value` is what gets compared against it. It
collides with an SDK field on most surfaces and is still invented. The scaffold
refuses a member only when the block is not the key/value shape: refusing on the
collision alone would refuse twenty-one of the twenty-five, and **a guard that fires
on the normal case is one people learn to route around.**

**Three referees, none of which subsumes another.** Uniformity compares
structure across all twenty-five and deliberately ignores prose. The golden
pins all hundred and one strings including descriptions. The contract
comparison reads the released projection. Proven distinct by mutation: drop a
description and the golden fails while uniformity stays green; change a
disposition and both fail; **reword a description and then do what the golden's
own message tells you — regenerate it — and the golden goes green while the
contract comparison still fails.**

That last one is why the third exists. **A golden cut from the implementation is
a regression test; a comparison against the contract is a conformance test.**
Only the second survives someone asking how you know the golden was right the
day it was written.

**The rename referee does not apply.** A list policy binds no SDK field, so it
has no rename to check — but its generator name matches the managed surface it
lists, so the default mapping found that surface's conversion file and counted
twenty-five surfaces as checked. Nothing went red; the reported coverage simply
doubled while the claim count stayed at 224. `conversionFile` now allow-lists
`managed_resource` and returns empty otherwise.

### Actions

The estate has one, `unifi_port`. Its schema is **its arguments**, not a
projection of the `Device` it acts on, so all 109 SDK fields are omitted and the
three arguments are provider-owned. `cmd/action-gen` emits it; there is no
action subcommand upstream either.

**`device_mac` carries a `hwtypes.MACAddressType`, and that custom type IS the
validation.** The protocol renders it as a plain string, so the released
baseline records `"string"` — a generated schema that dropped the type satisfies
every schema comparison and accepts any string as a MAC address. This is the
same class as the three traps above, and it is the failure a *generator*
actually produces: rewording a description is a human mistake, while silently
restating a policy without its custom type is what code generation does.

**Derive the behaviour half; do not type it.** `cmd/schema-behaviour` merges the
custom type into the policy with its import and value type. It read only
`resource.SchemaResponse` and `resource.MetadataResponse` methods until this
surface needed it, so a policy it wrote for the action would have omitted the
MAC type without saying so. That gap surfaced by composition rather than search:
widening the behaviour inventory to walk actions made
`Test_schemaBehaviourIsDerivable` fail, and that test named the tool.

**`timeouts` stays `provider_owned` with `generated: false`**, grafted by the
kernel as everywhere else. The deriver reports it as unreadable from source,
which is correct — its value is `timeouts.Attributes(ctx)`, not a literal.

### Two emitters, deliberately not one

`cmd/list-resource-gen` and `cmd/action-gen` are separate straight-line tools.
They overlap only in "write a schema literal": a list config schema is strings
and one fixed block with a constant import pair, while an action schema has no
blocks, mixes scalar kinds, and carries custom types whose imports must be
collected. Merging them would put twenty-five working surfaces behind a change
made for one new one, for a shared core of a dozen lines. **A third use case
should merge them, not the second.**

## Smaller things that cost an afternoon

- **`tfplugingen-framework` does not create its output directory.** A new
  surface needs one before its first run. `dns_record` hides this by having had
  generated Go committed from the beginning.
- **Derive the bootstrap; do not write one.** A hand-written bootstrap for
  `firewall_policy` omitted six SDK fields — the record ID, four controller
  bookkeeping flags, and the site ID. Nothing could have caught it: exactly-once
  accounting checks that every *observed* field is classified, and a field the
  bootstrap never mentions is not observed. The policy was complete with respect
  to a projection that was itself incomplete.
- **A `terraform_type` override is checked for cardinality.** A field declared
  as a list over a scalar used to compile clean, which is what an SDK changing a
  slice to a plain value looks like from here.
- **After changing the compiler, regenerate and commit the mapping artifacts.**
  The unit suite does not regenerate; CI runs `go generate` then
  `git diff --exit-code`. Those answer different questions — whether the
  compiler is correct, and whether its committed output is current — and passing
  the first tells you nothing about the second. This turned CI red twice.
- **A custom validator or plan modifier must live in a leaf package.** Anything
  declared in package `unifi` is unreachable from generated code, and it cannot
  be reached by importing, because `unifi` imports the generated packages — the
  dependency runs the other way and adding one would be a cycle. `ap_group`'s
  `keepEquivalentMACs` hit this; the helpers live in `unifi/planmodifiers` now.
  `power_supervisor` only ever worked because its validators were already in
  `unifi/validators`.
- **The specification cannot express `write_only`.** v0.2.0 has no such member,
  so a surface with a write-only attribute must **graft** it in the kernel, as
  `wlan`'s `passphrase_wo` does and `timeouts` always has. Generating it instead
  silently drops the one property the attribute exists for.
- **Omitting an SDK field is fine, but say so by name.** `firewall_zone` leaves
  out seven of its twelve SDK fields because they are controller bookkeeping the
  released schema never exposed; the policy names each one.

- **A generated file with no generator never changes, so determinism cannot see
  it.** A list surface landed outside its batch had its artifacts committed with
  no `//go:generate` directive producing them. `go generate ./... && git diff
  --exit-code` — the strongest routine check here — passed, because an orphan is
  trivially reproducible. **It was found by auditing all twenty-five from the
  artifact side**, which is the only direction that can find a missing producer:
  nothing you do from the generator side will ever mention a file no generator
  names.
- **Resolve a derived artifact by regenerating it, even when it merged cleanly.**
  The trigger is being *derived*, not being *conflicted*. During the branch merge
  `unifi/testdata/schema_behaviour.txt` merged with no conflict at all, because
  both sides only added lines — and a clean text merge of a generated file is not
  evidence the result is what the generator would produce. It is also exactly
  where nobody checks, because a golden is compared against itself.
- **"Both sides are additive" is true about intent and false about syntax.**
  Resolving two additive conflicts mechanically by keeping both sides produced a
  file that did not parse: two loop bodies whose closing braces collided. `gofmt`
  caught it. Two independent statements would have compiled and been wrong.
- **A receipt's counts must be compared against what they describe.** The wave
  receipts carried hand-maintained `status_counts` constants, and one had been
  wrong since the first surface moved — 37 `policy_complete` against a ledger
  holding 24 `generated_shadow`, green throughout. A constant answers *does this
  file still say what it said when the test was written*, which stays true
  through exactly the change that makes it wrong. They compare against the ledger
  they pin now, which also removed an edit that had been made once per landing.

## Before you call a surface migrated

- the behaviour inventory taken beforehand is unchanged
- every behaviour is accounted for — compare against the count you took first
- both referees read the surface, including its blocks
- the generated Go is committed and diffable
- the mapping artifacts are regenerated and committed, so `go generate` leaves
  a clean tree
- the state moved to `generated_shadow` carrying its `migration` reason, with
  the five wave receipts re-cut in the same commit
- if it had a `PriorSchema` upgrader, a test asserts the prior type directly
- every rename came from the conversion code, and none was matched on a name
- there is a `//go:generate` line producing every artifact you committed —
  checked from the artifact side, because determinism cannot see an orphan

For a **list resource** or an **action**, three of the above read differently:

- the behaviour inventory is not the check that matters for a list surface,
  because it has no validators; the prose golden and the contract comparison are
- "every rename came from the conversion code" is satisfied vacuously — they
  bind no SDK field, and the rename referee reports them as unchecked by name
  rather than counting them
- an action's custom types ARE its behaviour, and only the inventory can see
  them
