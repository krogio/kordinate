// Package upstream is kordinate's window onto the MamaMoney microservices.
//
// Every service sits behind an interface with two implementations: a live HTTP
// client and a deterministic fake. UPSTREAM_MODE=fake (the default outside
// production) lets kordinate run and be demoed with no VPN and no credentials,
// which is also what makes the handlers testable.
//
// Contracts here were derived from the claire-admin PHP service classes
// (app/Services/*.php) — paths and field names match what that app sends and
// reads today. The JSON is kebab-case on the Claire/Customer-Service side and
// camelCase on the newer services (UML, UOPS, Emma); the structs carry explicit
// tags rather than relying on a global convention, because the two styles
// genuinely coexist.
package upstream

import (
	"context"
	"time"
)

// Set is the bundle of upstream clients a handler needs. One struct so the
// module takes a single dependency and tests swap the whole set for fakes.
type Set struct {
	Customer CustomerService
	Claire   ClaireService
	UML      UMLService
	UOPS     UOPSService
	Emma     EmmaService
	IDV      IDVService
	Device   DeviceService
	VMS      VMSService
}

// CustomerStatus is the customer lifecycle state held by the customer service.
// Values match claire-admin's App\Types\Enums\CustomerStatus.
type CustomerStatus string

const (
	StatusActive               CustomerStatus = "ACTIVE"
	StatusInactive             CustomerStatus = "INACTIVE"
	StatusSuspended            CustomerStatus = "SUSPENDED"
	StatusDuplicate            CustomerStatus = "DUPLICATE"
	StatusUndergoingScreening  CustomerStatus = "UNDERGOING_SCREENING"
	StatusBlockedPositiveMatch CustomerStatus = "BLOCKED_POSITIVEMATCH"
	StatusPermanentlyBlocked   CustomerStatus = "PERMANENTLY_BLOCKED"
)

// Product identifies which MamaMoney product a transaction belongs to.
// Matches App\Types\Enums\UopsProduct.
type Product string

const (
	ProductRemittance Product = "REMITTANCE"
	ProductBanking    Product = "BANKING"
	ProductWallet     Product = "WALLET"
	ProductUSDSavings Product = "USD_SAVINGS"
)

// PaymentMethod matches App\Types\Enums\UopsPaymentMethod.
type PaymentMethod string

const (
	PayBillPayment      PaymentMethod = "BILL_PAYMENT"
	PayEFT              PaymentMethod = "EFT"
	PayBanking          PaymentMethod = "BANKING"
	PayWallet           PaymentMethod = "WALLET"
	PayUSDSavings       PaymentMethod = "USD_SAVINGS"
	PayRemittanceRefund PaymentMethod = "REMITTANCE_REFUND"
)

// OrderStatus matches App\Types\Enums\UopsOrderStatus.
type OrderStatus string

const (
	OrderActive    OrderStatus = "ACTIVE"
	OrderCancelled OrderStatus = "CANCELLED"
	OrderPaid      OrderStatus = "PAID"
)

// Customer is the customer-service record. Field names follow the kebab-case
// JSON that service returns (see claire-admin CustomerService).
//
// MMGlobalCustomerID is the identity that ties a customer together ACROSS
// services — every other client keys off it. ClaireCustomerID is the legacy
// numeric id that only the Claire monolith knows, resolved by MSISDN; it is
// absent for customers that never existed in Claire.
type Customer struct {
	MMGlobalCustomerID string         `json:"mm-global-customer-id"`
	ClaireCustomerID   string         `json:"claire-customer-id,omitempty"`
	FirstName          string         `json:"first-name"`
	LastName           string         `json:"last-name"`
	MSISDN             string         `json:"msisdn"`
	EmailAddress       string         `json:"email-address,omitempty"`
	DateOfBirth        string         `json:"date-of-birth,omitempty"`
	Gender             string         `json:"gender,omitempty"`
	CountryOfBirth     string         `json:"country-of-birth,omitempty"`
	PreferredLanguage  string         `json:"preferred-language,omitempty"`
	Status             CustomerStatus `json:"customer-status"`
	AgentID            string         `json:"agent-id,omitempty"`
	InboundChannel     string         `json:"inbound-channel,omitempty"`
	ActivationDate     string         `json:"activation-date,omitempty"`
	DateCreated        string         `json:"date-created,omitempty"`
	DateModified       string         `json:"date-modified,omitempty"`

	// Income/limit fields stitched in from Claire (legacy).
	LimitID          string `json:"limit-id,omitempty"`
	IncomeID         string `json:"income-id,omitempty"`
	IncomeSourceType string `json:"income-source-type,omitempty"`

	Addresses      []Address       `json:"customer-addresses,omitempty"`
	ContactNumbers []ContactNumber `json:"customer-contact-numbers,omitempty"`
	IDNumbers      []IDNumber      `json:"customer-identification-numbers,omitempty"`
	Documents      []Document      `json:"customer-documents,omitempty"`
}

