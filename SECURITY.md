# Security

This document explains, precisely and without marketing language, what this
service can and cannot access, how your Crowdin token is protected, and what
the actual limitations of that protection are.

## What we store

When you register a project, we store:

- Your Crowdin Project ID, **in plaintext**.
- Your Crowdin Personal Access Token, **encrypted**.
- A randomly generated `public_id` used in your embed URLs.

## Token encryption

Tokens are encrypted at rest using **NaCl secretbox**
(XSalsa20-Poly1305, authenticated encryption). The encryption key
(`MASTER_KEY`) exists only in the server's runtime environment — it is
**never** written to disk in the database, never included in backups, and
never logged.

A copy of the database alone is insufficient to recover any token. The
running application decrypts a token transiently, in memory, only at the
moment it needs to make an authenticated request to Crowdin's API on your
behalf. The decrypted value is not persisted, logged, or returned in any
response.

This is an honest, not an absolute, guarantee: it protects against database
compromise, backup exposure, and casual operator access. It does not protect
against a full compromise of the running server process itself, which could
in principle observe a token during the brief window it is decrypted in
memory. We are not aware of a self-hosted architecture that closes that
specific gap without moving decryption client-side (see "Alternatives" in
the project README for the fully offline CI-based option, which avoids
sending us a token at all).

## The Crowdin Project ID is not encrypted

This is deliberate, not an oversight. The project ID is required unencrypted
to make API calls, and it is not itself a credential — knowing a project ID
grants no access to anything without a valid token.

If your Crowdin project is **public**, this has no practical effect: your
project ID is already visible in Crowdin's own public project URL.

If your Crowdin project is **private**, be aware that our database holds an
unencrypted association between your embed's `public_id` and your private
project's identity. If that distinction matters to you, weigh it before
registering a private project's token with this service.

## What the token can do

We ask you to create your token using Crowdin's **Granular Access** option,
scoped to **one project only**, with exactly three scopes enabled: **Projects**
(Read only), **Translation status** (Read only), and **Reports** (Read and
Write — Crowdin requires Write access to generate a report even though doing
so only reads translation activity and changes nothing in your project). The
exact scopes and levels are shown in the setup guide. This is enforced by
instructions during setup, not by anything on our side — nothing prevents you
from pasting a broader-scoped token, but doing so widens the blast radius of
any compromise (ours or otherwise) beyond what this service actually needs.

With a correctly scoped token, this service can read translation progress
and top-contributor reports for the one project you registered. It cannot
modify translations, invite or remove members, delete files, or access any
other project on your account.

## Revocation

The real kill switch is deleting the Personal Access Token in your Crowdin
account. This takes effect immediately and requires no action from us —
the service simply loses the ability to call Crowdin on your behalf on its
next refresh.

To also have your project's row removed from our database, email
`revoke@crowdin-stats.rewake.org` with your embed URL. We process these manually; there
is no automated self-service deletion in the current version.

## Key rotation

The service does not currently support rotating `MASTER_KEY` without
requiring every registered project to re-onboard (re-paste their token).
The database schema reserves a `key_version` field to make a real rotation
mechanism possible in a future version, but this is not yet implemented.
If you have a specific reason to need rotation support sooner, please open
an issue.

## Data residency

This service is self-hosted on a single EU-based virtual machine. There are
no third-party subprocessors involved in storing or processing your token —
no cloud KMS, no managed database, no external cache provider. The only
external party in the data path is Crowdin's own API, which you have already
chosen to trust by using Crowdin itself.

## Logging

Request logs record method, path, status code, and duration. They never
record request or response bodies. The `/setup` endpoint — the only code
path where your plaintext token ever transits our server — is explicitly
excluded from any logging middleware that could capture it.

## Source code

This service is open source. If you want to verify any of the above rather
than take our word for it, the encryption, request handling, and logging
code are all readable in the repository linked from the project's landing
page.

## Reporting a vulnerability

If you believe you've found a security issue, please email
`security@crowdin-stats.rewake.org` rather than opening a public issue. We'll
acknowledge reports within a reasonable timeframe and credit responsible
disclosures unless you'd prefer otherwise.
