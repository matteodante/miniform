# Release preparation — v0.3.2

Prepared on 2026-10-22 from `bdad22af5796c706ca04beacc4861dd23a6f368c` on `main`. This release includes the request-parsing and credential-revocation fixes from #44, plus compatible dependency and build updates.

## Updates

| Component | Previous main | Release |
| --- | --- | --- |
| Go requirements, CI, and container builders | 1.27.2 | 1.27.3 |
| Cartridge | 0.15.5 | 0.17.0 |
| Matcha | 0.16.0 | 0.17.0 |
| fasthttp | 1.74.0 | 1.76.0 |
| x/crypto; x/net; x/text | 0.57.0; 0.60.0; 0.42.0 | 0.58.0; 0.61.0; 0.43.0 |
| Playwright | 1.63.0 | 1.64.1 |

Compatible transitive Go and Node dependencies, GitHub Actions commit pins, and Go/Alpine image digests were also refreshed. Registry manifests were checked before updating image pins. Dependency licenses were regenerated; Cartridge no longer requires `petaki/inertia-go`, so its bundled license was removed.

Cartridge 0.17.0 retains the Fiber-based API and passes Miniform's tests without application changes. Its upstream changes add session issuance timestamps, secure production flash cookies, and Inertia asset-version handling. Miniform does not use the flash or Inertia APIs. A separate trial with Cartridge 1.9.0 failed compilation because the framework now uses `net/http`: session calls, the embedded Fiber context, and response-header APIs differ. That migration is outside this patch release. Tailwind 3.4.19 remains current within its existing major version.

Matcha 0.17.0 drops `NET_RAW` for managed application containers. The existing `SkipPull` setting and deployment tests preserve explicit update behavior. fasthttp 1.76.0 includes upstream HTTP framing, multipart parsing, and connection-handling fixes.

## Local validation

Run on macOS ARM64 with Go 1.27.3 and an isolated Node 24.22.1 runtime:

- `make check`: formatting, modules, generated assets, static analysis, shell/workflow policy, and all Go tests passed.
- `make audit`: secret, license, Node vulnerability, reachable Go vulnerability, and Go security checks passed.
- `make test-race`: passed.
- `make test-e2e`: two Node tests and all 30 Chromium acceptance tests passed.
- `make test-stress`: 500 requests at concurrency 16 passed, including uploads, SMTP/webhook delivery, and restart recovery.
- `make container-test`: the final Linux ARM64 image passed health, storage ownership, runtime UID, staging cleanup, and symlink-rejection checks under Docker/Colima.
- `.tools/goreleaser check`: the pinned 2.17.0 binary validated the release configuration.
- `git diff --check`: passed.

The Go vulnerability scan reports no affected imported packages or reachable symbols. It still reports one advisory in the required module graph for unused OpenPGP code, as documented in the previous audit; this is not a finding of vulnerable code reachable by Miniform.

Required Linux CI must additionally pass on the release-preparation commit, including installer integration and both release-binary architectures. The signed tag reruns release validation, scans both OCI architectures, publishes the image, and then publishes the GitHub release. Published-artifact verification belongs in the release notes after that workflow completes.

## Upgrade notes

No database migration is required. HTTP request bodies must be uncompressed (absent `Content-Encoding` or `identity`); other encodings return HTTP 415. Upload field names and filenames are limited to 200 bytes each and count toward `MINIFORM_MAX_PAYLOAD_BYTES`. Existing correctly stored submissions and attachments remain unchanged.