// FullName is the display name, falling back gracefully on partial records —
// broken customer data is common enough in the legacy set that a blank name
// must not render as empty space in the UI.
func (c *Customer) FullName() string {
	switch {
	case c.FirstName != "" && c.LastName != "":
		return c.FirstName + " " + c.LastName
	case c.FirstName != "":
		return c.FirstName
	case c.LastName != "":
		return c.LastName
	default:
		return "(unnamed)"
	}
}

type Address struct {
	StreetAddress string `json:"street-address,omitempty"`
	Suburb        string `json:"suburb,omitempty"`
	City          string `json:"city,omitempty"`
	Province      string `json:"province,omitempty"`
	PostalCode    string `json:"postal-code,omitempty"`
	Country       string `json:"country,omitempty"`
}

// ContactNumber carries a typed phone number. The PRIMARY_MSISDN entry is the
// customer's live number; DEPRECATED_MSISDN entries are previous numbers kept
// for lookup (a customer who changed SIM is still findable by the old number).
type ContactNumber struct {
	ContactNumber         string `json:"contact-number"`
	ContactNumberTypeCode string `json:"contact-number-type-code"`
}

const (
	ContactPrimaryMSISDN    = "PRIMARY_MSISDN"
	ContactDeprecatedMSISDN = "DEPRECATED_MSISDN"
)

type IDNumber struct {
	IdentificationNumber    string `json:"identification-number"`
	CountryCode             string `json:"identification-number-country-code,omitempty"`
	CountryOfBirthCode      string `json:"identification-country-of-birth-code,omitempty"`
	TemporaryResidentNumber string `json:"temporary-resident-number,omitempty"`
}

// Document is a FICA/KYC document held against the customer. DocumentType is
// one of the DocType* constants; DocumentStatus the approval state.
type Document struct {
	MediaID              string `json:"media-id,omitempty"`
	DocumentName         string `json:"document-name,omitempty"`
	DocumentType         string `json:"document-type"`
	DocumentSubType      string `json:"document-sub-type,omitempty"`
	DocumentNumber       string `json:"document-number,omitempty"`
	DocumentStatus       string `json:"document-status,omitempty"`
	DocumentApprovalCode string `json:"document-approval-code,omitempty"`
	InboundChannel       string `json:"document-inbound-channel,omitempty"`
	IssueDate            string `json:"issue-date,omitempty"`
	ExpiryDate           string `json:"expiry-date,omitempty"`
	IssuingCountry       string `json:"issuing-country,omitempty"`
	CustomerMediaType    string `json:"customer-media-type,omitempty"`
	CustomerMediaSubType string `json:"customer-media-sub-type,omitempty"`
	TimeCreated          string `json:"time-created,omitempty"`
	ProcessingAgentID    string `json:"processing-agent-id,omitempty"`
}

// Document types recognised by the customer service (from claire-admin's
// compliance views). These drive which documents an onboarding state requires.
const (
	DocSAIDFront      = "SA_ID_FRONT"
	DocSAIDBack       = "SA_ID_BACK"
	DocForeignIDFront = "FOREIGN_ID_FRONT"
	DocForeignIDBack  = "FOREIGN_ID_BACK"
	DocPassport       = "PASSPORT"
	DocAsylumSeeker   = "ASYLUM_SEEKER"
	DocVoterCardFront = "VOTER_CARD_FRONT"
	DocVoterCardBack  = "VOTER_CARD_BACK"
	DocPOID           = "POID" // proof of identity
	DocPOIDBack       = "POID_BACK"
	DocPOSN           = "POSN" // proof of source of income/funds
	DocBankStatement  = "BANK_STATEMENT"
	DocPayslip        = "PAYSLIP"
	DocGeneral        = "GENERAL"
	DocUnspecified    = "UNSPECIFIED"
)

