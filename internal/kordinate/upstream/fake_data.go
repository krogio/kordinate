package upstream

import "time"

// fake_data.go is the seed dataset for the fake upstreams. It is deliberately
// hand-written rather than generated: the value of the fakes is that they carry
// the *shapes* of broken real data (blank surnames, missing claire ids, expired
// permits, failed card lookups) that the UI has to survive.

// seedNow anchors every date in the dataset. Nothing here may call time.Now():
// a demo that renders differently on Tuesday is a demo nobody trusts.
var seedNow = time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// dt returns a seed date offset by days/hours from seedNow.
func dt(days, hours int) time.Time {
	return seedNow.AddDate(0, 0, days).Add(time.Duration(hours) * time.Hour)
}

// ds formats an offset from seedNow as the date-only string the customer
// service returns.
func ds(days int) string { return dt(days, 0).Format("2006-01-02") }

// dts formats an offset as the timestamp string the customer service returns.
func dts(days, hours int) string { return dt(days, hours).Format("2006-01-02 15:04:05") }

// Customer GUIDs. Named constants because every other table in this file keys
// off them and a typo'd UUID is invisible.
const (
	guTendai   = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1001"
	guNomvula  = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1002"
	guBlessing = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1003"
	guChipo    = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1004"
	guSipho    = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1005"
	guAmara    = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1006"
	guThabo    = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1007"
	guFatima   = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1008"
	guMpho     = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1009"
	guEmmanuel = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1010"
	guLerato   = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1011"
	guJoao     = "8f2a1c40-1e11-4c8a-9d21-0a5f7b3c1012"
)

