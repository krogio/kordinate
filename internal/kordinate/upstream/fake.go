package upstream

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// fake.go is the in-memory implementation of every upstream interface. One
// shared store backs all eight clients so cross-service effects behave the way
// they do in production: blocking a device suspends the linked customers, and
// approving a document is visible to the next customer fetch.
//
// Mutations persist for the process lifetime, deliberately. A demo where
// approving a document silently does nothing teaches an agent the wrong thing.

// NewFake returns a Set backed by a fresh seeded store.
func NewFake() Set {
	s := newFakeStore()
	return Set{
		Customer: &fakeCustomer{s},
		Claire:   &fakeClaire{s},
		UML:      &fakeUML{s},
		UOPS:     &fakeUOPS{s},
		Emma:     &fakeEmma{s},
		IDV:      &fakeIDV{s},
		Device:   &fakeDevice{s},
		VMS:      &fakeVMS{s},
	}
}

type fakeStore struct {
	mu sync.RWMutex

	customers []Customer // ordered, so pagination is stable
	orders    []Order
	walletTx  []fakeWalletTx
	efts      []EFTNotification
	devices   []Device
	vouchers  []fakeVoucher

	balances    map[string]*Balances
	cloe        map[string]*CloeBalance
	bankStatus  map[string]*BankingStatus
	bankCust    map[string]*BankingCustomer
	bankDetails map[string]*BankingDetails
	eligibility map[string]*BankingEligibility
	risk        map[string]*RiskMatrix
	limits      map[string]*MonthlyLimit
	limitBals   map[string]*LimitBalance

	// pinResets records IDV/card PIN clears so a demo can show the action took.
	pinResets map[string]time.Time
	nextEFTID int64
	seq       int
}

type fakeVoucher struct {
	owner string
	v     Voucher
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		customers:   seedCustomers(),
		orders:      seedOrders(),
		walletTx:    seedWalletTransactions(),
		efts:        seedEFTNotifications(),
		devices:     seedDevices(),
		vouchers:    seedVouchers(),
		balances:    seedBalances(),
		cloe:        seedCloeBalances(),
		bankStatus:  seedBankingStatus(),
		bankCust:    seedBankingCustomers(),
		bankDetails: seedBankingDetails(),
		eligibility: seedEligibility(),
		risk:        seedRiskMatrix(),
		limits:      seedMonthlyLimits(),
		limitBals:   seedLimitBalances(),
		pinResets:   map[string]time.Time{},
		nextEFTID:   56000,
		seq:         1,
	}
}

func notFound(service, msg string) error {
	return &Error{Service: service, Status: 404, Message: msg}
}

func badRequest(service, msg string) error {
	return &Error{Service: service, Status: 400, Message: msg}
}

// find locates a customer by GUID. Callers must hold at least an RLock.
func (s *fakeStore) find(guid string) *Customer {
	for i := range s.customers {
		if s.customers[i].MMGlobalCustomerID == guid {
			return &s.customers[i]
		}
	}
	return nil
}

// findByMSISDN matches the primary number and any deprecated number, because a
// customer who swapped SIM is still expected to be findable by the old one.
func (s *fakeStore) findByMSISDN(msisdn string) *Customer {
	want := normaliseMSISDN(msisdn)
	for i := range s.customers {
		c := &s.customers[i]
		if normaliseMSISDN(c.MSISDN) == want {
			return c
		}
		for _, n := range c.ContactNumbers {
			if normaliseMSISDN(n.ContactNumber) == want {
				return c
			}
		}
	}
	return nil
}

// normaliseMSISDN reduces a number to its last 9 digits so +27821234502,
// 0821234502 and 27821234502 all match — agents type all three.
func normaliseMSISDN(s string) string {
	var digits []rune
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	if len(digits) > 9 {
		digits = digits[len(digits)-9:]
	}
	return string(digits)
}

func (s *fakeStore) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s-%06d", prefix, s.seq)
}

// ---------------------------------------------------------------------------
// CustomerService
// ---------------------------------------------------------------------------

type fakeCustomer struct{ s *fakeStore }

func (f *fakeCustomer) Search(_ context.Context, q CustomerSearchQuery) ([]Customer, int, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()

	var matched []Customer
	for i := range f.s.customers {
		if matchesQuery(&f.s.customers[i], q) {
			matched = append(matched, f.s.customers[i])
		}
	}

	perPage := q.PerPage
	if perPage <= 0 {
		perPage = 20
	}
	page := q.Page
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * perPage
	if start >= len(matched) {
		return nil, len(matched), nil
	}
	end := min(start+perPage, len(matched))
	return matched[start:end], len(matched), nil
}

