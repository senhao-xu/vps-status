# Administrator Password Changes

## 1. Scope / Trigger

Apply when changing administrator credentials, session issuance, or the
Security screen's password form. Password changes invalidate prior
authentication without changing MFA or node credentials.

## 2. Signatures

```text
POST /api/v1/admin/password
Content-Type: application/json
Cookie: vps_session=…
X-CSRF-Token: …
{"current_password":"…","new_password":"…"}
```

Both fields must exist and be strings, including an explicit empty string.
Success is `204 No Content` with expired session and CSRF cookies.

## 3. Contracts

- Require authentication and CSRF before verifying passwords. Rate-limit with
  the existing login limiter under `password:<adminID>`, independently of the
  login client key, and return `Retry-After` on exhaustion.
- Verify the current Argon2id digest; hash the new password with existing
  parameters outside SQL transactions. Preserve the existing lack of password
  length restrictions and JSON body cap.
- Atomically match the verified old digest, MFA state, and active current
  session, update the digest, and revoke every administrator session. Reject
  stale requests without changing credentials or sessions.
- Session issuance compares both the password digest and MFA state used for
  authentication under the write lock. A login verified before a password
  change cannot create an active session after that change commits.
- Retain MFA secrets, confirmation state, recovery codes and node credentials.
  Reuse schema v8; no migration or new persistent credential field is needed.
- Never log, store as UI preferences, or return passwords or digests. Browser
  success unmounts private streams and renders a localized login notice.
  Existing streams in other browsers stop on their normal session recheck.

## 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Missing/revoked/expired session | `401 unauthorized` |
| Missing/incorrect CSRF | `403 csrf_invalid` |
| Missing/null/non-string password, unknown field, malformed JSON | `400` input/JSON error; no write |
| Wrong current password | `400 current_password_invalid`; session retained |
| Password attempts exceed bound | `429 rate_limited`, `Retry-After`; no write |
| Verified digest/MFA/current session no longer valid | `409 auth_state_changed`; transaction unchanged |
| Storage failure | Stable `500 internal_error`; transaction rolls back |
| Empty, short or long explicit password | Accepted with existing body limit |
| Successful update | `204`, cookies cleared; all old sessions rejected |

## 5. Good / Base / Bad Cases

- Good: a concurrent login either has its session revoked by the change, or
  has its stale authentication snapshot rejected when issuing a session.
- Base: the Security form verifies matching confirmation locally, submits
  once, then returns to login. New-password login still requires existing MFA.
- Bad: updating the hash and revoking sessions in separate autocommit writes,
  or issuing a session using a password snapshot that SQL never validates.

## 6. Tests Required

- HTTP authentication, CSRF, malformed fields, wrong password, limiter,
  cookies, all-session revocation, old-password rejection/new-password success.
- Storage stale digest, revoked/expired current session, concurrent changes,
  transaction rollback, and stale session issuance after password change.
- MFA/recovery material unchanged and new login still requires MFA. Cover
  empty/short/long passwords to prevent rules drifting from setup.
- Browser mismatch sends no mutation; duplicate submits blocked; incorrect
  current password retains the form; success unmounts authenticated UI and
  shows a localized login notice. Check 320px and both built-in themes.

## 7. Wrong vs Correct

Wrong: verify a password, update its digest, then revoke sessions without
checking if the verified digest/session changed in the meantime.

Correct: compare the authentication snapshot and active current session inside
the transaction updating the digest and revoking sessions; also compare the
snapshot when issuing a concurrent login session.
