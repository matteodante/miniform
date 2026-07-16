# Roadmap

The roadmap communicates direction, not delivery dates. Priorities may change based on security, maintenance cost, and community feedback.

## Now: first public release

- Harden first-run credentials and production configuration
- Stabilize installation, upgrades, backups, and restore workflows
- Establish reproducible CI, release artifacts, OCI images, SBOMs, and provenance
- Complete accessibility, security, and privacy review of core form flows
- Publish a documented compatibility and migration policy

## Next: operational confidence

- Improve delivery observability and retry diagnostics
- Add import/export and disaster-recovery verification
- Expand browser and concurrency regression coverage
- Document reverse-proxy patterns and resource sizing
- Establish performance baselines for common SQLite workloads

## Later: carefully scoped capabilities

- Evaluate additional delivery integrations based on real demand
- Improve operator workflows for high-volume inboxes
- Add stable APIs only where HTML and webhook workflows are insufficient

## Explicitly not planned

- A managed hosted form service
- A distributed service architecture by default
- A general-purpose plugin marketplace
- Features that require sending submission data through Miniform-operated infrastructure

Propose roadmap changes in [GitHub Discussions](https://github.com/matteodante/miniform/discussions) before opening an implementation pull request.
