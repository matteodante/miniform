# Third-party notices

Miniform is licensed under MIT. The application also incorporates open-source dependencies and vendored browser assets under compatible licenses.

The release SBOM is the authoritative machine-readable inventory for each artifact. This file summarizes the license families found by `go-licenses` for the source tree and the browser assets shipped in the repository.

## Go dependencies

| License | Dependencies |
| --- | --- |
| MIT | Fiber, GORM, Viper, Testify, Cartridge, Matcha, `go-sqlite3`, `fasthttp`, `brotli`, `compress`, `mapstructure`, `afero`, `cast`, `gotenv`, `lumberjack`, and other transitive modules |
| BSD-3-Clause | Go `x/crypto`, `x/net`, `x/sync`, `x/sys`, `x/term`, `x/text`; `fsnotify`, `google/uuid`, `pflag`, and selected transitive components |
| Apache-2.0 | Portions of `github.com/klauspost/compress` and `github.com/spf13/afero` |
| ISC | `github.com/davecgh/go-spew` |

Exact module versions and source license URLs can be regenerated with:

```bash
make audit-licenses
```

## Browser and build assets

| Component | Version | License | Source |
| --- | --- | --- | --- |
| htmx | 1.9.12 | BSD-2-Clause | <https://github.com/bigskysoftware/htmx> |
| highlight.js | 11.9.0 | BSD-3-Clause | <https://github.com/highlightjs/highlight.js> |
| Tailwind CSS CLI | 3.4.17 | MIT | <https://github.com/tailwindlabs/tailwindcss> |

Copyright remains with each dependency's authors and contributors. Full license texts and copyright notices are available at the linked sources and in downloaded Go modules; release artifacts include this notice and an SPDX SBOM. No third-party trademark rights are granted.