// Document approval states.
const (
	DocStatusPending  = "PENDING"
	DocStatusApproved = "APPROVED"
	DocStatusRejected = "REJECTED"
)

// CustomerSearchQuery is the filter set for finding customers. All fields are
// optional; the service ANDs whatever is provided.
type CustomerSearchQuery struct {
	MSISDN             string
	MMGlobalCustomerID string
	IDNumber           string
	FirstName          string
	LastName           string
	EmailAddress       string
	Status             CustomerStatus
	Page               int
	PerPage            int
}

// CustomerService is the customer master-data service (CUSTOMER_SERVICE_URL).
type CustomerService interface {
	// Search finds customers matching the query. Returns the page of results
	// and the total match count.
	Search(ctx context.Context, q CustomerSearchQuery) ([]Customer, int, error)
	// GetByGUID fetches one customer by mm-global-customer-id, with relations
	// (addresses, contact numbers, documents) loaded.
	GetByGUID(ctx context.Context, guid string) (*Customer, error)
	// GetByMSISDN fetches one customer by phone number.
	GetByMSISDN(ctx context.Context, msisdn string) (*Customer, error)
	// Create registers a new customer and returns the created record.
	Create(ctx context.Context, c CustomerCreate) (*Customer, error)
	// Update patches mutable customer fields.
	Update(ctx context.Context, guid string, c CustomerUpdate) (*Customer, error)
	// UpdateStatus moves the customer to a new lifecycle status. Reason is
	// recorded upstream and is required for suspend/block transitions.
	UpdateStatus(ctx context.Context, guid string, status CustomerStatus, reason string) error
	// Deprecate retires a duplicate/erroneous customer record.
	Deprecate(ctx context.Context, guid, reason string) error
	// Reinstate reverses a deprecation.
	Reinstate(ctx context.Context, guid, reason string) error
	// UpdateIncome sets the customer's declared income band and source.
	UpdateIncome(ctx context.Context, guid string, incomeID, sourceType string) error
	// ListDocuments returns the customer's FICA/KYC documents.
	ListDocuments(ctx context.Context, guid string) ([]Document, error)
	// CreateDocument uploads a document. Data is the raw file bytes.
	CreateDocument(ctx context.Context, guid string, d DocumentUpload) (*Document, error)
	// SetDocumentStatus approves or rejects a document. Reason is required on
	// rejection so the customer can be told what to re-submit.
	SetDocumentStatus(ctx context.Context, guid, mediaID, status, reason string) error
	// FetchDocument retrieves a stored document's bytes and media type.
	FetchDocument(ctx context.Context, guid, mediaID string) ([]byte, string, error)
	// BulkSuspend suspends many customers at once (device-blocker driven).
	BulkSuspend(ctx context.Context, guids []string, reason string) (map[string]error, error)
}

// CustomerCreate is the payload for registering a customer. Mirrors
// claire-admin's CustomerCreateDTO, including its defaults.
//
// The struct tags here are NOT the wire format: the customer service expects
// kebab-case ("first-name", "identification-id-type"), so live_customer.go
// builds the request body explicitly rather than marshalling this struct.
// Tags are retained only for kordinate's own JSON (form round-trips, logs).
type CustomerCreate struct {
	MSISDN            string `json:"msisdn"`
	FirstName         string `json:"firstName"`
	LastName          string `json:"lastName"`
	Gender            string `json:"gender,omitempty"`
	EmailAddress      string `json:"emailAddress,omitempty"`
	DateOfBirth       string `json:"dateOfBirth"`
	StreetAddress     string `json:"streetAddress,omitempty"`
	StreetSuburb      string `json:"streetSuburb,omitempty"`
	StreetCity        string `json:"streetCity,omitempty"`
	StreetProvince    string `json:"streetProvince,omitempty"`
	PostalCode        string `json:"postalCode,omitempty"`
	PIN               string `json:"pin,omitempty"`
	AgentID           string `json:"agentId,omitempty"`
	PreferredLanguage string `json:"preferredLanguage"`
	InboundChannel    string `json:"inboundChannel"`
	IDType            string `json:"identificationIdType"`
	IDNumber          string `json:"identificationNumber,omitempty"`
}

