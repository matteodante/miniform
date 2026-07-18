# Roadmap

The roadmap communicates direction, not delivery dates. Priorities may change based on security, maintenance cost, and community feedback.

## Implemented foundations

- Harden first-run credentials and production configuration
- Stabilize installation, upgrades, backups, and restore workflows
- Establish reproducible CI, release artifacts, OCI images, SBOMs, and provenance
- Protect single-process SQLite, background delivery, upload recovery, and graceful shutdown lifecycles
- Cover core operator, native submission, error, and logout flows in sequential browser tests

## Now: first public release verification

- Complete the final accessibility, security, and privacy review of core form flows
- Validate a clean clone, OCI lifecycle, persistence, backup, restore, and upgrade on supported macOS and Linux environments
- Freeze and publish the first versioned compatibility and migration policy
- Verify repository protections, the first signed release tag, GHCR visibility, provenance, checksums, SBOMs, and installation instructions end to end

## Next: operational confidence

- Improve delivery observability and retry diagnostics
- Add import/export and automated full-storage disaster-recovery verification
- Expand browser and concurrency regression coverage
- Document reverse-proxy patterns and resource sizing
- Establish performance baselines for common SQLite workloads
- Move integration-profile ownership behind explicit integration APIs instead of querying integration tables from the forms domain
- Separate browser origin policy from server-to-server authentication, including a migration path away from reusable capabilities in logged URLs
- Decide whether generated form HTML is durable product state or derived export content before extending form templates

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
