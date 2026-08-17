# What the shell pipeline emitted, before it was replaced

These four receipts were produced by the **shell** implementation on pipeline
232, tree `b8f5c1cd`. They exist for one purpose: to be the thing the Go
rewrite is compared against.

## Why they are committed rather than re-derived

**The producers are being deleted.** Once `catalog-build-schema.sh`,
`catalog-controller-differential.sh`, `catalog-unit-differential.sh` and
`m1-dns-compiler.sh` are gone, no tree can produce these again — and CI logs
rotate, so the only other copy has a shelf life. A reference that cannot be
regenerated has to be committed or it is not a reference.

## The acceptance criterion

Only one field in these receipts varies between two runs of the same
implementation on the same tree: `source_commit`. Everything else is either
content-derived or fixed. So the test is exact rather than approximate:

> Run the shell implementation and the Go implementation **on one tree that
> carries both**, and diff the receipts. Fields other than `source_commit`
> must match byte for byte.

**That comparison has to happen before the shell is deleted.** It is the only
window in which both implementations exist, which is why each lane lands its
Go beside the shell first and cuts over second.

A receipt that differs is not automatically a regression — the Go version may
be *more* correct, and two defects found during this migration prove that a
faithful port can legitimately change output: a missing baseline manifest used
to fail soft and record a promotable run, and blocker order used to be
unsorted. **Where the Go output differs, the difference gets explained in the
commit that causes it, or it is a bug.**

## Delete these when

The last shell script under `.woodpecker/scripts/` is gone and the comparison
above has been made and recorded. **If this directory outlives the shell, the
comparison never happened.**

| file | gate | produced by |
|---|---|---|
| `catalog-build-schema.json` | `catalog-build-schema` | `catalog-build-schema.sh` |
| `catalog-controller-differential.json` | `catalog controller differential` | `catalog-controller-differential.sh` |
| `catalog-unit-differential.json` | `catalog-unit-http-differential` | `catalog-unit-differential.sh` |
| `m1-dns-compiler.json` | *(none — 28 keys)* | `m1-dns-compiler.sh` |

The controller differential receipt records `released: accepted_limitation`,
`candidate: pass`, `evidence_gap_count: 6` against a ceiling of 6, with empty
`failed`, `unexpected_failures` and `missing`. **That is a passing campaign,
and it is what the Go pipeline has to keep producing.**