// Defaults fills the constant fields claire-admin always sent, so callers only
// supply real customer data.
func (c *CustomerCreate) Defaults() {
	if c.PreferredLanguage == "" {
		c.PreferredLanguage = "en-ZA"
	}
	if c.InboundChannel == "" {
		c.InboundChannel = "KORDINATE"
	}
	if c.IDType == "" {
		c.IDType = "UNSPECIFIED"
	}
}

// CustomerUpdate patches mutable fields. Pointer fields distinguish
// "not supplied" from "set to empty" — clearing an email is a real operation.
type CustomerUpdate struct {
	FirstName      *string
	LastName       *string
	EmailAddress   *string
	DateOfBirth    *string
	Gender         *string
	StreetAddress  *string
	StreetSuburb   *string
	StreetCity     *string
	StreetProvince *string
	PostalCode     *string
}

// DocumentUpload is a new document plus its bytes.
type DocumentUpload struct {
	DocumentType    string
	DocumentSubType string
	DocumentNumber  string
	IssueDate       string
	ExpiryDate      string
	IssuingCountry  string
	Filename        string
	MediaType       string
	Data            []byte
	// AgentID is the operator performing the upload, recorded upstream.
	AgentID string
}

// ClaireService is the legacy Claire monolith (CLAIRE_API_URL). It still owns
// transaction limits, income reference data and the risk matrix.
type ClaireService interface {
	// GetCustomerByMSISDN resolves the legacy Claire customer record, which
	// carries the numeric claire-customer-id and limit/income ids.
	GetCustomerByMSISDN(ctx context.Context, msisdn, mmGUID string) (*ClaireCustomer, error)
	// MonthlyLimit returns the customer's transaction limit band.
	MonthlyLimit(ctx context.Context, limitID string) (*MonthlyLimit, error)
	// MonthlyLimitBalance returns how much of the monthly limit remains.
	MonthlyLimitBalance(ctx context.Context, mmGUID string) (*LimitBalance, error)
	// IncomeRanges lists the selectable declared-income bands.
	IncomeRanges(ctx context.Context) ([]IncomeRange, error)
	// IncomeSources lists the selectable income source types.
	IncomeSources(ctx context.Context) ([]IncomeSource, error)
	// RiskMatrix returns the customer's compliance risk score.
	RiskMatrix(ctx context.Context, mmGUID string) (*RiskMatrix, error)
	// Orders lists the customer's remittance orders held in Claire.
	Orders(ctx context.Context, mmGUID string, from, to time.Time) ([]Order, error)
}

type ClaireCustomer struct {
	CustomerID       string `json:"customer-id"`
	LimitID          string `json:"limit-id,omitempty"`
	IncomeID         string `json:"income-id,omitempty"`
	IncomeSourceType string `json:"income-source-type,omitempty"`
}

type MonthlyLimit struct {
	LimitID     string  `json:"limit-id"`
	Description string  `json:"description"`
	Amount      float64 `json:"monthly-limit"`
}

// LimitBalance is how much of a monthly limit is still available — the number
// an agent needs before promising a customer a transaction will go through.
type LimitBalance struct {
	MonthlyLimit float64 `json:"monthlyLimit"`
	Used         float64 `json:"used"`
	Remaining    float64 `json:"remaining"`
}

type IncomeRange struct {
	IncomeID string `json:"income-id"`
	Range    string `json:"income-salary-range"`
}

type IncomeSource struct {
	Type        string `json:"income-source-type"`
	Description string `json:"description"`
}

// RiskMatrix is the compliance risk rating. Higher Score = higher risk;
// claire-admin's stored ratings ran 1–6.
type RiskMatrix struct {
	Score       int    `json:"score"`
	Description string `json:"description,omitempty"`
	SetBy       string `json:"set-by,omitempty"`
	SetAt       string `json:"set-at,omitempty"`
}

