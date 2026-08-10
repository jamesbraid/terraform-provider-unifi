# Migrating a surface to the generated schema

Surfaces compile from a catalog and a policy one at a time, and the ledger is
the count — this document is the method, not the tally, because a number
written here is wrong by the next landing.

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

## The three traps

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

## At scale, the mechanism holds and review quality does not

`wlan` omits 54 SDK fields against 55 released attributes. The exactly-once
accounting still fails an unclassified new field, so nothing slips through
silently — **the protection against a *missing* omission is intact.** What
degrades is that nobody reads 54 omission lines carefully, so the protection
against a *wrong* one is weaker. That is a real limitation of this method and
it is not solved here.

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