func seedCustomers() []Customer {
	return []Customer{
		// Fully-onboarded happy path: all three products, all docs approved.
		{
			MMGlobalCustomerID: guTendai,
			ClaireCustomerID:   "418293",
			FirstName:          "Tendai",
			LastName:           "Mukwazhi",
			MSISDN:             "+27761234501",
			EmailAddress:       "tendai.mukwazhi@gmail.com",
			DateOfBirth:        "1989-03-14",
			Gender:             "MALE",
			CountryOfBirth:     "ZW",
			PreferredLanguage:  "en-ZA",
			Status:             StatusActive,
			AgentID:            "agent.naledi",
			InboundChannel:     "ANDROID_APP",
			ActivationDate:     ds(-820),
			DateCreated:        dts(-822, -3),
			DateModified:       dts(-12, 2),
			LimitID:            "LIM_25000",
			IncomeID:           "INC_3",
			IncomeSourceType:   "SALARY",
			Addresses: []Address{{
				StreetAddress: "142 Bree Street",
				Suburb:        "Marshalltown",
				City:          "Johannesburg",
				Province:      "Gauteng",
				PostalCode:    "2001",
				Country:       "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27761234501", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber: "ZN398471",
				CountryCode:          "ZW",
				CountryOfBirthCode:   "ZW",
			}},
			Documents: []Document{
				{
					MediaID: "med-tendai-pp", DocumentName: "passport-tendai.jpg",
					DocumentType: DocPassport, DocumentNumber: "ZN398471",
					DocumentStatus: DocStatusApproved, DocumentApprovalCode: "AUTO_VERIFIED",
					InboundChannel: "ANDROID_APP", IssueDate: ds(-1100), ExpiryDate: ds(1450),
					IssuingCountry: "ZW", CustomerMediaType: "image/jpeg",
					TimeCreated: dts(-820, 1), ProcessingAgentID: "agent.naledi",
				},
				{
					MediaID: "med-tendai-poa", DocumentName: "payslip-june.pdf",
					DocumentType: DocPayslip, DocumentStatus: DocStatusApproved,
					DocumentApprovalCode: "AGENT_APPROVED", InboundChannel: "ANDROID_APP",
					IssueDate: ds(-45), CustomerMediaType: "application/pdf",
					TimeCreated: dts(-44, 4), ProcessingAgentID: "agent.pieter",
				},
			},
		},

		// Pending document awaiting review — the compliance queue's reason to exist.
		{
			MMGlobalCustomerID: guNomvula,
			ClaireCustomerID:   "418755",
			FirstName:          "Nomvula",
			LastName:           "Dlamini",
			MSISDN:             "+27821234502",
			EmailAddress:       "nomvula.d@outlook.com",
			DateOfBirth:        "1994-11-02",
			Gender:             "FEMALE",
			CountryOfBirth:     "ZA",
			PreferredLanguage:  "zu-ZA",
			Status:             StatusActive,
			InboundChannel:     "USSD",
			ActivationDate:     ds(-310),
			DateCreated:        dts(-311, -6),
			DateModified:       dts(-4, 1),
			LimitID:            "LIM_5000",
			IncomeID:           "INC_2",
			IncomeSourceType:   "SALARY",
			Addresses: []Address{{
				StreetAddress: "27 Umbilo Road",
				Suburb:        "Umbilo",
				City:          "Durban",
				Province:      "KwaZulu-Natal",
				PostalCode:    "4075",
				Country:       "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27821234502", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber: "9411025842083",
				CountryCode:          "ZA",
				CountryOfBirthCode:   "ZA",
			}},
			Documents: []Document{
				{
					MediaID: "med-nomvula-id-f", DocumentName: "sa-id-front.jpg",
					DocumentType: DocSAIDFront, DocumentNumber: "9411025842083",
					DocumentStatus: DocStatusApproved, DocumentApprovalCode: "AGENT_APPROVED",
					InboundChannel: "USSD", IssuingCountry: "ZA",
					CustomerMediaType: "image/jpeg", TimeCreated: dts(-310, 2),
					ProcessingAgentID: "agent.pieter",
				},
				{
					MediaID: "med-nomvula-posn", DocumentName: "capitec-statement.pdf",
					DocumentType: DocBankStatement, DocumentSubType: DocPOSN,
					DocumentStatus: DocStatusPending, InboundChannel: "ANDROID_APP",
					IssueDate: ds(-9), CustomerMediaType: "application/pdf",
					TimeCreated: dts(-4, 1),
				},
			},
		},

		// Rejected document + near-exhausted monthly limit.
		{
			MMGlobalCustomerID: guBlessing,
			ClaireCustomerID:   "419002",
			FirstName:          "Blessing",
			LastName:           "Chirwa",
			MSISDN:             "+27831234503",
			DateOfBirth:        "1991-07-28",
			Gender:             "MALE",
			CountryOfBirth:     "MW",
			PreferredLanguage:  "en-ZA",
			Status:             StatusActive,
			InboundChannel:     "ANDROID_APP",
			ActivationDate:     ds(-540),
			DateCreated:        dts(-541, -2),
			DateModified:       dts(-2, -1),
			LimitID:            "LIM_5000",
			IncomeID:           "INC_1",
			IncomeSourceType:   "INFORMAL_TRADING",
			Addresses: []Address{{
				StreetAddress: "8 Kerk Street",
				Suburb:        "Sunnyside",
				City:          "Pretoria",
				Province:      "Gauteng",
				PostalCode:    "0002",
				Country:       "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27831234503", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber: "MW7742019",
				CountryCode:          "MW",
				CountryOfBirthCode:   "MW",
			}},
			Documents: []Document{
				{
					MediaID: "med-blessing-pp", DocumentName: "passport-blessing.jpg",
					DocumentType: DocPassport, DocumentNumber: "MW7742019",
					DocumentStatus: DocStatusApproved, DocumentApprovalCode: "AGENT_APPROVED",
					InboundChannel: "ANDROID_APP", IssueDate: ds(-900), ExpiryDate: ds(920),
					IssuingCountry: "MW", CustomerMediaType: "image/jpeg",
					TimeCreated: dts(-540, 1), ProcessingAgentID: "agent.naledi",
				},
				{
					MediaID: "med-blessing-posn", DocumentName: "proof-of-income.jpg",
					DocumentType: DocPOSN, DocumentStatus: DocStatusRejected,
					DocumentApprovalCode: "ILLEGIBLE", InboundChannel: "WHATSAPP",
					CustomerMediaType: "image/jpeg", TimeCreated: dts(-2, -1),
					ProcessingAgentID: "agent.zanele",
				},
			},
		},

		// Expired passport: FICA-lapsed, still ACTIVE upstream. The mismatch is
		// exactly what an agent gets called about.
		{
			MMGlobalCustomerID: guChipo,
			ClaireCustomerID:   "417118",
			FirstName:          "Chipo",
			LastName:           "Nyathi",
			MSISDN:             "+27761234504",
			EmailAddress:       "chipo.nyathi@yahoo.com",
			DateOfBirth:        "1986-01-19",
			Gender:             "FEMALE",
			CountryOfBirth:     "ZW",
			PreferredLanguage:  "en-ZA",
			Status:             StatusActive,
			InboundChannel:     "ANDROID_APP",
			ActivationDate:     ds(-1180),
			DateCreated:        dts(-1181, -5),
			DateModified:       dts(-120, 3),
			LimitID:            "LIM_25000",
			IncomeID:           "INC_4",
			IncomeSourceType:   "SALARY",
			Addresses: []Address{{
				StreetAddress: "31 Voortrekker Road",
				Suburb:        "Bellville",
				City:          "Cape Town",
				Province:      "Western Cape",
				PostalCode:    "7530",
				Country:       "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27761234504", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber: "ZN201884",
				CountryCode:          "ZW",
				CountryOfBirthCode:   "ZW",
			}},
			Documents: []Document{
				{
					MediaID: "med-chipo-pp", DocumentName: "passport-chipo.jpg",
					DocumentType: DocPassport, DocumentNumber: "ZN201884",
					DocumentStatus: DocStatusApproved, DocumentApprovalCode: "AGENT_APPROVED",
					InboundChannel: "ANDROID_APP", IssueDate: ds(-2000), ExpiryDate: ds(-91),
					IssuingCountry: "ZW", CustomerMediaType: "image/jpeg",
					TimeCreated: dts(-1180, 2), ProcessingAgentID: "agent.pieter",
				},
			},
		},

		// No documents at all — onboarding abandoned after registration.
		{
			MMGlobalCustomerID: guSipho,
			ClaireCustomerID:   "421390",
			FirstName:          "Sipho",
			LastName:           "Mabaso",
			MSISDN:             "+27821234505",
			DateOfBirth:        "1998-05-30",
			Gender:             "MALE",
			CountryOfBirth:     "ZA",
			PreferredLanguage:  "zu-ZA",
			Status:             StatusInactive,
			InboundChannel:     "USSD",
			DateCreated:        dts(-58, -4),
			DateModified:       dts(-58, -4),
			Addresses: []Address{{
				Suburb:   "Soweto",
				City:     "Johannesburg",
				Province: "Gauteng",
				Country:  "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27821234505", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
		},

		// Asylum-seeker permit holder, currently under screening.
		{
			MMGlobalCustomerID: guAmara,
			ClaireCustomerID:   "420117",
			FirstName:          "Amara",
			LastName:           "Okonkwo",
			MSISDN:             "+27831234506",
			EmailAddress:       "amara.okonkwo@gmail.com",
			DateOfBirth:        "1993-09-08",
			Gender:             "FEMALE",
			CountryOfBirth:     "NG",
			PreferredLanguage:  "en-ZA",
			Status:             StatusUndergoingScreening,
			InboundChannel:     "ANDROID_APP",
			ActivationDate:     ds(-95),
			DateCreated:        dts(-97, -1),
			DateModified:       dts(-1, 5),
			LimitID:            "LIM_5000",
			IncomeID:           "INC_2",
			IncomeSourceType:   "SELF_EMPLOYED",
			Addresses: []Address{{
				StreetAddress: "5 Kotze Street",
				Suburb:        "Hillbrow",
				City:          "Johannesburg",
				Province:      "Gauteng",
				PostalCode:    "2001",
				Country:       "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27831234506", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber:    "ASY-2024-884120",
				CountryCode:             "ZA",
				CountryOfBirthCode:      "NG",
				TemporaryResidentNumber: "TRN884120",
			}},
			Documents: []Document{
				{
					MediaID: "med-amara-asylum", DocumentName: "section-22-permit.jpg",
					DocumentType: DocAsylumSeeker, DocumentNumber: "ASY-2024-884120",
					DocumentStatus: DocStatusApproved, DocumentApprovalCode: "AGENT_APPROVED",
					InboundChannel: "ANDROID_APP", IssueDate: ds(-190), ExpiryDate: ds(80),
					IssuingCountry: "ZA", CustomerMediaType: "image/jpeg",
					TimeCreated: dts(-96, 2), ProcessingAgentID: "agent.zanele",
				},
				{
					MediaID: "med-amara-poid", DocumentName: "selfie-with-permit.jpg",
					DocumentType: DocPOID, DocumentStatus: DocStatusPending,
					InboundChannel: "ANDROID_APP", CustomerMediaType: "image/jpeg",
					TimeCreated: dts(-1, 5),
				},
			},
		},

		// FirstName only — blank surname, the FullName() fallback case.
		{
			MMGlobalCustomerID: guThabo,
			ClaireCustomerID:   "402881",
			FirstName:          "Thabo",
			MSISDN:             "+27761234507",
			DateOfBirth:        "1979-12-04",
			Gender:             "MALE",
			CountryOfBirth:     "LS",
			PreferredLanguage:  "st-ZA",
			Status:             StatusSuspended,
			InboundChannel:     "AGENT_PORTAL",
			ActivationDate:     ds(-1600),
			DateCreated:        dts(-1602, -8),
			DateModified:       dts(-30, 2),
			LimitID:            "LIM_5000",
			Addresses: []Address{{
				StreetAddress: "19 Main Reef Road",
				Suburb:        "Germiston",
				City:          "Johannesburg",
				Province:      "Gauteng",
				PostalCode:    "1401",
				Country:       "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27761234507", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber: "LS449120",
				CountryCode:          "LS",
				CountryOfBirthCode:   "LS",
			}},
			Documents: []Document{
				{
					MediaID: "med-thabo-pp", DocumentName: "passport.jpg",
					DocumentType: DocPassport, DocumentNumber: "LS449120",
					DocumentStatus: DocStatusApproved, DocumentApprovalCode: "AGENT_APPROVED",
					InboundChannel: "AGENT_PORTAL", IssueDate: ds(-1700), ExpiryDate: ds(600),
					IssuingCountry: "LS", CustomerMediaType: "image/jpeg",
					TimeCreated: dts(-1600, 3), ProcessingAgentID: "agent.pieter",
				},
			},
		},

		// Changed SIM: reachable only via a DEPRECATED_MSISDN entry. Search by the
		// old number must still find her.
		{
			MMGlobalCustomerID: guFatima,
			ClaireCustomerID:   "415620",
			FirstName:          "Fatima",
			LastName:           "Banda",
			MSISDN:             "+27831234508",
			DateOfBirth:        "1990-02-21",
			Gender:             "FEMALE",
			CountryOfBirth:     "MW",
			PreferredLanguage:  "en-ZA",
			Status:             StatusActive,
			InboundChannel:     "ANDROID_APP",
			ActivationDate:     ds(-700),
			DateCreated:        dts(-702, -2),
			DateModified:       dts(-64, 1),
			LimitID:            "LIM_25000",
			IncomeID:           "INC_3",
			IncomeSourceType:   "SALARY",
			Addresses: []Address{{
				StreetAddress: "76 Klipfontein Road",
				Suburb:        "Athlone",
				City:          "Cape Town",
				Province:      "Western Cape",
				PostalCode:    "7764",
				Country:       "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27831234508", ContactNumberTypeCode: ContactPrimaryMSISDN},
				{ContactNumber: "+27760099771", ContactNumberTypeCode: ContactDeprecatedMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber: "MW5510338",
				CountryCode:          "MW",
				CountryOfBirthCode:   "MW",
			}},
			Documents: []Document{
				{
					MediaID: "med-fatima-pp", DocumentName: "passport-fatima.jpg",
					DocumentType: DocPassport, DocumentNumber: "MW5510338",
					DocumentStatus: DocStatusApproved, DocumentApprovalCode: "AUTO_VERIFIED",
					InboundChannel: "ANDROID_APP", IssueDate: ds(-1300), ExpiryDate: ds(500),
					IssuingCountry: "MW", CustomerMediaType: "image/jpeg",
					TimeCreated: dts(-700, 2), ProcessingAgentID: "agent.naledi",
				},
			},
		},

		// No claire-customer-id: born after the monolith stopped taking writes.
		{
			MMGlobalCustomerID: guMpho,
			FirstName:          "Mpho",
			LastName:           "Sekhukhune",
			MSISDN:             "+27821234509",
			EmailAddress:       "mpho.sek@gmail.com",
			DateOfBirth:        "2000-08-16",
			Gender:             "FEMALE",
			CountryOfBirth:     "ZA",
			PreferredLanguage:  "en-ZA",
			Status:             StatusActive,
			InboundChannel:     "IOS_APP",
			ActivationDate:     ds(-40),
			DateCreated:        dts(-41, -3),
			DateModified:       dts(-6, 4),
			Addresses: []Address{{
				StreetAddress: "3 Jan Smuts Avenue",
				Suburb:        "Rosebank",
				City:          "Johannesburg",
				Province:      "Gauteng",
				PostalCode:    "2196",
				Country:       "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27821234509", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber: "0008165120084",
				CountryCode:          "ZA",
				CountryOfBirthCode:   "ZA",
			}},
			Documents: []Document{
				{
					MediaID: "med-mpho-id-f", DocumentName: "id-card-front.jpg",
					DocumentType: DocSAIDFront, DocumentNumber: "0008165120084",
					DocumentStatus: DocStatusApproved, DocumentApprovalCode: "AUTO_VERIFIED",
					InboundChannel: "IOS_APP", IssuingCountry: "ZA",
					CustomerMediaType: "image/jpeg", TimeCreated: dts(-41, 1),
				},
				{
					MediaID: "med-mpho-id-b", DocumentName: "id-card-back.jpg",
					DocumentType: DocSAIDBack, DocumentStatus: DocStatusApproved,
					DocumentApprovalCode: "AUTO_VERIFIED", InboundChannel: "IOS_APP",
					IssuingCountry: "ZA", CustomerMediaType: "image/jpeg",
					TimeCreated: dts(-41, 1),
				},
			},
		},

		// Sanctions screening hit.
		{
			MMGlobalCustomerID: guEmmanuel,
			ClaireCustomerID:   "409443",
			FirstName:          "Emmanuel",
			LastName:           "Adeyemi",
			MSISDN:             "+27761234510",
			DateOfBirth:        "1982-04-11",
			Gender:             "MALE",
			CountryOfBirth:     "NG",
			PreferredLanguage:  "en-ZA",
			Status:             StatusBlockedPositiveMatch,
			InboundChannel:     "ANDROID_APP",
			ActivationDate:     ds(-1020),
			DateCreated:        dts(-1022, -6),
			DateModified:       dts(-210, 1),
			LimitID:            "LIM_5000",
			Addresses: []Address{{
				StreetAddress: "44 Commissioner Street",
				Suburb:        "Jeppestown",
				City:          "Johannesburg",
				Province:      "Gauteng",
				PostalCode:    "2094",
				Country:       "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27761234510", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber: "A08841299",
				CountryCode:          "NG",
				CountryOfBirthCode:   "NG",
			}},
			Documents: []Document{
				{
					MediaID: "med-emmanuel-pp", DocumentName: "passport.jpg",
					DocumentType: DocPassport, DocumentNumber: "A08841299",
					DocumentStatus: DocStatusApproved, DocumentApprovalCode: "AGENT_APPROVED",
					InboundChannel: "ANDROID_APP", IssueDate: ds(-1500), ExpiryDate: ds(320),
					IssuingCountry: "NG", CustomerMediaType: "image/jpeg",
					TimeCreated: dts(-1020, 2), ProcessingAgentID: "agent.zanele",
				},
			},
		},

		// Duplicate record: same human as Lerato's live record, kept for lookup.
		{
			MMGlobalCustomerID: guLerato,
			ClaireCustomerID:   "413007",
			FirstName:          "Lerato",
			LastName:           "Motaung",
			MSISDN:             "+27821234511",
			DateOfBirth:        "1996-06-25",
			Gender:             "FEMALE",
			CountryOfBirth:     "ZA",
			PreferredLanguage:  "st-ZA",
			Status:             StatusDuplicate,
			InboundChannel:     "USSD",
			DateCreated:        dts(-880, -2),
			DateModified:       dts(-400, 3),
			Addresses: []Address{{
				Suburb:   "Mamelodi",
				City:     "Pretoria",
				Province: "Gauteng",
				Country:  "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27821234511", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber: "9606250913082",
				CountryCode:          "ZA",
				CountryOfBirthCode:   "ZA",
			}},
		},

		// Permanently blocked for confirmed fraud; shares the fraud-ring device.
		{
			MMGlobalCustomerID: guJoao,
			ClaireCustomerID:   "410558",
			FirstName:          "João",
			LastName:           "Macuácua",
			MSISDN:             "+27831234512",
			DateOfBirth:        "1988-10-09",
			Gender:             "MALE",
			CountryOfBirth:     "MZ",
			PreferredLanguage:  "pt-MZ",
			Status:             StatusPermanentlyBlocked,
			InboundChannel:     "ANDROID_APP",
			ActivationDate:     ds(-960),
			DateCreated:        dts(-962, -4),
			DateModified:       dts(-75, 6),
			LimitID:            "LIM_5000",
			Addresses: []Address{{
				StreetAddress: "12 Rissik Street",
				Suburb:        "Braamfontein",
				City:          "Johannesburg",
				Province:      "Gauteng",
				PostalCode:    "2017",
				Country:       "ZA",
			}},
			ContactNumbers: []ContactNumber{
				{ContactNumber: "+27831234512", ContactNumberTypeCode: ContactPrimaryMSISDN},
			},
			IDNumbers: []IDNumber{{
				IdentificationNumber: "MZ7719044",
				CountryCode:          "MZ",
				CountryOfBirthCode:   "MZ",
			}},
			Documents: []Document{
				{
					MediaID: "med-joao-pp", DocumentName: "passport.jpg",
					DocumentType: DocPassport, DocumentNumber: "MZ7719044",
					DocumentStatus: DocStatusRejected, DocumentApprovalCode: "SUSPECTED_FORGERY",
					InboundChannel: "ANDROID_APP", IssueDate: ds(-1400), ExpiryDate: ds(200),
					IssuingCountry: "MZ", CustomerMediaType: "image/jpeg",
					TimeCreated: dts(-960, 2), ProcessingAgentID: "agent.zanele",
				},
			},
		},
	}
}

// seedBalances covers every product-holding combination, including the
// card-lookup failure that distinguishes "no card" from "card service down".
func seedBalances() map[string]*Balances {
	return map[string]*Balances{
		guTendai:   {Wallet: ptr(1842.55), Card: ptr(430.10), USDC: ptr(215.40), ZAR: ptr(3980.20)},
		guNomvula:  {Wallet: ptr(310.00)},
		guBlessing: {Wallet: ptr(75.25), Card: ptr(0.00)},
		guChipo:    {Wallet: ptr(4210.80), Card: ptr(1120.45), USDC: ptr(88.00), ZAR: ptr(1610.00)},
		guSipho:    {},
		guAmara:    {Wallet: ptr(1250.00), Card: ptr(62.30)},
		guThabo:    {Wallet: ptr(0.00)},
		// Card service returned 502 — UI must show an error, not "no card".
		guFatima: {
			Wallet: ptr(2680.40),
			Errors: map[string]string{"card": "card balance unavailable"},
		},
		guMpho:     {Wallet: ptr(560.00), USDC: ptr(40.00), ZAR: ptr(740.00)},
		guEmmanuel: {Wallet: ptr(1975.00)},
		guLerato:   {},
		guJoao:     {Wallet: ptr(12.60), Card: ptr(0.00)},
	}
}

func seedBankingStatus() map[string]*BankingStatus {
	return map[string]*BankingStatus{
		guTendai:   {OnboardingStatus: "ACTIVE", CardStatus: "ACTIVE"},
		guBlessing: {OnboardingStatus: "ACTIVE", CardStatus: "BLOCKED"},
		guChipo:    {OnboardingStatus: "ACTIVE", CardStatus: "ACTIVE"},
		guAmara:    {OnboardingStatus: "ACTIVE", CardStatus: "ACTIVE"},
		guFatima:   {OnboardingStatus: "REGISTRATION_FAILED", CardStatus: "NONE"},
		guNomvula:  {OnboardingStatus: "NOT_ONBOARDED", CardStatus: "NONE"},
		guMpho:     {OnboardingStatus: "PENDING", CardStatus: "PENDING_ALLOCATION"},
		guJoao:     {OnboardingStatus: "ACTIVE", CardStatus: "BLOCKED"},
		guThabo:    {OnboardingStatus: "NOT_ONBOARDED", CardStatus: "NONE"},
		guEmmanuel: {OnboardingStatus: "SUSPENDED", CardStatus: "BLOCKED"},
	}
}

func seedBankingCustomers() map[string]*BankingCustomer {
	return map[string]*BankingCustomer{
		guTendai:   {CustomerID: "UML-8842001", CardSequenceNumber: "5412990001", RegistrationStatus: "REGISTERED"},
		guBlessing: {CustomerID: "UML-8842003", CardSequenceNumber: "5412990017", RegistrationStatus: "REGISTERED"},
		guChipo:    {CustomerID: "UML-8842004", CardSequenceNumber: "5412990022", RegistrationStatus: "REGISTERED"},
		guAmara:    {CustomerID: "UML-8842006", CardSequenceNumber: "5412990048", RegistrationStatus: "REGISTERED"},
		guJoao:     {CustomerID: "UML-8842012", CardSequenceNumber: "5412990091", RegistrationStatus: "REGISTERED"},
		guEmmanuel: {CustomerID: "UML-8842010", CardSequenceNumber: "5412990066", RegistrationStatus: "SUSPENDED"},
		// Banking-side onboarding failure: the record exists but never registered.
		guFatima: {
			CustomerID:                   "UML-8842008",
			RegistrationStatus:           "FAILED",
			RegistrationErrorCode:        "KYC_MISMATCH_04",
			RegistrationErrorDescription: "Surname on FICA document does not match Home Affairs record",
		},
		guMpho: {
			CustomerID:                   "UML-8842009",
			RegistrationStatus:           "PENDING",
			RegistrationErrorCode:        "CARD_STOCK_02",
			RegistrationErrorDescription: "Card allocation deferred: no stock at issuing branch",
		},
	}
}

func seedBankingDetails() map[string]*BankingDetails {
	return map[string]*BankingDetails{
		guTendai:   {AccountNumber: "62884120014", AccountType: "TRANSACTIONAL", BankName: "MamaMoney Banking", BranchCode: "470010"},
		guBlessing: {AccountNumber: "62884120039", AccountType: "TRANSACTIONAL", BankName: "MamaMoney Banking", BranchCode: "470010"},
		guChipo:    {AccountNumber: "62884120047", AccountType: "TRANSACTIONAL", BankName: "MamaMoney Banking", BranchCode: "470010"},
		guAmara:    {AccountNumber: "62884120088", AccountType: "TRANSACTIONAL", BankName: "MamaMoney Banking", BranchCode: "470010"},
		guJoao:     {AccountNumber: "62884120131", AccountType: "TRANSACTIONAL", BankName: "MamaMoney Banking", BranchCode: "470010"},
	}
}

func seedEligibility() map[string]*BankingEligibility {
	return map[string]*BankingEligibility{
		guNomvula:  {Eligible: true},
		guMpho:     {Eligible: true},
		guSipho:    {Eligible: false, IneligibleReason: "Customer is not FICA verified"},
		guThabo:    {Eligible: false, IneligibleReason: "Customer status is SUSPENDED"},
		guEmmanuel: {Eligible: false, IneligibleReason: "Customer is on a sanctions list"},
		guLerato:   {Eligible: false, IneligibleReason: "Duplicate customer record"},
		guJoao:     {Eligible: false, IneligibleReason: "Customer is permanently blocked"},
	}
}

func seedRiskMatrix() map[string]*RiskMatrix {
	return map[string]*RiskMatrix{
		guTendai:   {Score: 2, Description: "Low", SetBy: "risk.engine", SetAt: dts(-30, 0)},
		guNomvula:  {Score: 1, Description: "Very low", SetBy: "risk.engine", SetAt: dts(-25, 0)},
		guBlessing: {Score: 4, Description: "Elevated", SetBy: "agent.zanele", SetAt: dts(-14, 0)},
		guChipo:    {Score: 3, Description: "Medium", SetBy: "risk.engine", SetAt: dts(-60, 0)},
		guSipho:    {Score: 1, Description: "Very low", SetBy: "risk.engine", SetAt: dts(-58, 0)},
		guAmara:    {Score: 5, Description: "High", SetBy: "agent.zanele", SetAt: dts(-2, 0)},
		guThabo:    {Score: 4, Description: "Elevated", SetBy: "agent.pieter", SetAt: dts(-30, 0)},
		guFatima:   {Score: 2, Description: "Low", SetBy: "risk.engine", SetAt: dts(-64, 0)},
		guMpho:     {Score: 1, Description: "Very low", SetBy: "risk.engine", SetAt: dts(-40, 0)},
		guEmmanuel: {Score: 6, Description: "Prohibited", SetBy: "compliance.screening", SetAt: dts(-210, 0)},
		guLerato:   {Score: 3, Description: "Medium", SetBy: "risk.engine", SetAt: dts(-400, 0)},
		guJoao:     {Score: 6, Description: "Prohibited", SetBy: "compliance.fraud", SetAt: dts(-75, 0)},
	}
}

func seedMonthlyLimits() map[string]*MonthlyLimit {
	return map[string]*MonthlyLimit{
		"LIM_5000":  {LimitID: "LIM_5000", Description: "Standard FICA (R5 000/month)", Amount: 5000},
		"LIM_25000": {LimitID: "LIM_25000", Description: "Enhanced FICA (R25 000/month)", Amount: 25000},
		"LIM_50000": {LimitID: "LIM_50000", Description: "Full FICA (R50 000/month)", Amount: 50000},
	}
}

// seedLimitBalances includes several near-exhausted customers — the case where
// an agent has to tell someone their next transfer will bounce.
func seedLimitBalances() map[string]*LimitBalance {
	return map[string]*LimitBalance{
		guTendai:   {MonthlyLimit: 25000, Used: 8400, Remaining: 16600},
		guNomvula:  {MonthlyLimit: 5000, Used: 4820, Remaining: 180},
		guBlessing: {MonthlyLimit: 5000, Used: 4995, Remaining: 5},
		guChipo:    {MonthlyLimit: 25000, Used: 23150, Remaining: 1850},
		guSipho:    {MonthlyLimit: 5000, Used: 0, Remaining: 5000},
		guAmara:    {MonthlyLimit: 5000, Used: 1600, Remaining: 3400},
		guThabo:    {MonthlyLimit: 5000, Used: 0, Remaining: 5000},
		guFatima:   {MonthlyLimit: 25000, Used: 11200, Remaining: 13800},
		guMpho:     {MonthlyLimit: 5000, Used: 950, Remaining: 4050},
		guEmmanuel: {MonthlyLimit: 5000, Used: 5000, Remaining: 0},
		guLerato:   {MonthlyLimit: 5000, Used: 0, Remaining: 5000},
		guJoao:     {MonthlyLimit: 5000, Used: 3300, Remaining: 1700},
	}
}

func seedIncomeRanges() []IncomeRange {
	return []IncomeRange{
		{IncomeID: "INC_1", Range: "R0 - R3 000"},
		{IncomeID: "INC_2", Range: "R3 001 - R8 000"},
		{IncomeID: "INC_3", Range: "R8 001 - R15 000"},
		{IncomeID: "INC_4", Range: "R15 001 - R30 000"},
		{IncomeID: "INC_5", Range: "R30 001+"},
	}
}

func seedIncomeSources() []IncomeSource {
	return []IncomeSource{
		{Type: "SALARY", Description: "Salary or wages"},
		{Type: "SELF_EMPLOYED", Description: "Own business"},
		{Type: "INFORMAL_TRADING", Description: "Informal trading"},
		{Type: "PENSION", Description: "Pension or annuity"},
		{Type: "GRANT", Description: "Social grant"},
		{Type: "REMITTANCE_RECEIVED", Description: "Money received from family"},
	}
}

// seedOrders spans several months, all four products and every payment method.
// Source distinguishes UOPS-native orders from ones mapped out of Claire.
func seedOrders() []Order {
	return []Order{
		// Tendai: long-standing remitter, one late payment.
		{OrderID: "UOPS-900101", MMGlobalCustomerID: guTendai, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 2500, FeeAmount: 50, OrderReferenceNumber: "MM2500A1", OrderStatus: OrderPaid, TimeCreated: dts(-150, 1), TimeUpdated: dts(-150, 3), Source: "uops"},
		{OrderID: "UOPS-900102", MMGlobalCustomerID: guTendai, Product: ProductRemittance, PaymentMethod: PayBillPayment, Amount: 1800, FeeAmount: 40, OrderReferenceNumber: "MM1800B2", OrderStatus: OrderPaid, LatePayment: true, TimeCreated: dts(-118, 2), TimeUpdated: dts(-114, 9), Source: "uops"},
		{OrderID: "UOPS-900103", MMGlobalCustomerID: guTendai, Product: ProductBanking, PaymentMethod: PayBanking, Amount: 3200, OrderReferenceNumber: "MMBK3200", OrderStatus: OrderPaid, TimeCreated: dts(-84, 4), TimeUpdated: dts(-84, 4), Source: "uops"},
		{OrderID: "UOPS-900104", MMGlobalCustomerID: guTendai, Product: ProductWallet, PaymentMethod: PayWallet, Amount: 900, OrderReferenceNumber: "MMWL0900", OrderStatus: OrderPaid, TimeCreated: dts(-52, 6), TimeUpdated: dts(-52, 6), Source: "uops"},
		{OrderID: "UOPS-900105", MMGlobalCustomerID: guTendai, Product: ProductUSDSavings, PaymentMethod: PayUSDSavings, Amount: 1500, FeeAmount: 15, OrderReferenceNumber: "MMUSD1500", OrderStatus: OrderPaid, TimeCreated: dts(-21, 2), TimeUpdated: dts(-21, 2), Source: "uops"},
		{OrderID: "UOPS-900106", MMGlobalCustomerID: guTendai, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 2200, FeeAmount: 45, OrderReferenceNumber: "MM2200C3", OrderStatus: OrderActive, TimeCreated: dts(-2, 3), TimeUpdated: dts(-2, 3), Source: "uops"},
		{OrderID: "CLR-771201", MMGlobalCustomerID: guTendai, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 4800, FeeAmount: 90, OrderReferenceNumber: "CL4800X1", OrderStatus: OrderPaid, TimeCreated: dts(-320, 5), TimeUpdated: dts(-319, 2), Source: "claire"},

		// Nomvula: near limit, one cancelled.
		{OrderID: "UOPS-900201", MMGlobalCustomerID: guNomvula, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 1200, FeeAmount: 30, OrderReferenceNumber: "MM1200D4", OrderStatus: OrderPaid, TimeCreated: dts(-95, 2), TimeUpdated: dts(-95, 5), Source: "uops"},
		{OrderID: "UOPS-900202", MMGlobalCustomerID: guNomvula, Product: ProductRemittance, PaymentMethod: PayBillPayment, Amount: 650, FeeAmount: 20, OrderReferenceNumber: "MM0650E5", OrderStatus: OrderCancelled, TimeCreated: dts(-40, 1), TimeUpdated: dts(-39, 1), Source: "uops"},
		{OrderID: "UOPS-900203", MMGlobalCustomerID: guNomvula, Product: ProductWallet, PaymentMethod: PayWallet, Amount: 310, OrderReferenceNumber: "MMWL0310", OrderStatus: OrderActive, TimeCreated: dts(-3, 7), TimeUpdated: dts(-3, 7), Source: "uops"},
		{OrderID: "CLR-771388", MMGlobalCustomerID: guNomvula, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 2970, FeeAmount: 60, OrderReferenceNumber: "CL2970Y2", OrderStatus: OrderPaid, TimeCreated: dts(-210, 3), TimeUpdated: dts(-209, 8), Source: "claire"},

		// Blessing: limit all but exhausted, refund on record.
		{OrderID: "UOPS-900301", MMGlobalCustomerID: guBlessing, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 3500, FeeAmount: 70, OrderReferenceNumber: "MM3500F6", OrderStatus: OrderPaid, TimeCreated: dts(-27, 2), TimeUpdated: dts(-27, 6), Source: "uops"},
		{OrderID: "UOPS-900302", MMGlobalCustomerID: guBlessing, Product: ProductRemittance, PaymentMethod: PayRemittanceRefund, Amount: 490, OrderReferenceNumber: "MMRF0490", OrderStatus: OrderPaid, TimeCreated: dts(-19, 3), TimeUpdated: dts(-18, 2), Source: "uops"},
		{OrderID: "UOPS-900303", MMGlobalCustomerID: guBlessing, Product: ProductBanking, PaymentMethod: PayBanking, Amount: 1005, OrderReferenceNumber: "MMBK1005", OrderStatus: OrderPaid, LatePayment: true, TimeCreated: dts(-8, 1), TimeUpdated: dts(-5, 4), Source: "uops"},
		{OrderID: "UOPS-900304", MMGlobalCustomerID: guBlessing, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 150, FeeAmount: 10, OrderReferenceNumber: "MM0150G7", OrderStatus: OrderActive, TimeCreated: dts(-1, 2), TimeUpdated: dts(-1, 2), Source: "uops"},

		// Chipo: high-volume, USD savings user.
		{OrderID: "UOPS-900401", MMGlobalCustomerID: guChipo, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 5000, FeeAmount: 95, OrderReferenceNumber: "MM5000H8", OrderStatus: OrderPaid, TimeCreated: dts(-172, 1), TimeUpdated: dts(-172, 4), Source: "uops"},
		{OrderID: "UOPS-900402", MMGlobalCustomerID: guChipo, Product: ProductUSDSavings, PaymentMethod: PayUSDSavings, Amount: 4000, FeeAmount: 40, OrderReferenceNumber: "MMUSD4000", OrderStatus: OrderPaid, TimeCreated: dts(-140, 2), TimeUpdated: dts(-140, 2), Source: "uops"},
		{OrderID: "UOPS-900403", MMGlobalCustomerID: guChipo, Product: ProductBanking, PaymentMethod: PayBanking, Amount: 2650, OrderReferenceNumber: "MMBK2650", OrderStatus: OrderPaid, TimeCreated: dts(-66, 5), TimeUpdated: dts(-66, 5), Source: "uops"},
		{OrderID: "UOPS-900404", MMGlobalCustomerID: guChipo, Product: ProductWallet, PaymentMethod: PayWallet, Amount: 1400, OrderReferenceNumber: "MMWL1400", OrderStatus: OrderPaid, TimeCreated: dts(-33, 3), TimeUpdated: dts(-33, 3), Source: "uops"},
		{OrderID: "UOPS-900405", MMGlobalCustomerID: guChipo, Product: ProductRemittance, PaymentMethod: PayBillPayment, Amount: 3800, FeeAmount: 75, OrderReferenceNumber: "MM3800I9", OrderStatus: OrderActive, LatePayment: true, TimeCreated: dts(-6, 2), TimeUpdated: dts(-6, 2), Source: "uops"},
		{OrderID: "CLR-770044", MMGlobalCustomerID: guChipo, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 4500, FeeAmount: 85, OrderReferenceNumber: "CL4500Z3", OrderStatus: OrderPaid, TimeCreated: dts(-540, 4), TimeUpdated: dts(-539, 1), Source: "claire"},

		// Amara: screening hold froze an active order.
		{OrderID: "UOPS-900601", MMGlobalCustomerID: guAmara, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 1600, FeeAmount: 35, OrderReferenceNumber: "MM1600J1", OrderStatus: OrderPaid, TimeCreated: dts(-58, 3), TimeUpdated: dts(-58, 7), Source: "uops"},
		{OrderID: "UOPS-900602", MMGlobalCustomerID: guAmara, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 2400, FeeAmount: 48, OrderReferenceNumber: "MM2400K2", OrderStatus: OrderActive, TimeCreated: dts(-1, 4), TimeUpdated: dts(-1, 4), Source: "uops"},
		{OrderID: "UOPS-900603", MMGlobalCustomerID: guAmara, Product: ProductBanking, PaymentMethod: PayBanking, Amount: 780, OrderReferenceNumber: "MMBK0780", OrderStatus: OrderPaid, TimeCreated: dts(-15, 6), TimeUpdated: dts(-15, 6), Source: "uops"},

		// Thabo: everything cancelled when he was suspended.
		{OrderID: "CLR-768812", MMGlobalCustomerID: guThabo, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 1950, FeeAmount: 42, OrderReferenceNumber: "CL1950W4", OrderStatus: OrderPaid, TimeCreated: dts(-400, 2), TimeUpdated: dts(-399, 5), Source: "claire"},
		{OrderID: "UOPS-900701", MMGlobalCustomerID: guThabo, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 2100, FeeAmount: 44, OrderReferenceNumber: "MM2100L3", OrderStatus: OrderCancelled, TimeCreated: dts(-31, 1), TimeUpdated: dts(-30, 2), Source: "uops"},

		// Fatima: regular remitter to Malawi.
		{OrderID: "UOPS-900801", MMGlobalCustomerID: guFatima, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 2800, FeeAmount: 55, OrderReferenceNumber: "MM2800M4", OrderStatus: OrderPaid, TimeCreated: dts(-120, 2), TimeUpdated: dts(-120, 5), Source: "uops"},
		{OrderID: "UOPS-900802", MMGlobalCustomerID: guFatima, Product: ProductRemittance, PaymentMethod: PayBillPayment, Amount: 3400, FeeAmount: 68, OrderReferenceNumber: "MM3400N5", OrderStatus: OrderPaid, TimeCreated: dts(-64, 1), TimeUpdated: dts(-64, 4), Source: "uops"},
		{OrderID: "UOPS-900803", MMGlobalCustomerID: guFatima, Product: ProductWallet, PaymentMethod: PayWallet, Amount: 1000, OrderReferenceNumber: "MMWL1000", OrderStatus: OrderPaid, TimeCreated: dts(-11, 3), TimeUpdated: dts(-11, 3), Source: "uops"},
		{OrderID: "UOPS-900804", MMGlobalCustomerID: guFatima, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 2000, FeeAmount: 42, OrderReferenceNumber: "MM2000O6", OrderStatus: OrderActive, TimeCreated: dts(-4, 8), TimeUpdated: dts(-4, 8), Source: "uops"},

		// Mpho: brand new, UOPS only.
		{OrderID: "UOPS-900901", MMGlobalCustomerID: guMpho, Product: ProductWallet, PaymentMethod: PayWallet, Amount: 400, OrderReferenceNumber: "MMWL0400", OrderStatus: OrderPaid, TimeCreated: dts(-38, 2), TimeUpdated: dts(-38, 2), Source: "uops"},
		{OrderID: "UOPS-900902", MMGlobalCustomerID: guMpho, Product: ProductUSDSavings, PaymentMethod: PayUSDSavings, Amount: 550, FeeAmount: 8, OrderReferenceNumber: "MMUSD0550", OrderStatus: OrderPaid, TimeCreated: dts(-20, 4), TimeUpdated: dts(-20, 4), Source: "uops"},
		{OrderID: "UOPS-900903", MMGlobalCustomerID: guMpho, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 950, FeeAmount: 25, OrderReferenceNumber: "MM0950P7", OrderStatus: OrderActive, TimeCreated: dts(-5, 1), TimeUpdated: dts(-5, 1), Source: "uops"},

		// Emmanuel: cut off at the screening hit.
		{OrderID: "CLR-769950", MMGlobalCustomerID: guEmmanuel, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 4200, FeeAmount: 82, OrderReferenceNumber: "CL4200V5", OrderStatus: OrderPaid, TimeCreated: dts(-260, 3), TimeUpdated: dts(-259, 6), Source: "claire"},
		{OrderID: "UOPS-901001", MMGlobalCustomerID: guEmmanuel, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 4900, FeeAmount: 94, OrderReferenceNumber: "MM4900Q8", OrderStatus: OrderCancelled, TimeCreated: dts(-211, 2), TimeUpdated: dts(-210, 1), Source: "uops"},

		// João: fraud-ring activity, refund and cancellations.
		{OrderID: "UOPS-901201", MMGlobalCustomerID: guJoao, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 4950, FeeAmount: 96, OrderReferenceNumber: "MM4950R9", OrderStatus: OrderCancelled, TimeCreated: dts(-78, 1), TimeUpdated: dts(-75, 6), Source: "uops"},
		{OrderID: "UOPS-901202", MMGlobalCustomerID: guJoao, Product: ProductRemittance, PaymentMethod: PayRemittanceRefund, Amount: 4950, OrderReferenceNumber: "MMRF4950", OrderStatus: OrderPaid, TimeCreated: dts(-75, 7), TimeUpdated: dts(-74, 2), Source: "uops"},
		{OrderID: "UOPS-901203", MMGlobalCustomerID: guJoao, Product: ProductBanking, PaymentMethod: PayBanking, Amount: 1300, OrderReferenceNumber: "MMBK1300", OrderStatus: OrderCancelled, LatePayment: true, TimeCreated: dts(-77, 4), TimeUpdated: dts(-75, 6), Source: "uops"},

		// Lerato: the duplicate record holds one stale Claire order.
		{OrderID: "CLR-767700", MMGlobalCustomerID: guLerato, Product: ProductRemittance, PaymentMethod: PayEFT, Amount: 700, FeeAmount: 22, OrderReferenceNumber: "CL0700U6", OrderStatus: OrderPaid, TimeCreated: dts(-860, 2), TimeUpdated: dts(-859, 4), Source: "claire"},
	}
}

// seedWalletTransactions is a real ledger: Balance is the running total after
// each entry, per customer, and it reconciles with seedBalances().
// fakeWalletTx pairs a ledger entry with its owning customer; the upstream
// WalletTransaction has no customer field because the live endpoint is already
// scoped to one.
type fakeWalletTx struct {
	owner string
	tx    WalletTransaction
}

func seedWalletTransactions() []fakeWalletTx {
	return []fakeWalletTx{
		// Tendai → 1842.55
		{owner: guTendai, tx: WalletTransaction{TransactionID: "WTX-3001", Amount: 3000.00, Balance: 3000.00, Type: "CREDIT", Description: "EFT deposit - Capitec", Reference: "MMTENDAI01", At: dt(-52, 5)}},
		{owner: guTendai, tx: WalletTransaction{TransactionID: "WTX-3002", Amount: -900.00, Balance: 2100.00, Type: "DEBIT", Description: "Wallet transfer out", Reference: "MMWL0900", At: dt(-52, 6)}},
		{owner: guTendai, tx: WalletTransaction{TransactionID: "WTX-3003", Amount: -207.45, Balance: 1892.55, Type: "DEBIT", Description: "Card load", Reference: "MMCL0207", At: dt(-30, 2)}},
		{owner: guTendai, tx: WalletTransaction{TransactionID: "WTX-3004", Amount: -50.00, Balance: 1842.55, Type: "FEE", Description: "Remittance fee", Reference: "MM2200C3", At: dt(-2, 3)}},

		// Nomvula → 310.00
		{owner: guNomvula, tx: WalletTransaction{TransactionID: "WTX-3101", Amount: 1500.00, Balance: 1500.00, Type: "CREDIT", Description: "EFT deposit - FNB", Reference: "MMNOMVULA1", At: dt(-40, 2)}},
		{owner: guNomvula, tx: WalletTransaction{TransactionID: "WTX-3102", Amount: -1200.00, Balance: 300.00, Type: "DEBIT", Description: "Remittance to Zimbabwe", Reference: "MM1200D4", At: dt(-95, 2)}},
		{owner: guNomvula, tx: WalletTransaction{TransactionID: "WTX-3103", Amount: 20.00, Balance: 310.00, Type: "CREDIT", Description: "Cancelled order refund", Reference: "MM0650E5", At: dt(-39, 1)}},

		// Blessing → 75.25
		{owner: guBlessing, tx: WalletTransaction{TransactionID: "WTX-3201", Amount: 500.00, Balance: 500.00, Type: "CREDIT", Description: "Branch deposit - Standard Bank", Reference: "MMBLESS01", At: dt(-27, 1)}},
		{owner: guBlessing, tx: WalletTransaction{TransactionID: "WTX-3202", Amount: -424.75, Balance: 75.25, Type: "DEBIT", Description: "Card load", Reference: "MMCL0424", At: dt(-8, 1)}},

		// Chipo → 4210.80
		{owner: guChipo, tx: WalletTransaction{TransactionID: "WTX-3301", Amount: 6000.00, Balance: 6000.00, Type: "CREDIT", Description: "EFT deposit - Nedbank", Reference: "MMCHIPO01", At: dt(-35, 1)}},
		{owner: guChipo, tx: WalletTransaction{TransactionID: "WTX-3302", Amount: -1400.00, Balance: 4600.00, Type: "DEBIT", Description: "Wallet transfer out", Reference: "MMWL1400", At: dt(-33, 3)}},
		{owner: guChipo, tx: WalletTransaction{TransactionID: "WTX-3303", Amount: -314.20, Balance: 4285.80, Type: "DEBIT", Description: "USD savings top-up", Reference: "MMUSD4000", At: dt(-20, 2)}},
		{owner: guChipo, tx: WalletTransaction{TransactionID: "WTX-3304", Amount: -75.00, Balance: 4210.80, Type: "FEE", Description: "Remittance fee", Reference: "MM3800I9", At: dt(-6, 2)}},

		// Amara → 1250.00
		{owner: guAmara, tx: WalletTransaction{TransactionID: "WTX-3401", Amount: 2000.00, Balance: 2000.00, Type: "CREDIT", Description: "ATM deposit - Absa", Reference: "MMAMARA01", At: dt(-16, 3)}},
		{owner: guAmara, tx: WalletTransaction{TransactionID: "WTX-3402", Amount: -750.00, Balance: 1250.00, Type: "DEBIT", Description: "Card load", Reference: "MMCL0750", At: dt(-15, 6)}},

		// Fatima → 2680.40
		{owner: guFatima, tx: WalletTransaction{TransactionID: "WTX-3501", Amount: 4000.00, Balance: 4000.00, Type: "CREDIT", Description: "EFT deposit - TymeBank", Reference: "MMFATIMA01", At: dt(-12, 2)}},
		{owner: guFatima, tx: WalletTransaction{TransactionID: "WTX-3502", Amount: -1000.00, Balance: 3000.00, Type: "DEBIT", Description: "Wallet transfer out", Reference: "MMWL1000", At: dt(-11, 3)}},
		{owner: guFatima, tx: WalletTransaction{TransactionID: "WTX-3503", Amount: -277.60, Balance: 2722.40, Type: "DEBIT", Description: "Airtime purchase", Reference: "MMAIR0277", At: dt(-7, 4)}},
		{owner: guFatima, tx: WalletTransaction{TransactionID: "WTX-3504", Amount: -42.00, Balance: 2680.40, Type: "FEE", Description: "Remittance fee", Reference: "MM2000O6", At: dt(-4, 8)}},

		// Mpho → 560.00
		{owner: guMpho, tx: WalletTransaction{TransactionID: "WTX-3601", Amount: 1000.00, Balance: 1000.00, Type: "CREDIT", Description: "EFT deposit - Capitec", Reference: "MMMPHO01", At: dt(-39, 1)}},
		{owner: guMpho, tx: WalletTransaction{TransactionID: "WTX-3602", Amount: -400.00, Balance: 600.00, Type: "DEBIT", Description: "Wallet transfer out", Reference: "MMWL0400", At: dt(-38, 2)}},
		{owner: guMpho, tx: WalletTransaction{TransactionID: "WTX-3603", Amount: -40.00, Balance: 560.00, Type: "DEBIT", Description: "USD savings top-up", Reference: "MMUSD0550", At: dt(-20, 4)}},

		// Emmanuel → 1975.00 (frozen when he was blocked)
		{owner: guEmmanuel, tx: WalletTransaction{TransactionID: "WTX-3701", Amount: 1975.00, Balance: 1975.00, Type: "CREDIT", Description: "Cancelled remittance reversal", Reference: "MM4900Q8", At: dt(-210, 1)}},

		// João → 12.60
		{owner: guJoao, tx: WalletTransaction{TransactionID: "WTX-3801", Amount: 4950.00, Balance: 4950.00, Type: "CREDIT", Description: "EFT deposit - FNB", Reference: "MMJOAO01", At: dt(-78, 1)}},
		{owner: guJoao, tx: WalletTransaction{TransactionID: "WTX-3802", Amount: -4950.00, Balance: 0.00, Type: "DEBIT", Description: "Refund to source (fraud hold)", Reference: "MMRF4950", At: dt(-75, 7)}},
		{owner: guJoao, tx: WalletTransaction{TransactionID: "WTX-3803", Amount: 12.60, Balance: 12.60, Type: "CREDIT", Description: "Fee reversal", Reference: "MMFR0012", At: dt(-74, 2)}},

		// Thabo → 0.00
		{owner: guThabo, tx: WalletTransaction{TransactionID: "WTX-3901", Amount: 2100.00, Balance: 2100.00, Type: "CREDIT", Description: "EFT deposit - Absa", Reference: "MMTHABO01", At: dt(-31, 1)}},
		{owner: guThabo, tx: WalletTransaction{TransactionID: "WTX-3902", Amount: -2100.00, Balance: 0.00, Type: "DEBIT", Description: "Reversal on suspension", Reference: "MM2100L3", At: dt(-30, 2)}},
	}
}

func seedEFTNotifications() []EFTNotification {
	return []EFTNotification{
		{EFTNotificationID: 55001, OriginalReference: "MMTENDAI01", Amount: 3000.00, PaymentChannel: "EFT", Bank: "Capitec", ProcessOutcome: EFTOrderPaid, DateReceived: dt(-52, 5), MMGlobalCustomerID: guTendai},
		{EFTNotificationID: 55002, OriginalReference: "MMNOMVULA1", Amount: 1500.00, PaymentChannel: "EFT", Bank: "FNB", ProcessOutcome: EFTWalletOrderCreated, DateReceived: dt(-40, 2), MMGlobalCustomerID: guNomvula},
		{EFTNotificationID: 55003, OriginalReference: "MMBLESS01", Amount: 500.00, PaymentChannel: "BRANCH", Bank: "Standard Bank", ProcessOutcome: EFTOrderPaid, DateReceived: dt(-27, 1), MMGlobalCustomerID: guBlessing},
		{EFTNotificationID: 55004, OriginalReference: "MMCHIPO01", Amount: 6000.00, PaymentChannel: "EFT", Bank: "Nedbank", ProcessOutcome: EFTManualIntervention, DateReceived: dt(-35, 1), MMGlobalCustomerID: guChipo},
		{EFTNotificationID: 55005, OriginalReference: "MMAMARA01", Amount: 2000.00, PaymentChannel: "ATM", Bank: "Absa", ProcessOutcome: EFTPendingProcessing, DateReceived: dt(-16, 3), MMGlobalCustomerID: guAmara},
		{EFTNotificationID: 55006, OriginalReference: "MMFATIMA01", Amount: 4000.00, PaymentChannel: "EFT", Bank: "TymeBank", ProcessOutcome: EFTOrderPaid, DateReceived: dt(-12, 2), MMGlobalCustomerID: guFatima},
		{EFTNotificationID: 55007, OriginalReference: "MMMPHO01", Amount: 1000.00, PaymentChannel: "EFT", Bank: "Capitec", ProcessOutcome: EFTPendingProcessing, DateReceived: dt(-39, 1), MMGlobalCustomerID: guMpho},
		{EFTNotificationID: 55008, OriginalReference: "MMJOAO01", Amount: 4950.00, PaymentChannel: "EFT", Bank: "FNB", ProcessOutcome: EFTManualIntervention, DateReceived: dt(-78, 1), MMGlobalCustomerID: guJoao},
		{EFTNotificationID: 55009, OriginalReference: "MMTHABO01", Amount: 2100.00, PaymentChannel: "BRANCH", Bank: "Absa", ProcessOutcome: EFTPurged, DateReceived: dt(-31, 1), MMGlobalCustomerID: guThabo},
		{EFTNotificationID: 55010, OriginalReference: "MMCL0424", Amount: 424.75, PaymentChannel: "EFT", Bank: "Standard Bank", ProcessOutcome: EFTManualOrderPaid, DateReceived: dt(-8, 1), MMGlobalCustomerID: guBlessing},

		// Unmatched: reference typo'd or absent, so no customer was resolved.
		{EFTNotificationID: 55101, OriginalReference: "MM TENDAI 01", Amount: 850.00, PaymentChannel: "EFT", Bank: "Capitec", ProcessOutcome: EFTManualIntervention, DateReceived: dt(-9, 4)},
		{EFTNotificationID: 55102, OriginalReference: "NOREF", Amount: 1750.00, PaymentChannel: "BRANCH", Bank: "Nedbank", ProcessOutcome: EFTManualIntervention, DateReceived: dt(-7, 2)},
		{EFTNotificationID: 55103, OriginalReference: "0821234599", Amount: 300.00, PaymentChannel: "ATM", Bank: "TymeBank", ProcessOutcome: EFTPendingProcessing, DateReceived: dt(-5, 6)},
		{EFTNotificationID: 55104, OriginalReference: "MAMA MONEY", Amount: 2400.00, PaymentChannel: "EFT", Bank: "FNB", ProcessOutcome: EFTManualIntervention, DateReceived: dt(-3, 1)},
		{EFTNotificationID: 55105, OriginalReference: "SALARY JULY", Amount: 4500.00, PaymentChannel: "EFT", Bank: "Absa", ProcessOutcome: EFTPendingProcessing, DateReceived: dt(-2, 8)},
		{EFTNotificationID: 55106, OriginalReference: "MMCHIP0", Amount: 1200.00, PaymentChannel: "EFT", Bank: "Standard Bank", ProcessOutcome: EFTManualIntervention, DateReceived: dt(-1, 3)},
	}
}

// seedDevices includes the shared-device fraud ring the device blocker exists
// to contain: one handset registered against three customers.
func seedDevices() []Device {
	return []Device{
		{DeviceID: "dev-a1b2c3d4e5", DeviceStatus: DeviceActive, LinkedCustomers: []string{guTendai}, FirstSeen: dts(-820, 1), LastSeen: dts(-2, 3)},
		{DeviceID: "dev-f6g7h8i9j0", DeviceStatus: DeviceActive, LinkedCustomers: []string{guNomvula}, FirstSeen: dts(-310, 2), LastSeen: dts(-3, 7)},
		{DeviceID: "dev-fraudring01", DeviceStatus: DeviceActive, LinkedCustomers: []string{guJoao, guEmmanuel, guBlessing}, FirstSeen: dts(-980, 4), LastSeen: dts(-1, 5)},
		{DeviceID: "dev-blocked9x9x", DeviceStatus: DeviceBlocked, LinkedCustomers: []string{guThabo}, FirstSeen: dts(-1600, 3), LastSeen: dts(-30, 2)},
		{DeviceID: "dev-k1l2m3n4o5", DeviceStatus: DeviceActive, LinkedCustomers: []string{guChipo, guFatima}, FirstSeen: dts(-700, 2), LastSeen: dts(-4, 8)},
		{DeviceID: "dev-p6q7r8s9t0", DeviceStatus: DeviceActive, LinkedCustomers: []string{guMpho}, FirstSeen: dts(-41, 1), LastSeen: dts(-5, 1)},
		{DeviceID: "dev-u1v2w3x4y5", DeviceStatus: DeviceActive, LinkedCustomers: []string{guAmara}, FirstSeen: dts(-96, 2), LastSeen: dts(-1, 4)},
	}
}

func seedVouchers() []fakeVoucher {
	return []fakeVoucher{
		{owner: guTendai, v: Voucher{
			Code: "MMV-4821-7734-0091", Amount: 500, Currency: "ZAR", Status: "ACTIVE", Product: "GROCERY",
			Recipient: VoucherRecipient{Name: "Rudo Mukwazhi", MSISDN: "+263772881044"},
			CreatedAt: dt(-14, 2), ExpiresAt: ptr(dt(76, 2)),
		}},
		{owner: guTendai, v: Voucher{
			Code: "MMV-4821-7734-0092", Amount: 250, Currency: "ZAR", Status: "REDEEMED", Product: "AIRTIME",
			Recipient: VoucherRecipient{Name: "Rudo Mukwazhi", MSISDN: "+263772881044"},
			CreatedAt: dt(-60, 1), RedeemedAt: ptr(dt(-58, 5)), ExpiresAt: ptr(dt(30, 1)),
		}},
		{owner: guChipo, v: Voucher{
			Code: "MMV-5510-2288-0011", Amount: 1000, Currency: "ZAR", Status: "ACTIVE", Product: "CASH_PICKUP",
			Recipient: VoucherRecipient{Name: "Tarisai Nyathi", MSISDN: "+263712004411", Email: "tarisai@example.co.zw"},
			CreatedAt: dt(-6, 3), ExpiresAt: ptr(dt(84, 3)),
		}},
		{owner: guChipo, v: Voucher{
			Code: "MMV-5510-2288-0012", Amount: 750, Currency: "ZAR", Status: "EXPIRED", Product: "GROCERY",
			Recipient: VoucherRecipient{Name: "Tarisai Nyathi", MSISDN: "+263712004411"},
			CreatedAt: dt(-200, 1), ExpiresAt: ptr(dt(-110, 1)),
		}},
		{owner: guFatima, v: Voucher{
			Code: "MMV-6602-9911-0044", Amount: 400, Currency: "ZAR", Status: "REDEEMED", Product: "AIRTIME",
			Recipient: VoucherRecipient{Name: "Grace Banda", MSISDN: "+265991773200"},
			CreatedAt: dt(-45, 2), RedeemedAt: ptr(dt(-44, 6)), ExpiresAt: ptr(dt(45, 2)),
		}},
		{owner: guJoao, v: Voucher{
			Code: "MMV-7714-3355-0077", Amount: 1500, Currency: "ZAR", Status: "CANCELLED", Product: "CASH_PICKUP",
			Recipient: VoucherRecipient{Name: "Anselmo Macuácua", MSISDN: "+258842119003"},
			CreatedAt: dt(-77, 3), ExpiresAt: ptr(dt(13, 3)),
		}},
		{owner: guNomvula, v: Voucher{
			Code: "MMV-8823-4466-0100", Amount: 200, Currency: "ZAR", Status: "ACTIVE", Product: "GROCERY",
			Recipient: VoucherRecipient{Name: "Sindi Dlamini", MSISDN: "+27821234587"},
			CreatedAt: dt(-2, 5), ExpiresAt: ptr(dt(88, 5)),
		}},
		{owner: guAmara, v: Voucher{
			Code: "MMV-9934-5577-0155", Amount: 600, Currency: "ZAR", Status: "EXPIRED", Product: "AIRTIME",
			Recipient: VoucherRecipient{Name: "Ngozi Okonkwo", MSISDN: "+2348031882044"},
			CreatedAt: dt(-150, 1), ExpiresAt: ptr(dt(-60, 1)),
		}},
	}
}

// Voucher statuses used by the seed set and by Cancel.
const (
	voucherActive    = "ACTIVE"
	voucherRedeemed  = "REDEEMED"
	voucherCancelled = "CANCELLED"
	voucherExpired   = "EXPIRED"
)

func seedCloeBalances() map[string]*CloeBalance {
	return map[string]*CloeBalance{
		guTendai: {ZARBalance: 3980.20, USDCBalance: 215.40},
		guChipo:  {ZARBalance: 1610.00, USDCBalance: 88.00},
		guMpho:   {ZARBalance: 740.00, USDCBalance: 40.00},
	}
}

// seedDocumentBytes stands in for stored FICA scans. A 1x1 PNG is enough for the
// UI to render an <img> and for a download to be exercised end to end.
var seedDocumentBytes = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}
