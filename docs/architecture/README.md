# UniFi control plane architecture

Read the documents in this order:

1. [Product brief](2026-08-03-unifi-control-plane-product-brief.md) defines
   user problems, promises, and boundaries.
2. [Architecture](2026-08-03-unifi-control-plane-architecture.md) defines
   component ownership, the provider spec compiler, runtime behavior,
   contracts, and invariants.
3. [Decisions](2026-08-03-unifi-control-plane-decisions.md) records the
   alternatives considered and the choices that constrain implementation.
4. [Transition roadmap](2026-08-03-unifi-control-plane-transition-roadmap.md)
   maps the current repositories to the target architecture, with trust gates
   and an order that preserves the shipping provider.
5. [Implementation program](2026-08-03-unifi-control-plane-implementation-program.md)
   turns the design into staged work with exit gates and rollback rules after
   the transition ordering is accepted.
6. [Milestone 0 baseline audit](2026-08-03-m0-baseline-audit.md) records the
   repository evidence behind the accepted starting point and disposes every
   commit on the obsolete `provider-prereqs` side line.

The [research and compatibility annex](2026-08-03-unifi-control-plane-design-review.md)
preserves experimental evidence, repository comparisons, and historical
proposals. It is supporting material, not an authority for product or runtime
behavior.
