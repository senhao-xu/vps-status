# Built-in Appearance

## 1. Scope / Trigger

Use when changing site themes, display configuration or the unauthenticated
appearance bootstrap. Themes are bundled React/CSS presentation; they do not
load or execute uploaded code. Emerald and Classic support light/dark modes.

## 2. Signatures

```go
type Appearance struct { DefaultTheme string }
func (*Store) AppearanceConfig(context.Context) (Appearance, error)
func (*Store) UpdateAppearance(context.Context, Appearance) error
```

```text
GET /api/v1/public/appearance
GET /api/v1/admin/appearance
PUT /api/v1/admin/appearance
```

Control schema v8 owns singleton `appearance(singleton_id=1, default_theme)`.
The SQL CHECK permits only `emerald` and `classic`; initial value is `emerald`.

## 3. Contracts

- All appearance responses contain exactly `{ "default_theme": "emerald" | "classic" }`.
- Administrator PUT accepts exactly the same shape, requires a valid session
  and CSRF, and validates the enum at the storage boundary.
- Public GET is available on login and disabled public pages. Its explicit DTO
  is also the administrator presentation projection; it contains no node data,
  secrets, file locations or theme source code.
- Persistence belongs only to control.db. Telemetry and node configuration
  are unaffected by appearance writes.
- The browser decodes this payload at `web/src/api/contracts.ts`. ThemeProvider
  owns the site value and browser override, separate from the light/dark mode.
- Migration backups precede mutation. Older Control versions reject schema v8;
  switching to Classic is a visual rollback, not a database downgrade.

## 4. Validation & Error Matrix

| Condition | Result |
|---|---|
| Unknown, blank or non-string theme | `400 invalid_input` |
| Unknown JSON fields or trailing JSON | Existing strict JSON rejection |
| Administrator GET/PUT without session | `401 unauthorized` |
| PUT without matching CSRF | `403 csrf_invalid` |
| Storage failure | `500 internal_error`, no SQL or filesystem details |
| Browser appearance read fails | Emerald fallback; page remains usable |

## 5. Good / Base / Bad Cases

- Good: administrator saves Classic and a fresh browser follows Classic after
  Control restarts, while a browser previewing Emerald keeps its local override.
- Base: new schema defaults to Emerald, regardless of public-page enablement.
- Bad: treating the automatically loaded site default as a personal preference
  prevents that browser from following future administrator changes.

## 6. Tests Required

- Persistence across close/reopen and default Emerald on a fresh database.
- Invalid theme leaves the saved value unchanged.
- v7-to-v8 backup is mode 0600 and does not contain the appearance table.
- Existing older migration tests and future-version rejection remain valid.
- Public DTO exact keys, session/CSRF enforcement and unknown-field rejection.
- Browser overrides, follow-site reset and existing light/dark v1 compatibility.

## 7. Wrong vs Correct

Wrong: expose a general settings object or upload a theme script through this API.

Correct:

```go
writeJSON(w, http.StatusOK, appearanceDTO{DefaultTheme: appearance.DefaultTheme})
```

Reference implementation: `internal/control/appearance_api.go`,
`internal/storage/control/appearance.go`, `migrations/control/008_appearance.sql`.