func matchesQuery(c *Customer, q CustomerSearchQuery) bool {
	if q.MMGlobalCustomerID != "" && !strings.EqualFold(c.MMGlobalCustomerID, q.MMGlobalCustomerID) {
		return false
	}
	if q.MSISDN != "" {
		want := normaliseMSISDN(q.MSISDN)
		if !msisdnMatches(c, want) {
			return false
		}
	}
	if q.Status != "" && c.Status != q.Status {
		return false
	}
	if q.FirstName != "" && !containsFold(c.FirstName, q.FirstName) {
		return false
	}
	if q.LastName != "" && !containsFold(c.LastName, q.LastName) {
		return false
	}
	if q.EmailAddress != "" && !containsFold(c.EmailAddress, q.EmailAddress) {
		return false
	}
	if q.IDNumber != "" {
		found := false
		for _, id := range c.IDNumbers {
			if strings.EqualFold(id.IdentificationNumber, q.IDNumber) ||
				strings.EqualFold(id.TemporaryResidentNumber, q.IDNumber) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func msisdnMatches(c *Customer, want string) bool {
	if normaliseMSISDN(c.MSISDN) == want {
		return true
	}
	for _, n := range c.ContactNumbers {
		if normaliseMSISDN(n.ContactNumber) == want {
			return true
		}
	}
	return false
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func (f *fakeCustomer) GetByGUID(_ context.Context, guid string) (*Customer, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	c := f.s.find(guid)
	if c == nil {
		return nil, notFound("customer-service", "customer "+guid+" not found")
	}
	out := cloneCustomer(*c)
	return &out, nil
}

func (f *fakeCustomer) GetByMSISDN(_ context.Context, msisdn string) (*Customer, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	c := f.s.findByMSISDN(msisdn)
	if c == nil {
		return nil, notFound("customer-service", "no customer for msisdn "+msisdn)
	}
	out := cloneCustomer(*c)
	return &out, nil
}

// cloneCustomer deep-copies the slices so a caller mutating a returned customer
// cannot corrupt the store behind the mutex.
func cloneCustomer(c Customer) Customer {
	c.Addresses = append([]Address(nil), c.Addresses...)
	c.ContactNumbers = append([]ContactNumber(nil), c.ContactNumbers...)
	c.IDNumbers = append([]IDNumber(nil), c.IDNumbers...)
	c.Documents = append([]Document(nil), c.Documents...)
	return c
}

func (f *fakeCustomer) Create(_ context.Context, in CustomerCreate) (*Customer, error) {
	in.Defaults()
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	if in.MSISDN == "" {
		return nil, badRequest("customer-service", "msisdn is required")
	}
	if f.s.findByMSISDN(in.MSISDN) != nil {
		return nil, &Error{Service: "customer-service", Status: 409, Message: "customer already exists for " + in.MSISDN}
	}

	c := Customer{
		MMGlobalCustomerID: fmt.Sprintf("8f2a1c40-1e11-4c8a-9d21-0a5f7b3c%04d", 2000+f.s.seq),
		FirstName:          in.FirstName,
		LastName:           in.LastName,
		MSISDN:             in.MSISDN,
		EmailAddress:       in.EmailAddress,
		DateOfBirth:        in.DateOfBirth,
		Gender:             in.Gender,
		PreferredLanguage:  in.PreferredLanguage,
		Status:             StatusInactive,
		AgentID:            in.AgentID,
		InboundChannel:     in.InboundChannel,
		DateCreated:        dts(0, 0),
		DateModified:       dts(0, 0),
		ContactNumbers: []ContactNumber{
			{ContactNumber: in.MSISDN, ContactNumberTypeCode: ContactPrimaryMSISDN},
		},
	}
	f.s.seq++
	if in.StreetAddress != "" || in.StreetCity != "" {
		c.Addresses = []Address{{
			StreetAddress: in.StreetAddress,
			Suburb:        in.StreetSuburb,
			City:          in.StreetCity,
			Province:      in.StreetProvince,
			PostalCode:    in.PostalCode,
			Country:       "ZA",
		}}
	}
	if in.IDNumber != "" {
		c.IDNumbers = []IDNumber{{IdentificationNumber: in.IDNumber, CountryCode: "ZA"}}
	}

	f.s.customers = append(f.s.customers, c)
	f.s.balances[c.MMGlobalCustomerID] = &Balances{}
	f.s.limitBals[c.MMGlobalCustomerID] = &LimitBalance{MonthlyLimit: 5000, Remaining: 5000}

	out := cloneCustomer(c)
	return &out, nil
}

func (f *fakeCustomer) Update(_ context.Context, guid string, u CustomerUpdate) (*Customer, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c := f.s.find(guid)
	if c == nil {
		return nil, notFound("customer-service", "customer "+guid+" not found")
	}

	set(&c.FirstName, u.FirstName)
	set(&c.LastName, u.LastName)
	set(&c.EmailAddress, u.EmailAddress)
	set(&c.DateOfBirth, u.DateOfBirth)
	set(&c.Gender, u.Gender)

	if u.StreetAddress != nil || u.StreetSuburb != nil || u.StreetCity != nil ||
		u.StreetProvince != nil || u.PostalCode != nil {
		if len(c.Addresses) == 0 {
			c.Addresses = []Address{{Country: "ZA"}}
		}
		a := &c.Addresses[0]
		set(&a.StreetAddress, u.StreetAddress)
		set(&a.Suburb, u.StreetSuburb)
		set(&a.City, u.StreetCity)
		set(&a.Province, u.StreetProvince)
		set(&a.PostalCode, u.PostalCode)
	}
	c.DateModified = dts(0, 0)

	out := cloneCustomer(*c)
	return &out, nil
}

func set(dst *string, src *string) {
	if src != nil {
		*dst = *src
	}
}

func (f *fakeCustomer) UpdateStatus(_ context.Context, guid string, status CustomerStatus, reason string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c := f.s.find(guid)
	if c == nil {
		return notFound("customer-service", "customer "+guid+" not found")
	}
	switch status {
	case StatusSuspended, StatusPermanentlyBlocked, StatusBlockedPositiveMatch:
		if strings.TrimSpace(reason) == "" {
			return badRequest("customer-service", "reason is required for "+string(status))
		}
	}
	c.Status = status
	c.DateModified = dts(0, 0)
	return nil
}

func (f *fakeCustomer) Deprecate(_ context.Context, guid, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return badRequest("customer-service", "reason is required")
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c := f.s.find(guid)
	if c == nil {
		return notFound("customer-service", "customer "+guid+" not found")
	}
	c.Status = StatusDuplicate
	c.DateModified = dts(0, 0)
	return nil
}

func (f *fakeCustomer) Reinstate(_ context.Context, guid, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return badRequest("customer-service", "reason is required")
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c := f.s.find(guid)
	if c == nil {
		return notFound("customer-service", "customer "+guid+" not found")
	}
	c.Status = StatusActive
	c.DateModified = dts(0, 0)
	return nil
}

func (f *fakeCustomer) UpdateIncome(_ context.Context, guid, incomeID, sourceType string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c := f.s.find(guid)
	if c == nil {
		return notFound("customer-service", "customer "+guid+" not found")
	}
	c.IncomeID = incomeID
	c.IncomeSourceType = sourceType
	c.DateModified = dts(0, 0)
	return nil
}

func (f *fakeCustomer) ListDocuments(_ context.Context, guid string) ([]Document, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	c := f.s.find(guid)
	if c == nil {
		return nil, notFound("customer-service", "customer "+guid+" not found")
	}
	return append([]Document(nil), c.Documents...), nil
}

func (f *fakeCustomer) CreateDocument(_ context.Context, guid string, d DocumentUpload) (*Document, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c := f.s.find(guid)
	if c == nil {
		return nil, notFound("customer-service", "customer "+guid+" not found")
	}
	if d.DocumentType == "" {
		return nil, badRequest("customer-service", "documentType is required")
	}
	doc := Document{
		MediaID:              f.s.nextID("med"),
		DocumentName:         d.Filename,
		DocumentType:         d.DocumentType,
		DocumentSubType:      d.DocumentSubType,
		DocumentNumber:       d.DocumentNumber,
		DocumentStatus:       DocStatusPending,
		InboundChannel:       "KORDINATE",
		IssueDate:            d.IssueDate,
		ExpiryDate:           d.ExpiryDate,
		IssuingCountry:       d.IssuingCountry,
		CustomerMediaType:    d.MediaType,
		CustomerMediaSubType: d.DocumentSubType,
		TimeCreated:          dts(0, 0),
		ProcessingAgentID:    d.AgentID,
	}
	c.Documents = append(c.Documents, doc)
	return &doc, nil
}

func (f *fakeCustomer) SetDocumentStatus(_ context.Context, guid, mediaID, status, reason string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c := f.s.find(guid)
	if c == nil {
		return notFound("customer-service", "customer "+guid+" not found")
	}
	if status == DocStatusRejected && strings.TrimSpace(reason) == "" {
		return badRequest("customer-service", "reason is required when rejecting a document")
	}
	for i := range c.Documents {
		if c.Documents[i].MediaID != mediaID {
			continue
		}
		d := &c.Documents[i]
		d.DocumentStatus = status
		d.ProcessingAgentID = "kordinate.agent"
		switch status {
		case DocStatusApproved:
			d.DocumentApprovalCode = "AGENT_APPROVED"
		case DocStatusRejected:
			d.DocumentApprovalCode = "AGENT_REJECTED"
		}
		c.DateModified = dts(0, 0)
		return nil
	}
	return notFound("customer-service", "document "+mediaID+" not found")
}

func (f *fakeCustomer) FetchDocument(_ context.Context, guid, mediaID string) ([]byte, string, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	c := f.s.find(guid)
	if c == nil {
		return nil, "", notFound("customer-service", "customer "+guid+" not found")
	}
	for _, d := range c.Documents {
		if d.MediaID == mediaID {
			mt := d.CustomerMediaType
			if mt == "" {
				mt = "image/png"
			}
			return append([]byte(nil), seedDocumentBytes...), mt, nil
		}
	}
	return nil, "", notFound("customer-service", "document "+mediaID+" not found")
}

func (f *fakeCustomer) BulkSuspend(_ context.Context, guids []string, reason string) (map[string]error, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, badRequest("customer-service", "reason is required")
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()

	// Per-customer failures are returned rather than aborting: the device
	// blocker must suspend everyone it can even if one guid is stale.
	results := make(map[string]error, len(guids))
	for _, g := range guids {
		c := f.s.find(g)
		if c == nil {
			results[g] = notFound("customer-service", "customer "+g+" not found")
			continue
		}
		if c.Status == StatusPermanentlyBlocked {
			results[g] = badRequest("customer-service", "customer is permanently blocked")
			continue
		}
		c.Status = StatusSuspended
		c.DateModified = dts(0, 0)
		results[g] = nil
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// ClaireService
// ---------------------------------------------------------------------------

type fakeClaire struct{ s *fakeStore }

func (f *fakeClaire) GetCustomerByMSISDN(_ context.Context, msisdn, mmGUID string) (*ClaireCustomer, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()

	var c *Customer
	if mmGUID != "" {
		c = f.s.find(mmGUID)
	}
	if c == nil && msisdn != "" {
		c = f.s.findByMSISDN(msisdn)
	}
	if c == nil {
		return nil, notFound("claire", "no claire customer for "+msisdn)
	}
	if c.ClaireCustomerID == "" {
		return nil, notFound("claire", "customer never existed in claire")
	}
	return &ClaireCustomer{
		CustomerID:       c.ClaireCustomerID,
		LimitID:          c.LimitID,
		IncomeID:         c.IncomeID,
		IncomeSourceType: c.IncomeSourceType,
	}, nil
}

func (f *fakeClaire) MonthlyLimit(_ context.Context, limitID string) (*MonthlyLimit, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	l, ok := f.s.limits[limitID]
	if !ok {
		return nil, notFound("claire", "limit "+limitID+" not found")
	}
	out := *l
	return &out, nil
}

func (f *fakeClaire) MonthlyLimitBalance(_ context.Context, mmGUID string) (*LimitBalance, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	b, ok := f.s.limitBals[mmGUID]
	if !ok {
		return nil, notFound("claire", "no limit balance for "+mmGUID)
	}
	out := *b
	return &out, nil
}

func (f *fakeClaire) IncomeRanges(context.Context) ([]IncomeRange, error) {
	return seedIncomeRanges(), nil
}

func (f *fakeClaire) IncomeSources(context.Context) ([]IncomeSource, error) {
	return seedIncomeSources(), nil
}

func (f *fakeClaire) RiskMatrix(_ context.Context, mmGUID string) (*RiskMatrix, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	r, ok := f.s.risk[mmGUID]
	if !ok {
		return nil, notFound("claire", "no risk rating for "+mmGUID)
	}
	out := *r
	return &out, nil
}

func (f *fakeClaire) Orders(_ context.Context, mmGUID string, from, to time.Time) ([]Order, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	return f.s.ordersFor(mmGUID, from, to, "claire"), nil
}

// ordersFor filters by customer, date range and (optionally) source. Callers
// must hold at least an RLock.
func (s *fakeStore) ordersFor(mmGUID string, from, to time.Time, source string) []Order {
	var out []Order
	for _, o := range s.orders {
		if o.MMGlobalCustomerID != mmGUID {
			continue
		}
		if source != "" && o.Source != source {
			continue
		}
		created, err := time.Parse("2006-01-02 15:04:05", o.TimeCreated)
		if err == nil && !inRange(created, from, to) {
			continue
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TimeCreated > out[j].TimeCreated })
	return out
}

// inRange treats a zero bound as unbounded, matching how the handlers pass
// optional date filters through.
func inRange(t, from, to time.Time) bool {
	if !from.IsZero() && t.Before(from) {
		return false
	}
	if !to.IsZero() && t.After(to) {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// UMLService
// ---------------------------------------------------------------------------

type fakeUML struct{ s *fakeStore }

func (f *fakeUML) Balances(_ context.Context, mmGUID, msisdn string) (*Balances, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	b, ok := f.s.balances[mmGUID]
	if !ok {
		return nil, notFound("uml", "no balances for "+mmGUID)
	}
	out := Balances{Wallet: copyPtr(b.Wallet), Card: copyPtr(b.Card), USDC: copyPtr(b.USDC), ZAR: copyPtr(b.ZAR)}
	if len(b.Errors) > 0 {
		out.Errors = make(map[string]string, len(b.Errors))
		for k, v := range b.Errors {
			out.Errors[k] = v
		}
	}
	return &out, nil
}

func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func (f *fakeUML) WalletBalance(_ context.Context, mmGUID string) (float64, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	b, ok := f.s.balances[mmGUID]
	if !ok || b.Wallet == nil {
		return 0, notFound("uml", "no wallet for "+mmGUID)
	}
	return *b.Wallet, nil
}

func (f *fakeUML) CloeBalance(_ context.Context, mmGUID string) (*CloeBalance, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	c, ok := f.s.cloe[mmGUID]
	if !ok {
		return nil, notFound("uml", "no usd savings account for "+mmGUID)
	}
	out := *c
	return &out, nil
}

func (f *fakeUML) CardBalance(_ context.Context, msisdn string, transactionAmount float64) (*CardBalance, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	c := f.s.findByMSISDN(msisdn)
	if c == nil {
		return nil, notFound("uml", "no customer for msisdn "+msisdn)
	}
	b := f.s.balances[c.MMGlobalCustomerID]
	if b == nil || b.Card == nil {
		if b != nil && b.Errors["card"] != "" {
			return nil, &Error{Service: "uml", Status: 502, Message: b.Errors["card"]}
		}
		return nil, notFound("uml", "no card for msisdn "+msisdn)
	}
	return &CardBalance{
		Response:         "00",
		AvailableBalance: *b.Card,
		RequestedAmount:  transactionAmount,
		IsEnough:         *b.Card >= transactionAmount,
	}, nil
}

func (f *fakeUML) BankingStatus(_ context.Context, mmGUID string) (*BankingStatus, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	b, ok := f.s.bankStatus[mmGUID]
	if !ok {
		return nil, notFound("uml", "no banking status for "+mmGUID)
	}
	out := *b
	return &out, nil
}

func (f *fakeUML) BankingEligibility(_ context.Context, mmGUID string) (*BankingEligibility, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	if e, ok := f.s.eligibility[mmGUID]; ok {
		out := *e
		return &out, nil
	}
	if f.s.find(mmGUID) == nil {
		return nil, notFound("uml", "no customer "+mmGUID)
	}
	// Already a banking customer, so the opt-in question doesn't apply.
	return &BankingEligibility{Eligible: false, IneligibleReason: "Customer is already onboarded for banking"}, nil
}

func (f *fakeUML) BankingCustomer(_ context.Context, mmGUID string) (*BankingCustomer, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	b, ok := f.s.bankCust[mmGUID]
	if !ok {
		return nil, notFound("uml", "no banking customer for "+mmGUID)
	}
	out := *b
	return &out, nil
}

// MamaBankingCustomer is the registration-status view; the fake serves the same
// record because a single store row carries both sets of fields.
func (f *fakeUML) MamaBankingCustomer(ctx context.Context, mmGUID string) (*BankingCustomer, error) {
	return f.BankingCustomer(ctx, mmGUID)
}

func (f *fakeUML) OptInForBanking(_ context.Context, mmGUID string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	c := f.s.find(mmGUID)
	if c == nil {
		return notFound("uml", "no customer "+mmGUID)
	}
	if e, ok := f.s.eligibility[mmGUID]; ok && !e.Eligible {
		return badRequest("uml", "customer not eligible: "+e.IneligibleReason)
	}
	f.s.bankStatus[mmGUID] = &BankingStatus{OnboardingStatus: "PENDING", CardStatus: "PENDING_ALLOCATION"}
	f.s.bankCust[mmGUID] = &BankingCustomer{
		CustomerID:         "UML-" + strconv.Itoa(8843000+f.s.seq),
		RegistrationStatus: "PENDING",
	}
	f.s.seq++
	delete(f.s.eligibility, mmGUID)
	return nil
}

func (f *fakeUML) BlockCard(_ context.Context, mmGUID, reason string) error {
	return f.setCardStatus(mmGUID, "BLOCKED", reason)
}

func (f *fakeUML) UnblockCard(_ context.Context, mmGUID, reason string) error {
	return f.setCardStatus(mmGUID, "ACTIVE", reason)
}

func (f *fakeUML) setCardStatus(mmGUID, cardStatus, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return badRequest("uml", "reason is required")
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	st, ok := f.s.bankStatus[mmGUID]
	if !ok {
		return notFound("uml", "no banking status for "+mmGUID)
	}
	if !st.IsActive() {
		return badRequest("uml", "customer is not an active banking customer")
	}
	st.CardStatus = cardStatus
	return nil
}

func (f *fakeUML) ResetCardPIN(_ context.Context, mmGUID string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	if _, ok := f.s.bankStatus[mmGUID]; !ok {
		return notFound("uml", "no banking status for "+mmGUID)
	}
	f.s.pinResets["card:"+mmGUID] = dt(0, 0)
	return nil
}

func (f *fakeUML) ReallocateCard(_ context.Context, mmGUID, cardSequenceNumber string) error {
	if strings.TrimSpace(cardSequenceNumber) == "" {
		return badRequest("uml", "cardSequenceNumber is required")
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	bc, ok := f.s.bankCust[mmGUID]
	if !ok {
		return notFound("uml", "no banking customer for "+mmGUID)
	}
	bc.CardSequenceNumber = cardSequenceNumber
	bc.RegistrationStatus = "REGISTERED"
	bc.RegistrationErrorCode = ""
	bc.RegistrationErrorDescription = ""
	f.s.bankStatus[mmGUID] = &BankingStatus{OnboardingStatus: "ACTIVE", CardStatus: "ACTIVE"}
	return nil
}

func (f *fakeUML) RetryCardAllocation(_ context.Context, mmGUID string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	bc, ok := f.s.bankCust[mmGUID]
	if !ok {
		return notFound("uml", "no banking customer for "+mmGUID)
	}
	if bc.RegistrationStatus == "REGISTERED" {
		return badRequest("uml", "card already allocated")
	}
	bc.CardSequenceNumber = "54129" + strconv.Itoa(90100+f.s.seq)
	bc.RegistrationStatus = "REGISTERED"
	bc.RegistrationErrorCode = ""
	bc.RegistrationErrorDescription = ""
	f.s.seq++
	f.s.bankStatus[mmGUID] = &BankingStatus{OnboardingStatus: "ACTIVE", CardStatus: "ACTIVE"}
	if _, ok := f.s.bankDetails[mmGUID]; !ok {
		f.s.bankDetails[mmGUID] = &BankingDetails{
			AccountNumber: "628841" + strconv.Itoa(20200+f.s.seq),
			AccountType:   "TRANSACTIONAL",
			BankName:      "MamaMoney Banking",
			BranchCode:    "470010",
		}
	}
	return nil
}

func (f *fakeUML) BankingDetails(_ context.Context, mmGUID string) (*BankingDetails, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	d, ok := f.s.bankDetails[mmGUID]
	if !ok {
		return nil, notFound("uml", "no banking details for "+mmGUID)
	}
	out := *d
	return &out, nil
}

func (f *fakeUML) WalletTransactions(_ context.Context, mmGUID string, from, to time.Time) ([]WalletTransaction, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	if f.s.find(mmGUID) == nil {
		return nil, notFound("uml", "no customer "+mmGUID)
	}
	var out []WalletTransaction
	for _, w := range f.s.walletTx {
		if w.owner == mmGUID && inRange(w.tx.At, from, to) {
			out = append(out, w.tx)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// ---------------------------------------------------------------------------
// UOPSService
// ---------------------------------------------------------------------------

type fakeUOPS struct{ s *fakeStore }

func (f *fakeUOPS) CustomerOrders(_ context.Context, mmGUID string, from, to time.Time) ([]Order, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	if f.s.find(mmGUID) == nil {
		return nil, notFound("uops", "no customer "+mmGUID)
	}
	return f.s.ordersFor(mmGUID, from, to, ""), nil
}

// ---------------------------------------------------------------------------
// EmmaService
// ---------------------------------------------------------------------------

type fakeEmma struct{ s *fakeStore }

func (f *fakeEmma) PendingManualNotifications(context.Context) ([]EFTNotification, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	var out []EFTNotification
	for _, n := range f.s.efts {
		if n.ProcessOutcome == EFTManualIntervention || n.ProcessOutcome == EFTPendingProcessing {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DateReceived.After(out[j].DateReceived) })
	return out, nil
}

func (f *fakeEmma) NotificationsByCustomer(_ context.Context, mmGUID string) ([]EFTNotification, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	var out []EFTNotification
	for _, n := range f.s.efts {
		if n.MMGlobalCustomerID == mmGUID {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DateReceived.After(out[j].DateReceived) })
	return out, nil
}

func (f *fakeEmma) AssignDeposit(_ context.Context, notificationID int64, mmGUID, agentID string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	n := f.s.eft(notificationID)
	if n == nil {
		return notFound("emma", fmt.Sprintf("notification %d not found", notificationID))
	}
	if n.MMGlobalCustomerID != "" {
		return badRequest("emma", "deposit is already assigned")
	}
	if f.s.find(mmGUID) == nil {
		return notFound("emma", "no customer "+mmGUID)
	}
	n.MMGlobalCustomerID = mmGUID
	n.ProcessOutcome = EFTManualOrderPaid

	// Assigning a deposit credits the wallet, so the ledger and balance move
	// together — otherwise the demo shows an assignment with no money.
	if b := f.s.balances[mmGUID]; b != nil {
		newBal := n.Amount
		if b.Wallet != nil {
			newBal += *b.Wallet
		}
		b.Wallet = &newBal
		f.s.walletTx = append(f.s.walletTx, fakeWalletTx{owner: mmGUID, tx: WalletTransaction{
			TransactionID: f.s.nextID("WTX"),
			Amount:        n.Amount,
			Balance:       newBal,
			Type:          "CREDIT",
			Description:   "Manually assigned " + n.PaymentChannel + " deposit - " + n.Bank,
			Reference:     n.OriginalReference,
			At:            dt(0, 0),
		}})
	}
	return nil
}

func (f *fakeEmma) RefundDeposit(_ context.Context, notificationID int64, reason, agentID string) error {
	if strings.TrimSpace(reason) == "" {
		return badRequest("emma", "reason is required")
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	n := f.s.eft(notificationID)
	if n == nil {
		return notFound("emma", fmt.Sprintf("notification %d not found", notificationID))
	}
	n.ProcessOutcome = EFTPurged
	return nil
}

func (f *fakeEmma) MarkSuccess(_ context.Context, notificationID int64, agentID string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	n := f.s.eft(notificationID)
	if n == nil {
		return notFound("emma", fmt.Sprintf("notification %d not found", notificationID))
	}
	n.ProcessOutcome = EFTOrderPaid
	return nil
}

func (f *fakeEmma) SearchUnmatched(_ context.Context, q UnmatchedQuery) ([]EFTNotification, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	var out []EFTNotification
	for _, n := range f.s.efts {
		if n.MMGlobalCustomerID != "" {
			continue
		}
		if q.Reference != "" && !containsFold(n.OriginalReference, q.Reference) {
			continue
		}
		if q.Bank != "" && !containsFold(n.Bank, q.Bank) {
			continue
		}
		if q.AmountMin > 0 && n.Amount < q.AmountMin {
			continue
		}
		if q.AmountMax > 0 && n.Amount > q.AmountMax {
			continue
		}
		if !inRange(n.DateReceived, q.From, q.To) {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DateReceived.After(out[j].DateReceived) })
	return out, nil
}

func (s *fakeStore) eft(id int64) *EFTNotification {
	for i := range s.efts {
		if s.efts[i].EFTNotificationID == id {
			return &s.efts[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// IDVService
// ---------------------------------------------------------------------------

type fakeIDV struct{ s *fakeStore }

func (f *fakeIDV) ResetLoginPIN(_ context.Context, mmGUID, agentID string) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	if f.s.find(mmGUID) == nil {
		return notFound("idv", "no customer "+mmGUID)
	}
	f.s.pinResets["login:"+mmGUID] = dt(0, 0)
	return nil
}

// ---------------------------------------------------------------------------
// DeviceService
// ---------------------------------------------------------------------------

type fakeDevice struct{ s *fakeStore }

func (f *fakeDevice) Device(_ context.Context, deviceID string) (*Device, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	d := f.s.device(deviceID)
	if d == nil {
		return nil, notFound("device-blocker", "device "+deviceID+" not found")
	}
	out := *d
	out.LinkedCustomers = append([]string(nil), d.LinkedCustomers...)
	return &out, nil
}

func (f *fakeDevice) DevicesForCustomer(_ context.Context, mmGUID string) ([]Device, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	var out []Device
	for _, d := range f.s.devices {
		for _, l := range d.LinkedCustomers {
			if l == mmGUID {
				cp := d
				cp.LinkedCustomers = append([]string(nil), d.LinkedCustomers...)
				out = append(out, cp)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeDevice) SetStatus(_ context.Context, deviceID string, status DeviceStatus, reason, agentID string) error {
	if status == DeviceBlocked && strings.TrimSpace(reason) == "" {
		return badRequest("device-blocker", "reason is required to block a device")
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	d := f.s.device(deviceID)
	if d == nil {
		return notFound("device-blocker", "device "+deviceID+" not found")
	}
	d.DeviceStatus = status
	return nil
}

func (f *fakeDevice) Register(_ context.Context, deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return badRequest("device-blocker", "deviceId is required")
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	if f.s.device(deviceID) != nil {
		return &Error{Service: "device-blocker", Status: 409, Message: "device already registered"}
	}
	f.s.devices = append(f.s.devices, Device{
		DeviceID:     deviceID,
		DeviceStatus: DeviceActive,
		FirstSeen:    dts(0, 0),
		LastSeen:     dts(0, 0),
	})
	return nil
}

func (f *fakeDevice) PatchAndUpdateLinked(_ context.Context, deviceID string, status DeviceStatus, linkedCustomers []string, reason, agentID string) error {
	if status == DeviceBlocked && strings.TrimSpace(reason) == "" {
		return badRequest("device-blocker", "reason is required to block a device")
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	d := f.s.device(deviceID)
	if d == nil {
		return notFound("device-blocker", "device "+deviceID+" not found")
	}
	d.DeviceStatus = status

	// The point of this call is the cascade: block the handset and every
	// customer on it, in one atomic step.
	target := StatusActive
	if status == DeviceBlocked {
		target = StatusSuspended
	}
	for _, g := range linkedCustomers {
		c := f.s.find(g)
		if c == nil || c.Status == StatusPermanentlyBlocked {
			continue
		}
		c.Status = target
		c.DateModified = dts(0, 0)
	}
	return nil
}

func (s *fakeStore) device(id string) *Device {
	for i := range s.devices {
		if s.devices[i].DeviceID == id {
			return &s.devices[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// VMSService
// ---------------------------------------------------------------------------

type fakeVMS struct{ s *fakeStore }

func (f *fakeVMS) Voucher(_ context.Context, code string) (*Voucher, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	v := f.s.voucher(code)
	if v == nil {
		return nil, notFound("vms", "voucher "+code+" not found")
	}
	out := v.v
	return &out, nil
}

func (f *fakeVMS) VouchersForCustomer(_ context.Context, mmGUID string) ([]Voucher, error) {
	f.s.mu.RLock()
	defer f.s.mu.RUnlock()
	var out []Voucher
	for _, fv := range f.s.vouchers {
		if fv.owner == mmGUID {
			out = append(out, fv.v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeVMS) Cancel(_ context.Context, code, reason, agentID string) error {
	if strings.TrimSpace(reason) == "" {
		return badRequest("vms", "reason is required")
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	fv := f.s.voucher(code)
	if fv == nil {
		return notFound("vms", "voucher "+code+" not found")
	}
	if fv.v.Status != voucherActive {
		return badRequest("vms", "cannot cancel a voucher in status "+fv.v.Status)
	}
	fv.v.Status = voucherCancelled
	return nil
}

func (f *fakeVMS) UpdateRecipient(_ context.Context, code string, r VoucherRecipient) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	fv := f.s.voucher(code)
	if fv == nil {
		return notFound("vms", "voucher "+code+" not found")
	}
	if fv.v.Status == voucherRedeemed {
		return badRequest("vms", "cannot change the recipient of a redeemed voucher")
	}
	fv.v.Recipient = r
	return nil
}

func (f *fakeVMS) Create(_ context.Context, req VoucherCreate) ([]Voucher, error) {
	if req.Amount <= 0 {
		return nil, badRequest("vms", "amount must be positive")
	}
	qty := req.Quantity
	if qty <= 0 {
		qty = 1
	}
	currency := req.Currency
	if currency == "" {
		currency = "ZAR"
	}

	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	out := make([]Voucher, 0, qty)
	for range qty {
		f.s.seq++
		v := Voucher{
			Code:      fmt.Sprintf("MMV-%04d-%04d-%04d", 1000+f.s.seq, 2000+f.s.seq, f.s.seq),
			Amount:    req.Amount,
			Currency:  currency,
			Status:    voucherActive,
			Product:   req.Product,
			CreatedAt: dt(0, 0),
			ExpiresAt: ptr(dt(90, 0)),
		}
		f.s.vouchers = append(f.s.vouchers, fakeVoucher{v: v})
		out = append(out, v)
	}
	return out, nil
}

func (s *fakeStore) voucher(code string) *fakeVoucher {
	for i := range s.vouchers {
		if strings.EqualFold(s.vouchers[i].v.Code, code) {
			return &s.vouchers[i]
		}
	}
	return nil
}
