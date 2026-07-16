# Security Policy

## Supported versions

Until Miniform reaches 1.0, only the latest published release receives security fixes. After 1.0, this table will be updated with the supported release lines.

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Older releases | No |
| Unreleased `main` | Best effort |

## Report a vulnerability

Do not open a public issue, discussion, or pull request containing vulnerability details.

Use GitHub's private vulnerability reporting at:

<https://github.com/matteodante/miniform/security/advisories/new>

Include the affected version, impact, reproduction steps, proof of concept if available, and any suggested mitigation. Remove real credentials and personal data from the report.

The maintainers aim to acknowledge a report within 3 business days, provide an initial assessment within 7 business days, and coordinate disclosure after a fix is available. These are targets, not service-level guarantees.

If private vulnerability reporting is temporarily unavailable, open a public issue asking for a private security contact without including sensitive details.

## Disclosure and credit

Please allow reasonable time to investigate and release a fix before public disclosure. Reporters will be credited in the advisory and release notes unless they request anonymity.

## Security scope

Security reports may cover the Go application, embedded frontend, installer, OCI image, release artifacts, or build pipeline. General hardening suggestions without an exploitable impact belong in a normal issue.