// Order is a transaction/order record. Shape follows UOPS OrderDTO, which is
// the richer of the two (Claire's orders are mapped onto it) so the UI has one
// transaction type regardless of source.
type Order struct {
	OrderID              string        `json:"orderId"`
	MMGlobalCustomerID   string        `json:"mmGlobalCustomerId"`
	Product              Product       `json:"product"`
	PaymentMethod        PaymentMethod `json:"paymentMethod"`
	Amount               float64       `json:"amount"`
	FeeAmount            float64       `json:"feeAmount,omitempty"`
	OrderReferenceNumber string        `json:"orderReferenceNumber"`
	OrderStatus          OrderStatus   `json:"orderStatus"`
	LatePayment          bool          `json:"latePayment"`
	TimeCreated          string        `json:"timeCreated"`
	TimeUpdated          string        `json:"timeUpdated"`
	// Source records which service this order came from, so a stitched
	// transaction list can show provenance ("uops" / "claire").
	Source string `json:"source,omitempty"`
}

// UMLService is the universal middle layer (UML_URL): banking, card and wallet
// balances. Note it versions per-endpoint — the status call is v2, the rest v1
// (see claire-admin UMLService::processRequest).
type UMLService interface {
	// Balances returns every product balance for the customer in one call.
	// This is the "wallet balance across all products" view.
	Balances(ctx context.Context, mmGUID, msisdn string) (*Balances, error)
	// WalletBalance is the ZAR wallet balance.
	WalletBalance(ctx context.Context, mmGUID string) (float64, error)
	// CloeBalance is the USD savings (Cloe) balance.
	CloeBalance(ctx context.Context, mmGUID string) (*CloeBalance, error)
	// CardBalance checks the card's available balance. transactionAmount may be
	// 0 to query without testing affordability.
	CardBalance(ctx context.Context, msisdn string, transactionAmount float64) (*CardBalance, error)
	// BankingStatus is the customer's card/banking onboarding state (v2).
	BankingStatus(ctx context.Context, mmGUID string) (*BankingStatus, error)
	// BankingEligibility reports whether a non-banking customer may opt in.
	BankingEligibility(ctx context.Context, mmGUID string) (*BankingEligibility, error)
	// BankingCustomer returns the card-side customer record (card sequence
	// number, customer id) used by card operations.
	BankingCustomer(ctx context.Context, mmGUID string) (*BankingCustomer, error)
	// MamaBankingCustomer returns the registration-status view of the banking
	// customer, which carries onboarding error codes.
	MamaBankingCustomer(ctx context.Context, mmGUID string) (*BankingCustomer, error)
	// OptInForBanking enrols the customer for banking/card.
	OptInForBanking(ctx context.Context, mmGUID string) error
	// BlockCard / UnblockCard toggle the card's block state.
	BlockCard(ctx context.Context, mmGUID, reason string) error
	UnblockCard(ctx context.Context, mmGUID, reason string) error
	// ResetCardPIN clears the card PIN so the customer can set a new one.
	ResetCardPIN(ctx context.Context, mmGUID string) error
	// ReallocateCard moves a card to the customer (recovery from a failed
	// allocation).
	ReallocateCard(ctx context.Context, mmGUID, cardSequenceNumber string) error
	// RetryCardAllocation re-runs a failed card allocation.
	RetryCardAllocation(ctx context.Context, mmGUID string) error
	// BankingDetails returns the customer's account/branch details.
	BankingDetails(ctx context.Context, mmGUID string) (*BankingDetails, error)
	// WalletTransactions lists wallet ledger entries.
	WalletTransactions(ctx context.Context, mmGUID string, from, to time.Time) ([]WalletTransaction, error)
}

// Balances is the cross-product balance view. Each field is a pointer because
// a product the customer doesn't hold is genuinely absent, which must render
// as "—" rather than "R0.00" — showing zero where there's no account is the
// kind of thing that starts a support ticket.
type Balances struct {
	Wallet *float64 `json:"wallet"`
	Card   *float64 `json:"card"`
	USDC   *float64 `json:"usdc"`
	ZAR    *float64 `json:"zar"`
	// Errors records per-product retrieval failures so the UI can distinguish
	// "no account" (nil balance, no error) from "lookup failed" (nil balance
	// with an error). Conflating those hides outages.
	Errors map[string]string `json:"errors,omitempty"`
}

