package kordinate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/krogio/kore/store"
)

// store.go owns kordinate's OWN tables. Everything about a customer that the
// microservices already hold — identity, balances, transactions, documents —
// stays upstream and is never copied here; duplicating it would guarantee two
// disagreeing answers to "what is this customer's status".
//
// What kordinate persists is what no upstream service owns: the back-office
// layer. Agent notes, ticket references, onboarding case state, AI vetting
// verdicts, redaction records, and the audit of who looked at whose data.

// Migrations are append-only. Every table is account-scoped so the same schema
// serves a partner deployment without cross-tenant leakage.
var Migrations = []store.Migration{
	// Onboarding cases: the lifecycle wrapper around a customer's journey from
	// lead to active. The customer record itself lives in the customer service;
	// this tracks the WORK — which state, who owns it, what's outstanding.
	{ID: "0001_onboarding_cases", SQL: `CREATE TABLE IF NOT EXISTS kdn_onboarding_cases (
		id            BIGINT AUTO_INCREMENT PRIMARY KEY,
		account       VARCHAR(64)  NOT NULL,
		mm_guid       VARCHAR(64)  NOT NULL,
		msisdn        VARCHAR(32)  NOT NULL DEFAULT '',
		display_name  VARCHAR(200) NOT NULL DEFAULT '',
		state         VARCHAR(48)  NOT NULL,
		assignee      VARCHAR(320) NOT NULL DEFAULT '',
		priority      TINYINT      NOT NULL DEFAULT 2,   -- 1 high, 2 normal, 3 low
		sla_due_at    DATETIME     NULL,
		opened_at     DATETIME     NOT NULL,
		closed_at     DATETIME     NULL,
		close_reason  VARCHAR(255) NOT NULL DEFAULT '',
		created_by    VARCHAR(320) NOT NULL,
		updated_at    DATETIME     NOT NULL,
		UNIQUE KEY uq_acct_guid_open (account, mm_guid, closed_at),
		KEY idx_acct_state (account, state),
		KEY idx_assignee (account, assignee),
		KEY idx_sla (account, sla_due_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},

	// Every state transition, append-only. An onboarding decision is a
	// regulated act: who moved this customer to APPROVED, when, and why has to
	// be reconstructable years later.
	{ID: "0002_onboarding_transitions", SQL: `CREATE TABLE IF NOT EXISTS kdn_onboarding_transitions (
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,
		account    VARCHAR(64)  NOT NULL,
		case_id    BIGINT       NOT NULL,
		from_state VARCHAR(48)  NOT NULL DEFAULT '',
		to_state   VARCHAR(48)  NOT NULL,
		actor      VARCHAR(320) NOT NULL,
		note       TEXT         NULL,
		at         DATETIME     NOT NULL,
		KEY idx_case (case_id, at),
		KEY idx_acct_at (account, at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},

	// Agent notes/comments against a customer, carried over from claire-admin's
	// customer_chat. Kept local because it's internal commentary, not customer
	// master data.
	{ID: "0003_notes", SQL: `CREATE TABLE IF NOT EXISTS kdn_notes (
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,
		account    VARCHAR(64)  NOT NULL,
		mm_guid    VARCHAR(64)  NOT NULL,
		subject    VARCHAR(32)  NOT NULL DEFAULT 'customer', -- customer | transaction | document | case
		subject_id VARCHAR(128) NOT NULL DEFAULT '',
		body       TEXT         NOT NULL,
		sentiment  VARCHAR(32)  NOT NULL DEFAULT '',
		ticket_ref VARCHAR(64)  NOT NULL DEFAULT '',
		author     VARCHAR(320) NOT NULL,
		at         DATETIME     NOT NULL,
		KEY idx_acct_guid (account, mm_guid, at),
		KEY idx_subject (account, subject, subject_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},

	// AI document vetting verdicts. Stored so a decision is auditable and not
	// re-billed on every page view; ALWAYS advisory — the Decision column
	// records what the model said, never what was done.
	{ID: "0004_doc_vettings", SQL: `CREATE TABLE IF NOT EXISTS kdn_doc_vettings (
		id            BIGINT AUTO_INCREMENT PRIMARY KEY,
		account       VARCHAR(64)  NOT NULL,
		mm_guid       VARCHAR(64)  NOT NULL,
		media_id      VARCHAR(128) NOT NULL,
		doc_type      VARCHAR(48)  NOT NULL DEFAULT '',
		verdict       VARCHAR(32)  NOT NULL,            -- pass | concerns | fail | error
		confidence    VARCHAR(16)  NOT NULL DEFAULT '',
		legible       TINYINT(1)   NOT NULL DEFAULT 0,
		type_matches  TINYINT(1)   NOT NULL DEFAULT 0,
		name_matches  TINYINT(1)   NOT NULL DEFAULT 0,
		dob_matches   TINYINT(1)   NOT NULL DEFAULT 0,
		expired       TINYINT(1)   NOT NULL DEFAULT 0,
		extracted     JSON         NULL,                -- fields the model read off the document
		findings      JSON         NULL,                -- list of concerns, human readable
		model         VARCHAR(96)  NOT NULL DEFAULT '',
		requested_by  VARCHAR(320) NOT NULL,
		at            DATETIME     NOT NULL,
		KEY idx_acct_media (account, media_id, at),
		KEY idx_acct_guid (account, mm_guid, at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},

	// Redactions applied to a stored document. The redacted derivative is what
	// most roles are served; this records what was covered and by whom, so a
	// later dispute can be answered without re-deriving it.
	{ID: "0005_doc_redactions", SQL: `CREATE TABLE IF NOT EXISTS kdn_doc_redactions (
		id           BIGINT AUTO_INCREMENT PRIMARY KEY,
		account      VARCHAR(64)  NOT NULL,
		mm_guid      VARCHAR(64)  NOT NULL,
		media_id     VARCHAR(128) NOT NULL,
		regions      JSON         NOT NULL,             -- normalised boxes + detected PII kind
		detected     JSON         NULL,
		auto         TINYINT(1)   NOT NULL DEFAULT 1,   -- proposed by detection vs drawn by hand
		applied_by   VARCHAR(320) NOT NULL,
		at           DATETIME     NOT NULL,
		UNIQUE KEY uq_acct_media (account, media_id),
		KEY idx_acct_guid (account, mm_guid)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},

	// Who viewed which customer's data, and whether they saw it unredacted.
	// claire-admin logged a "viewed" event for compliance; this generalises it.
	// This is the table that answers "which agent opened this customer's ID".
	{ID: "0006_access_log", SQL: `CREATE TABLE IF NOT EXISTS kdn_access_log (
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,
		account    VARCHAR(64)  NOT NULL,
		mm_guid    VARCHAR(64)  NOT NULL,
		principal  VARCHAR(320) NOT NULL,
		action     VARCHAR(64)  NOT NULL,               -- view_customer | view_document | reveal_unredacted | export
		target     VARCHAR(128) NOT NULL DEFAULT '',
		reason     VARCHAR(255) NOT NULL DEFAULT '',
		at         DATETIME     NOT NULL,
		KEY idx_acct_guid_at (account, mm_guid, at),
		KEY idx_principal (account, principal, at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},

	// The one permission kore's role and section axes cannot express: who may
	// view an UNREDACTED identity document. Per user, not per group — the
	// population is small and named, and a group grant would silently extend to
	// whoever is added to that group next. Expiring by default because a
	// standing permission to read every customer's ID is an audit finding.
	{ID: "0008_reveal_grants", SQL: `CREATE TABLE IF NOT EXISTS kdn_reveal_grants (
		id         BIGINT AUTO_INCREMENT PRIMARY KEY,
		account    VARCHAR(64)  NOT NULL,
		email      VARCHAR(320) NOT NULL,
		granted_by VARCHAR(320) NOT NULL,
		reason     VARCHAR(500) NOT NULL DEFAULT '',
		granted_at DATETIME     NOT NULL,
		expires_at DATETIME     NULL,
		UNIQUE KEY uq_acct_email (account, email)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},

	// Reference data carried over from claire-admin: the sentiment tags agents
	// pick when noting a call, and the ticket-system prefix. Small enough to be
	// settings, but agents edit the list, so it's a table.
	{ID: "0007_sentiments", SQL: `CREATE TABLE IF NOT EXISTS kdn_sentiments (
		id      BIGINT AUTO_INCREMENT PRIMARY KEY,
		account VARCHAR(64)  NOT NULL,
		label   VARCHAR(64)  NOT NULL,
		icon    VARCHAR(32)  NOT NULL DEFAULT '',
		sort    INT          NOT NULL DEFAULT 0,
		UNIQUE KEY uq_acct_label (account, label)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`},
}

// Store is kordinate's local persistence.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB) *Store { return &Store{db: db, now: time.Now} }

// ---------- Onboarding cases ----------

// Case is an onboarding case: the unit of work an activations/AML agent owns.
type Case struct {
	ID          int64
	MMGuid      string
	MSISDN      string
	DisplayName string
	State       State
	Assignee    string
	Priority    int
	SLADueAt    *time.Time
	OpenedAt    time.Time
	ClosedAt    *time.Time
	CloseReason string
	CreatedBy   string
	UpdatedAt   time.Time
}

// Overdue reports whether an open case has blown its SLA — the flag the work
// queue sorts on.
func (c *Case) Overdue(now time.Time) bool {
	return c.ClosedAt == nil && c.SLADueAt != nil && now.After(*c.SLADueAt)
}

// OpenCase starts a case for a customer, or returns the existing open one.
// Re-opening is deliberately not automatic: a closed case is a completed
// regulated decision, and a new one should be a new record.
func (s *Store) OpenCase(ctx context.Context, account string, c Case) (*Case, error) {
	if existing, err := s.OpenCaseFor(ctx, account, c.MMGuid); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	now := s.now().UTC()
	if c.State == "" {
		c.State = StateLead
	}
	if c.Priority == 0 {
		c.Priority = 2
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO kdn_onboarding_cases
		(account, mm_guid, msisdn, display_name, state, assignee, priority, sla_due_at, opened_at, created_by, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		account, c.MMGuid, c.MSISDN, c.DisplayName, string(c.State), c.Assignee,
		c.Priority, c.SLADueAt, now, c.CreatedBy, now)
	if err != nil {
		return nil, fmt.Errorf("open case: %w", err)
	}
	id, _ := res.LastInsertId()
	c.ID, c.OpenedAt, c.UpdatedAt = id, now, now

	if err := s.recordTransition(ctx, account, id, "", c.State, c.CreatedBy, "case opened"); err != nil {
		return nil, err
	}
	return &c, nil
}

// OpenCaseFor returns the customer's open case, or nil when there isn't one.
func (s *Store) OpenCaseFor(ctx context.Context, account, mmGuid string) (*Case, error) {
	row := s.db.QueryRowContext(ctx, caseSelect+` WHERE account=? AND mm_guid=? AND closed_at IS NULL`, account, mmGuid)
	c, err := scanCase(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open case for %s: %w", mmGuid, err)
	}
	return c, nil
}

// GetCase loads one case by id, scoped to the account.
func (s *Store) GetCase(ctx context.Context, account string, id int64) (*Case, error) {
	row := s.db.QueryRowContext(ctx, caseSelect+` WHERE account=? AND id=?`, account, id)
	c, err := scanCase(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get case %d: %w", id, err)
	}
	return c, nil
}

// CaseQuery filters the work queue.
type CaseQuery struct {
	States   []State
	Assignee string
	// Unassigned restricts to cases nobody owns — the queue agents pull from.
	Unassigned bool
	// OverdueOnly restricts to SLA breaches.
	OverdueOnly   bool
	IncludeClosed bool
	Limit         int
}

// ListCases returns cases matching the query, most urgent first: overdue
// before on-time, then by priority, then oldest first — an agent working top
// down handles the most at-risk case next.
func (s *Store) ListCases(ctx context.Context, account string, q CaseQuery) ([]Case, error) {
	sqlStr := caseSelect + ` WHERE account=?`
	args := []any{account}

	if !q.IncludeClosed {
		sqlStr += ` AND closed_at IS NULL`
	}
	if len(q.States) > 0 {
		sqlStr += ` AND state IN (` + placeholders(len(q.States)) + `)`
		for _, st := range q.States {
			args = append(args, string(st))
		}
	}
	if q.Unassigned {
		sqlStr += ` AND assignee=''`
	} else if q.Assignee != "" {
		sqlStr += ` AND assignee=?`
		args = append(args, q.Assignee)
	}
	if q.OverdueOnly {
		sqlStr += ` AND sla_due_at IS NOT NULL AND sla_due_at < ?`
		args = append(args, s.now().UTC())
	}
	sqlStr += ` ORDER BY (sla_due_at IS NOT NULL AND sla_due_at < NOW()) DESC, priority ASC, opened_at ASC`
	if q.Limit > 0 {
		sqlStr += ` LIMIT ?`
		args = append(args, q.Limit)
	}

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("list cases: %w", err)
	}
	defer rows.Close()

	var out []Case
	for rows.Next() {
		c, err := scanCase(rows)
		if err != nil {
			return nil, fmt.Errorf("list cases: %w", err)
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// MoveCase applies a state transition and records it. The legality of the
// transition is decided by the lifecycle (see onboarding.go), not here.
func (s *Store) MoveCase(ctx context.Context, account string, id int64, to State, actor, note string) error {
	cur, err := s.GetCase(ctx, account, id)
	if err != nil {
		return err
	}
	if cur == nil {
		return fmt.Errorf("case %d not found", id)
	}

	now := s.now().UTC()
	closed := terminal(to)
	// A terminal state closes the case; SLA no longer applies once there's
	// nothing outstanding to breach.
	if closed {
		_, err = s.db.ExecContext(ctx, `UPDATE kdn_onboarding_cases
			SET state=?, closed_at=?, close_reason=?, sla_due_at=NULL, updated_at=? WHERE account=? AND id=?`,
			string(to), now, note, now, account, id)
	} else {
		due := slaDue(to, now)
		_, err = s.db.ExecContext(ctx, `UPDATE kdn_onboarding_cases
			SET state=?, sla_due_at=?, updated_at=? WHERE account=? AND id=?`,
			string(to), due, now, account, id)
	}
	if err != nil {
		return fmt.Errorf("move case %d to %s: %w", id, to, err)
	}
	return s.recordTransition(ctx, account, id, cur.State, to, actor, note)
}

// AssignCase sets (or clears, with an empty assignee) the case owner.
func (s *Store) AssignCase(ctx context.Context, account string, id int64, assignee, actor string) error {
	now := s.now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE kdn_onboarding_cases
		SET assignee=?, updated_at=? WHERE account=? AND id=?`, assignee, now, account, id); err != nil {
		return fmt.Errorf("assign case %d: %w", id, err)
	}
	note := "unassigned"
	if assignee != "" {
		note = "assigned to " + assignee
	}
	cur, err := s.GetCase(ctx, account, id)
	if err != nil || cur == nil {
		return err
	}
	return s.recordTransition(ctx, account, id, cur.State, cur.State, actor, note)
}

// Transition is one recorded state change.
type Transition struct {
	FromState State
	ToState   State
	Actor     string
	Note      string
	At        time.Time
}

// CaseHistory returns a case's transitions, oldest first.
func (s *Store) CaseHistory(ctx context.Context, account string, caseID int64) ([]Transition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT from_state, to_state, actor, COALESCE(note,''), at
		FROM kdn_onboarding_transitions WHERE account=? AND case_id=? ORDER BY at ASC, id ASC`, account, caseID)
	if err != nil {
		return nil, fmt.Errorf("case history: %w", err)
	}
	defer rows.Close()

	var out []Transition
	for rows.Next() {
		var t Transition
		var from, to string
		if err := rows.Scan(&from, &to, &t.Actor, &t.Note, &t.At); err != nil {
			return nil, fmt.Errorf("case history: %w", err)
		}
		t.FromState, t.ToState = State(from), State(to)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) recordTransition(ctx context.Context, account string, caseID int64, from, to State, actor, note string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO kdn_onboarding_transitions
		(account, case_id, from_state, to_state, actor, note, at) VALUES (?,?,?,?,?,?,?)`,
		account, caseID, string(from), string(to), actor, note, s.now().UTC())
	if err != nil {
		return fmt.Errorf("record transition: %w", err)
	}
	return nil
}

// CaseCounts is the per-state tally behind the queue's summary tiles.
func (s *Store) CaseCounts(ctx context.Context, account string) (map[State]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM kdn_onboarding_cases
		WHERE account=? AND closed_at IS NULL GROUP BY state`, account)
	if err != nil {
		return nil, fmt.Errorf("case counts: %w", err)
	}
	defer rows.Close()

	out := map[State]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, fmt.Errorf("case counts: %w", err)
		}
		out[State(st)] = n
	}
	return out, rows.Err()
}

// ---------- Notes ----------

// Note is an agent's internal commentary.
type Note struct {
	ID        int64
	MMGuid    string
	Subject   string
	SubjectID string
	Body      string
	Sentiment string
	TicketRef string
	Author    string
	At        time.Time
}

// AddNote records a note against a customer (optionally scoped to a
// transaction, document or case).
func (s *Store) AddNote(ctx context.Context, account string, n Note) (int64, error) {
	if n.Subject == "" {
		n.Subject = "customer"
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO kdn_notes
		(account, mm_guid, subject, subject_id, body, sentiment, ticket_ref, author, at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		account, n.MMGuid, n.Subject, n.SubjectID, n.Body, n.Sentiment, n.TicketRef, n.Author, s.now().UTC())
	if err != nil {
		return 0, fmt.Errorf("add note: %w", err)
	}
	return res.LastInsertId()
}

// Notes returns a customer's notes, newest first.
func (s *Store) Notes(ctx context.Context, account, mmGuid string, limit int) ([]Note, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, mm_guid, subject, subject_id, body, sentiment, ticket_ref, author, at
		FROM kdn_notes WHERE account=? AND mm_guid=? ORDER BY at DESC, id DESC LIMIT ?`, account, mmGuid, limit)
	if err != nil {
		return nil, fmt.Errorf("notes: %w", err)
	}
	defer rows.Close()

	var out []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.MMGuid, &n.Subject, &n.SubjectID, &n.Body,
			&n.Sentiment, &n.TicketRef, &n.Author, &n.At); err != nil {
			return nil, fmt.Errorf("notes: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---------- Access log ----------

// Access-log actions.
const (
	AccessViewCustomer     = "view_customer"
	AccessViewDocument     = "view_document"
	AccessRevealUnredacted = "reveal_unredacted"
	AccessExport           = "export"
)

// LogAccess records that a principal touched a customer's data. Best-effort by
// design at the call sites: failing to write an access log must not block an
// agent mid-call, but it is logged loudly so a silent gap can't develop.
func (s *Store) LogAccess(ctx context.Context, account, mmGuid, principal, action, target, reason string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO kdn_access_log
		(account, mm_guid, principal, action, target, reason, at) VALUES (?,?,?,?,?,?,?)`,
		account, mmGuid, principal, action, target, reason, s.now().UTC())
	if err != nil {
		return fmt.Errorf("log access: %w", err)
	}
	return nil
}

// AccessEntry is one access-log row.
type AccessEntry struct {
	MMGuid    string
	Principal string
	Action    string
	Target    string
	Reason    string
	At        time.Time
}

// AccessLog returns recent access entries for a customer.
func (s *Store) AccessLog(ctx context.Context, account, mmGuid string, limit int) ([]AccessEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT mm_guid, principal, action, target, reason, at
		FROM kdn_access_log WHERE account=? AND mm_guid=? ORDER BY at DESC, id DESC LIMIT ?`,
		account, mmGuid, limit)
	if err != nil {
		return nil, fmt.Errorf("access log: %w", err)
	}
	defer rows.Close()

	var out []AccessEntry
	for rows.Next() {
		var e AccessEntry
		if err := rows.Scan(&e.MMGuid, &e.Principal, &e.Action, &e.Target, &e.Reason, &e.At); err != nil {
			return nil, fmt.Errorf("access log: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------- helpers ----------

const caseSelect = `SELECT id, mm_guid, msisdn, display_name, state, assignee, priority,
	sla_due_at, opened_at, closed_at, close_reason, created_by, updated_at FROM kdn_onboarding_cases`

// rowScanner covers both *sql.Row and *sql.Rows so one scan helper serves the
// single-row and list paths.
type rowScanner interface{ Scan(dest ...any) error }

func scanCase(r rowScanner) (*Case, error) {
	var c Case
	var state string
	if err := r.Scan(&c.ID, &c.MMGuid, &c.MSISDN, &c.DisplayName, &state, &c.Assignee,
		&c.Priority, &c.SLADueAt, &c.OpenedAt, &c.ClosedAt, &c.CloseReason,
		&c.CreatedBy, &c.UpdatedAt); err != nil {
		return nil, err
	}
	c.State = State(state)
	return &c, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	s := "?"
	for i := 1; i < n; i++ {
		s += ",?"
	}
	return s
}

// ---------- Vetting and redaction records ----------

// SaveVetting stores an AI vetting verdict. Append-only: a re-run records a new
// row rather than overwriting, so a change of verdict between passes is visible
// rather than silently replacing what an earlier reviewer acted on.
func (s *Store) SaveVetting(ctx context.Context, account, mmGuid string, v Vetting) error {
	extracted, err := json.Marshal(v.Extracted)
	if err != nil {
		return fmt.Errorf("save vetting: encode extracted: %w", err)
	}
	findings, err := json.Marshal(v.Findings)
	if err != nil {
		return fmt.Errorf("save vetting: encode findings: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO kdn_doc_vettings
		(account, mm_guid, media_id, doc_type, verdict, confidence, legible,
		 type_matches, name_matches, dob_matches, expired, extracted, findings,
		 model, requested_by, at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		account, mmGuid, v.MediaID, v.DocType, string(v.Verdict), v.Confidence,
		v.Legible, v.TypeMatches, v.NameMatches, v.DOBMatches, v.Expired,
		extracted, findings, v.Model, v.RequestedBy, s.now().UTC())
	if err != nil {
		return fmt.Errorf("save vetting: %w", err)
	}
	return nil
}

// LatestVetting returns the most recent vetting verdict for a document, or nil
// when it has never been vetted.
func (s *Store) LatestVetting(ctx context.Context, account, mediaID string) (*Vetting, error) {
	row := s.db.QueryRowContext(ctx, `SELECT media_id, doc_type, verdict, confidence,
		legible, type_matches, name_matches, dob_matches, expired,
		COALESCE(extracted,'{}'), COALESCE(findings,'[]'), model, requested_by, at
		FROM kdn_doc_vettings WHERE account=? AND media_id=? ORDER BY at DESC, id DESC LIMIT 1`,
		account, mediaID)

	var v Vetting
	var verdict string
	var extracted, findings []byte
	if err := row.Scan(&v.MediaID, &v.DocType, &verdict, &v.Confidence,
		&v.Legible, &v.TypeMatches, &v.NameMatches, &v.DOBMatches, &v.Expired,
		&extracted, &findings, &v.Model, &v.RequestedBy, &v.At); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("latest vetting: %w", err)
	}
	v.Verdict = VetVerdict(verdict)
	if err := json.Unmarshal(extracted, &v.Extracted); err != nil {
		v.Extracted = map[string]string{}
	}
	if err := json.Unmarshal(findings, &v.Findings); err != nil {
		v.Findings = nil
	}
	return &v, nil
}

// SaveRedaction stores (or replaces) the redaction applied to a document. Unlike
// vettings this IS a replace: there is one current redaction per document, and
// it's what gets served.
func (s *Store) SaveRedaction(ctx context.Context, account, mmGuid string, red Redaction) error {
	regions, err := json.Marshal(red.Regions)
	if err != nil {
		return fmt.Errorf("save redaction: encode regions: %w", err)
	}
	detected, err := json.Marshal(red.Detected)
	if err != nil {
		return fmt.Errorf("save redaction: encode detected: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO kdn_doc_redactions
		(account, mm_guid, media_id, regions, detected, auto, applied_by, at)
		VALUES (?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE regions=VALUES(regions), detected=VALUES(detected),
		 auto=VALUES(auto), applied_by=VALUES(applied_by), at=VALUES(at)`,
		account, mmGuid, red.MediaID, regions, detected, red.Auto, red.AppliedBy, s.now().UTC())
	if err != nil {
		return fmt.Errorf("save redaction: %w", err)
	}
	return nil
}

// Redaction returns the stored redaction for a document, or nil when none has
// been applied.
func (s *Store) Redaction(ctx context.Context, account, mediaID string) (*Redaction, error) {
	row := s.db.QueryRowContext(ctx, `SELECT media_id, regions, COALESCE(detected,'[]'), auto, applied_by
		FROM kdn_doc_redactions WHERE account=? AND media_id=?`, account, mediaID)

	var red Redaction
	var regions, detected []byte
	if err := row.Scan(&red.MediaID, &regions, &detected, &red.Auto, &red.AppliedBy); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("redaction: %w", err)
	}
	if err := json.Unmarshal(regions, &red.Regions); err != nil {
		return nil, fmt.Errorf("redaction: decode regions: %w", err)
	}
	if err := json.Unmarshal(detected, &red.Detected); err != nil {
		red.Detected = nil
	}
	return &red, nil
}
