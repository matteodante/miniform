# Governance

Miniform currently uses a maintainer-led governance model.

## Roles

- **Contributors** report issues, join discussions, review changes, and submit pull requests.
- **Maintainers** triage work, review and merge changes, manage releases, moderate community spaces, and respond to security reports.

The current repository owner is the initial maintainer. Additional maintainers may be invited after sustained, constructive contributions and demonstrated judgment across code, security, documentation, and community work.

## Decisions

Routine decisions happen through issues and pull requests. Significant changes should start as a public proposal describing the problem, alternatives, compatibility impact, migration path, and operational cost. Maintainers seek rough consensus, but retain final responsibility for project direction and safety.

Security response, active abuse, and embargoed fixes may be handled privately until disclosure is safe.

## Project boundaries

Miniform prioritizes a small self-hosted form inbox, a single-process deployment, SQLite, standard HTML forms, and explicit delivery integrations. New infrastructure, plugin systems, hosted services, or broad platform features require strong evidence that they fit this scope.

## Releases and changes

`main` is the integration branch. Versioned releases follow [Semantic Versioning](https://semver.org/) and the process in [docs/releasing.md](docs/releasing.md). User-visible changes are recorded in [CHANGELOG.md](CHANGELOG.md).

## Funding

The project has no funding or sponsorship channel at this time, so no `FUNDING.yml` is published. If that changes, destinations and the effect of funding on governance will be documented here before enabling GitHub Sponsors links.

## Amendments

Governance changes use the same public proposal and pull request process as other significant project changes.
