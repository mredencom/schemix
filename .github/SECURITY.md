# Security Policy

## Supported Versions

schemix is currently pre-1.0 (`v0.x`). Only the latest published release
on the [releases page](https://github.com/mredencom/schemix/releases) is
supported with security fixes. There is no commitment to backport fixes
to older `v0.x` releases.

| Version | Supported          |
| ------- | ------------------- |
| Latest `v0.x` release | ✅ |
| Older releases        | ❌ |

Once schemix reaches `v1.0`, this policy will be updated to define a
supported version window.

## Reporting a Vulnerability

If you discover a security vulnerability in schemix, please **do not**
open a public GitHub issue.

Instead, report it privately using one of the following methods:

1. **GitHub Security Advisories** (preferred): open a
   [private security advisory](https://github.com/mredencom/schemix/security/advisories/new)
   on this repository. This is the fastest way to reach the maintainers
   and allows coordinated disclosure.
2. If you do not have access to GitHub Security Advisories, open a
   regular issue with minimal detail (e.g. "Security issue — see email")
   and state that you will follow up privately; a maintainer will reach
   out to establish a private channel.

Please include as much of the following as possible:

- A description of the vulnerability and its potential impact
- Steps to reproduce, or a minimal proof-of-concept schema/input
- The schemix version (or commit hash) affected
- Whether the issue is in schemix itself, or in an upstream dependency
  (`cuelang.org/go`, `github.com/warpstreamlabs/bento`)

## Scope

schemix is a schema validation and transformation library. Security-relevant
areas include (non-exhaustive):

- **Validation bypass**: any input that should be rejected by a schema but
  is instead accepted (`Result.Valid == true`), or vice versa
- **Panics on untrusted input**: schemix must not panic when processing
  attacker-controlled schemas or data — a crash is a denial-of-service risk
  for any service embedding this library
- **ReDoS (regular expression denial of service)**: `=~"pattern"` constraints
  compile user-supplied regex; a pathological pattern that causes catastrophic
  backtracking is in scope
- **Resource exhaustion**: unbounded memory/CPU usage from adversarial schemas
  or deeply nested/oversized input data
- **`@blob()` expression evaluation**: unexpected side effects, injection, or
  sandbox escape through Bloblang expressions

Issues in the library's *use* by a downstream application (e.g. failing to
validate untrusted data before trusting it) are the responsibility of that
application, not schemix.

## Response Process

- We aim to acknowledge new reports within **5 business days**.
- We will work with you to understand and confirm the issue, and agree on
  a disclosure timeline before any public details are published.
- Once a fix is available, it will be released and credited (unless you
  prefer to remain anonymous) in the release notes / `CHANGELOG.md`.

## Out of Scope

- Vulnerabilities in `cuelang.org/go` or `github.com/warpstreamlabs/bento`
  themselves should be reported to those projects directly, not here
  (though we appreciate a heads-up if it affects schemix's usage of them).
- Issues that require the attacker to already control the schema
  *and* have arbitrary code execution privileges in the host process
  (schemix does not sandbox `@blob()` custom functions/methods registered
  via `WithFunction`/`WithMethod` — those are provided by the library's
  own caller, not by an external attacker, unless your application exposes
  schema compilation to untrusted users, which is itself a design risk
  worth flagging in your report).