type CloeBalance struct {
	ZARBalance  float64 `json:"zarBalance"`
	USDCBalance float64 `json:"usdcBalance"`
}

type CardBalance struct {
	Response         string  `json:"response"`
	AvailableBalance float64 `json:"availableBalance"`
	RequestedAmount  float64 `json:"requestedAmount"`
	IsEnough         bool    `json:"isEnough"`
}

// BankingStatus is the v2 card/banking state.
type BankingStatus struct {
	OnboardingStatus string `json:"onboardingStatus"`
	CardStatus       string `json:"cardStatus"`
}

// IsActive reports whether the customer is a live banking customer — the gate
// claire-admin used before showing card actions or fetching a card balance.
func (b *BankingStatus) IsActive() bool { return b != nil && b.OnboardingStatus == "ACTIVE" }

type BankingEligibility struct {
	Eligible         bool   `json:"eligible"`
	IneligibleReason string `json:"ineligibleReason,omitempty"`
}

type BankingCustomer struct {
	CustomerID                   string `json:"customerId,omitempty"`
	CardSequenceNumber           string `json:"cardSequenceNumber,omitempty"`
	RegistrationStatus           string `json:"registrationStatus,omitempty"`
	RegistrationErrorCode        string `json:"registrationErrorCode,omitempty"`
	RegistrationErrorDescription string `json:"registrationErrorDescription,omitempty"`
}

type BankingDetails struct {
	AccountNumber string `json:"accountNumber,omitempty"`
	AccountType   string `json:"accountType,omitempty"`
	BankName      string `json:"bankName,omitempty"`
	BranchCode    string `json:"branchCode,omitempty"`
}

// WalletTransaction is one wallet ledger entry.
type WalletTransaction struct {
	TransactionID string    `json:"transactionId"`
	Amount        float64   `json:"amount"`
	Balance       float64   `json:"balance"`
	Type          string    `json:"type"`
	Description   string    `json:"description"`
	Reference     string    `json:"reference,omitempty"`
	At            time.Time `json:"at"`
}

// UOPSService is the unified order processing service (UOPS_API_URL) — the
// cross-product order view.
type UOPSService interface {
	// CustomerOrders lists the customer's orders across all products.
	CustomerOrders(ctx context.Context, mmGUID string, from, to time.Time) ([]Order, error)
}

// EmmaService is the EFT notification service (EMMA_API).
type EmmaService interface {
	// PendingManualNotifications lists EFT deposits awaiting manual handling.
	PendingManualNotifications(ctx context.Context) ([]EFTNotification, error)
	// NotificationsByCustomer lists a customer's EFT notifications.
	NotificationsByCustomer(ctx context.Context, mmGUID string) ([]EFTNotification, error)
	// AssignDeposit attaches an unmatched deposit to a customer.
	AssignDeposit(ctx context.Context, notificationID int64, mmGUID, agentID string) error
	// RefundDeposit refunds a deposit back to source.
	RefundDeposit(ctx context.Context, notificationID int64, reason, agentID string) error
	// MarkSuccess marks a deposit as successfully processed.
	MarkSuccess(ctx context.Context, notificationID int64, agentID string) error
	// SearchUnmatched finds EFT deposits that never matched a customer.
	SearchUnmatched(ctx context.Context, q UnmatchedQuery) ([]EFTNotification, error)
}

// EFTNotification is an inbound EFT/branch/ATM deposit notification.
// Mirrors claire-admin's EftNotificationDTO.
type EFTNotification struct {
	EFTNotificationID  int64     `json:"eftNotificationId"`
	OriginalReference  string    `json:"originalReference"`
	Amount             float64   `json:"amount"`
	PaymentChannel     string    `json:"paymentChannel"` // EFT | BRANCH | ATM
	Bank               string    `json:"bank"`
	ProcessOutcome     string    `json:"eftProcessOutcome"`
	DateReceived       time.Time `json:"dateReceived"`
	MMGlobalCustomerID string    `json:"mmGlobalCustomerId,omitempty"`
}

