# kordinate

MamaMoney's **customer operations console** — the replacement for the sunsetting
`claire-admin` Laravel app. A product on the [kore](https://github.com/krogio/kore)
framework and a child of the [Kosmos](https://github.com/krogio/kosmos) product set.

## What it does

Everything claire-admin did — customer search and a single customer view,
compliance documents, transactions, deposits and EFT reconciliation, vouchers,
card operations, the device blocker — plus the four things it never had:

- **An explicit onboarding lifecycle.** Named states, legal transitions,
  per-state document requirements, SLA clocks and an audit trail on every move.
  In claire-admin a customer's readiness was inferred by an agent eyeballing a
  document list, and the work in flight lived in spreadsheets.
- **One stitched customer view.** Every event about a customer — orders, wallet
  and card movements, EFT deposits, documents, device activity, vouchers, agent
  notes, case transitions — merged into one chronological feed with source
  attribution. Previously five screens reconciled by hand.
- **AI document vetting.** A vision model reads an uploaded FICA document and
  reports what it can see: type, legibility, extracted fields, expiry, and
  whether the name and date of birth match the customer record.
- **PII redaction.** Documents are served with redactions **burnt into the
  raster**. Seeing an unredacted original is a separate capability, requires a
  stated reason, and is logged.

## Architecture

```
cmd/kordinate/main.go          kore composition (brand + battery modules + kordinate)
internal/kordinate/
  module.go                    routes, sections, template funcs
  handlers.go                  customer search, 360 view, mutations, onboarding
  handlers_docs.go             document review, vetting, redaction, reveal
  handlers_ops.go              deposits/EFT, vouchers, devices, access log
  store.go                     kordinate's own MariaDB tables
  onboarding.go                the lifecycle state machine
  timeline.go                  cross-service event stitching
  docvet.go                    AI document vetting (advisory)
  redact.go                    burnt-in PII redaction
  roles.go                     the unredacted-document grant + claire-admin role migration
  upstream/                    microservice clients (live + fakes)
```

**kordinate stores almost nothing about customers.** Identity, balances,
transactions and documents stay in the microservices that own them; duplicating
them would guarantee two disagreeing answers to "what is this customer's
status". What it persists is the back-office layer: onboarding cases and their
transitions, agent notes, AI vetting verdicts, redaction records, and the log of
who viewed whose data.

### Upstream services

Every service sits behind an interface with a live HTTP client and a
deterministic fake:

| Service | Env var | Owns |
|---|---|---|
| Customer service | `CUSTOMER_SERVICE_URL` | customer master data, documents |
| Claire (legacy) | `CLAIRE_API_URL` | transaction limits, income reference, risk matrix |
| UML | `UML_URL` | banking, card and wallet balances |
| UOPS | `UOPS_API_URL` | cross-product orders |
| Emma | `EMMA_API` | EFT deposit notifications |
| IDV | `IDV_SERVICE_API_URL` | app login PIN reset |
| Device blocker | `DEVICE_BLOCKER_SERVICE_URL` | device status, linked customers |
| VMS | `VMS_API_URL` | vouchers |

`UPSTREAM_MODE=fake` (the default) serves an in-memory dataset of 12 customers,
39 orders, 16 EFT notifications and matching documents, devices and vouchers —
covering every customer status, an expired passport, a rejected document, a
shared fraud-ring device, a per-product balance failure, and a customer
reachable only by a deprecated MSISDN. **kordinate runs and demos with no VPN
and no credentials.** Anything other than `live` fails safe to fake.

## Authorisation

kore's three axes do almost all the work: **licence → section access → role**. A
route's minimum role gates the mutation; an `access.Section` rule (managed in the
access admin UI, keyed on stable group IDs) gates which groups reach a feature at
all. That is how, for example, sales and marketing are kept off
`/kordinate/documents` — one rule an operator can change, not a constant in Go.

**One permission genuinely doesn't fit that model**, and it is the only thing
`roles.go` exists for: viewing an **unredacted identity document**.

- It can't be a role — admin is a statement about configuration rights, not a
  standing authorisation to read every customer's ID number, address and
  photograph. No role confers it, including admin.
- It can't be a section rule — the redacted and unredacted views are the same
  route.
- So it's a per-user grant: persisted, **expiring by default** (30 days), granted
  with a recorded reason, revocable, and logged on every use. The grant list on
  the access-log screen *is* the answer to "who can see unredacted customer
  documents" — the question claire-admin could only answer by grepping route
  middleware.

A failed redaction serves **nothing** — never the original as a fallback.

claire-admin's eleven flat roles map onto kore groups + a role via
`MapLegacyRole`, so an existing user list migrates without hand-mapping every
account. An unrecognised role maps to the most restrictive combination rather
than erroring: a migration must not grant access it can't explain, nor fail the
whole import over one stale row.

## kore dependency

kordinate's AI document vetting needs **image input on `kore/ai`**
(`ai.Request.Images`), and its templates use kore's `statusBadge`. Both live on
the kore branch `feat/component-library-and-vision` and are not yet on kore
`main`, so with the local `go.work` overlay kordinate only builds when the
sibling kore checkout is on that branch:

```sh
cd ../kore && git checkout feat/component-library-and-vision
```

Once that branch merges, this note can go. (A kore-core refactor making the
kernel AI panel a built-in is in flight on `main` in parallel; the two touch
different files — `ai/`+`web/` versus `kore.go`/`module.go`/`run.go` — so they
coexist and this branch rebases cleanly.)

## Local dev

Runs in the shared Kosmos dev stack on port **3008**, with SSO via kontrol:

```sh
cd ~/Code/kosmos
docker compose up -d
./scripts/dev-sso-setup.sh          # issues the licence + registers the OIDC client
KORDINATE_OIDC_SECRET='…' docker compose up -d kordinate
# → https://localhost:3008
```

Or standalone against the fakes, with no stack at all:

```sh
make run
```

## Known gaps

Carried over honestly rather than hidden:

- **Some upstream paths are inferred.** claire-admin drives card operations
  (block/unblock/reallocate/retry) and deposit assign/refund through **Claire**,
  not the services this interface assigns them to, and its Claire paths need a
  numeric card id and Claire customer id that the current Go signatures don't
  carry. Those clients, plus `VouchersForCustomer` and voucher `Create`, are
  implemented against inferred paths and **must be verified against the real
  APIs before production**. See the per-method comments in `upstream/live_*.go`.
- **No `status_change` timeline events.** No upstream service exposes a
  customer-status history endpoint, so the event kind is declared but not
  emitted — fabricating one from `DateModified` would imply an audit trail that
  doesn't exist.
- **PDFs are not redactable.** Covering a box on a rendered page leaves the text
  layer extractable underneath. The UI says so rather than offering a control
  that produces a false redaction.
- **Default redaction regions are conventional, not detected.** There is no OCR
  here; `DefaultRegions` proposes boxes at usual field positions for standard
  layouts, which the agent drags into place.
- Statements (Gotenberg PDF), recipients, tokens, agent cashout, translations
  and the emoticon/avatar/country reference-data admin screens are **not yet
  ported**.