// EFT process outcomes (App\Types\Enums\EmmaEftProcessOutcome).
const (
	EFTPendingProcessing  = "PENDING_PROCESSING"
	EFTOrderPaid          = "ORDER_PAID"
	EFTWalletOrderCreated = "WALLET_ORDER_CREATED"
	EFTManualIntervention = "MANUAL_INTERVENTION_REQUIRED"
	EFTManualOrderPaid    = "MANUAL_ORDER_PAID"
	EFTPurged             = "PURGED"
)

// UnmatchedQuery filters the unmatched-EFT search.
type UnmatchedQuery struct {
	Reference string
	Bank      string
	AmountMin float64
	AmountMax float64
	From      time.Time
	To        time.Time
}

// IDVService is the identity-verification service (IDV_SERVICE_API_URL).
type IDVService interface {
	// ResetLoginPIN clears the customer's app login PIN.
	ResetLoginPIN(ctx context.Context, mmGUID, agentID string) error
}

// DeviceStatus is a device's block state.
type DeviceStatus string

const (
	DeviceActive  DeviceStatus = "ACTIVE"
	DeviceBlocked DeviceStatus = "BLOCKED"
)

// DeviceService is the device blocker (DEVICE_BLOCKER_SERVICE_URL) — fraud
// containment by blocking a device and the customers linked to it.
type DeviceService interface {
	// Device returns a device's status and the customers linked to it.
	Device(ctx context.Context, deviceID string) (*Device, error)
	// DevicesForCustomer lists devices a customer has used.
	DevicesForCustomer(ctx context.Context, mmGUID string) ([]Device, error)
	// SetStatus blocks or unblocks a device.
	SetStatus(ctx context.Context, deviceID string, status DeviceStatus, reason, agentID string) error
	// Register adds a device record.
	Register(ctx context.Context, deviceID string) error
	// PatchAndUpdateLinked blocks a device AND applies the status to every
	// linked customer in one upstream operation.
	PatchAndUpdateLinked(ctx context.Context, deviceID string, status DeviceStatus, linkedCustomers []string, reason, agentID string) error
}

// Device is a device record with its linked customers.
type Device struct {
	DeviceID        string       `json:"deviceId"`
	DeviceStatus    DeviceStatus `json:"deviceStatus"`
	LinkedCustomers []string     `json:"linkedCustomers,omitempty"`
	FirstSeen       string       `json:"firstSeen,omitempty"`
	LastSeen        string       `json:"lastSeen,omitempty"`
}

// VMSService is the voucher management service (VMS_API_URL).
type VMSService interface {
	// Voucher returns a voucher's details by code.
	Voucher(ctx context.Context, code string) (*Voucher, error)
	// VouchersForCustomer lists a customer's vouchers.
	VouchersForCustomer(ctx context.Context, mmGUID string) ([]Voucher, error)
	// Cancel cancels an unredeemed voucher.
	Cancel(ctx context.Context, code, reason, agentID string) error
	// UpdateRecipient corrects a voucher's recipient details.
	UpdateRecipient(ctx context.Context, code string, r VoucherRecipient) error
	// Create issues vouchers.
	Create(ctx context.Context, req VoucherCreate) ([]Voucher, error)
}

type Voucher struct {
	Code       string           `json:"code"`
	Amount     float64          `json:"amount"`
	Currency   string           `json:"currency"`
	Status     string           `json:"status"`
	Product    string           `json:"product,omitempty"`
	Recipient  VoucherRecipient `json:"recipient"`
	CreatedAt  time.Time        `json:"createdAt"`
	RedeemedAt *time.Time       `json:"redeemedAt,omitempty"`
	ExpiresAt  *time.Time       `json:"expiresAt,omitempty"`
}

type VoucherRecipient struct {
	Name   string `json:"name,omitempty"`
	MSISDN string `json:"msisdn,omitempty"`
	Email  string `json:"email,omitempty"`
}

type VoucherCreate struct {
	Amount   float64
	Currency string
	Quantity int
	Product  string
	AgentID  string
}
