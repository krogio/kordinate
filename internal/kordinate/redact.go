package kordinate

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // registers the GIF decoder; redacted output is re-encoded as PNG
	"image/jpeg"
	"image/png"
	"regexp"
	"strings"
)

// redact.go burns PII redactions into a document image.
//
// This is a different problem from masking a database column: the sensitive
// value is pixels in a photograph, so there is no query to rewrite. The
// redacted derivative is what most roles are served, and the unredacted
// original stays behind a separate, logged permission.
//
// The redaction is DESTRUCTIVE on purpose — the boxes are drawn into the
// raster and the result re-encoded. An overlay (CSS, PDF annotation, a
// client-side canvas) is not redaction: the underlying pixels travel with the
// file and anyone who saves the image gets the original. That distinction is
// the whole point of this file.
//
// Detection is heuristic and PROPOSES regions; a human confirms or adjusts
// before the redacted copy is stored. The detection classes reuse the same
// PII vocabulary as the platform's database masking (kwery's pii_scan), so
// "what counts as PII" is one answer across products.

// PIIKind classifies a detected sensitive value.
type PIIKind string

const (
	PIIIDNumber  PIIKind = "id_number"
	PIIPassport  PIIKind = "passport"
	PIIAddress   PIIKind = "address"
	PIIDOB       PIIKind = "dob"
	PIIPhone     PIIKind = "phone"
	PIIEmail     PIIKind = "email"
	PIIFace      PIIKind = "face"
	PIISignature PIIKind = "signature"
	PIIAccount   PIIKind = "account_number"
	PIIOther     PIIKind = "other"
)

// Label is the human name for a PII class, for the redaction UI's legend.
func (k PIIKind) Label() string {
	switch k {
	case PIIIDNumber:
		return "ID number"
	case PIIPassport:
		return "Passport number"
	case PIIAddress:
		return "Address"
	case PIIDOB:
		return "Date of birth"
	case PIIPhone:
		return "Phone number"
	case PIIEmail:
		return "Email address"
	case PIIFace:
		return "Photograph"
	case PIISignature:
		return "Signature"
	case PIIAccount:
		return "Account number"
	default:
		return "Sensitive detail"
	}
}

// Region is a rectangle to redact, in NORMALISED coordinates (0..1 of image
// width/height). Normalised because the agent draws boxes on a scaled preview
// in the browser; storing pixels would silently misalign the moment the preview
// size changes or a different derivative is generated.
type Region struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
	Kind PIIKind `json:"kind"`
	// Note carries the reason, for the audit record.
	Note string `json:"note,omitempty"`
	// Auto marks a region proposed by detection rather than drawn by hand.
	Auto bool `json:"auto"`
}

// Valid reports whether a region is inside the image and has real area. A
// zero-area or out-of-bounds box would silently redact nothing, which is the
// worst failure mode here — the operator believes the PII is covered.
func (r Region) Valid() bool {
	return r.W > 0 && r.H > 0 &&
		r.X >= 0 && r.Y >= 0 &&
		r.X+r.W <= 1.0001 && r.Y+r.H <= 1.0001
}

// pixelRect converts a normalised region to pixel bounds within an image,
// clamped to the image so a rounding overshoot can't panic the draw.
func (r Region) pixelRect(b image.Rectangle) image.Rectangle {
	w, h := float64(b.Dx()), float64(b.Dy())
	x0 := b.Min.X + int(r.X*w)
	y0 := b.Min.Y + int(r.Y*h)
	x1 := b.Min.X + int((r.X+r.W)*w)
	y1 := b.Min.Y + int((r.Y+r.H)*h)

	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	return image.Rect(x0, y0, x1, y1)
}

// Redaction is the stored record of what was covered on a document.
type Redaction struct {
	MediaID   string    `json:"mediaId"`
	Regions   []Region  `json:"regions"`
	Detected  []PIIKind `json:"detected,omitempty"`
	Auto      bool      `json:"auto"`
	AppliedBy string    `json:"appliedBy"`
}

// Apply burns the regions into the image and returns the re-encoded result
// along with its media type.
//
// Re-encoding is deliberate: it discards the original pixel data AND the EXIF
// metadata, which on a phone photo of an ID document routinely carries GPS
// coordinates and a device identifier. Copying metadata across would leak the
// customer's home location from a "redacted" file.
func Apply(data []byte, mediaType string, regions []Region) ([]byte, string, error) {
	if len(regions) == 0 {
		return nil, "", fmt.Errorf("redact: no regions given")
	}
	for i, r := range regions {
		if !r.Valid() {
			return nil, "", fmt.Errorf("redact: region %d is empty or outside the image", i)
		}
	}

	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("redact: decode image (%s): %w", mediaType, err)
	}

	// Draw onto an RGBA copy: the decoded image may be a read-only or paletted
	// type that can't be drawn into directly.
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)

	black := image.NewUniform(color.RGBA{A: 255})
	for _, r := range regions {
		draw.Draw(dst, r.pixelRect(b), black, image.Point{}, draw.Src)
	}

	var buf bytes.Buffer
	outType := "image/png"
	switch format {
	case "jpeg":
		// Re-encode JPEG at high quality: the redaction is evidence in a
		// compliance file, so legibility of the UNcovered fields matters.
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 92}); err != nil {
			return nil, "", fmt.Errorf("redact: encode jpeg: %w", err)
		}
		outType = "image/jpeg"
	default:
		if err := png.Encode(&buf, dst); err != nil {
			return nil, "", fmt.Errorf("redact: encode png: %w", err)
		}
	}
	return buf.Bytes(), outType, nil
}

// Redactable reports whether a media type can be redacted by this package.
// PDFs are explicitly NOT redactable here: covering a box on a rendered page
// leaves the original text layer intact and fully extractable, which would be
// a redaction that isn't one. Convert to an image first, or handle PDFs with a
// tool that rewrites the content stream.
func Redactable(mediaType string) bool {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/jpeg", "image/jpg", "image/png", "image/gif":
		return true
	}
	return false
}

// RedactionUnsupportedReason explains why a document can't be redacted, so the
// UI states the limitation rather than showing a dead button.
func RedactionUnsupportedReason(mediaType string) string {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	if mt == "application/pdf" {
		return "PDFs cannot be safely redacted in place — the text layer would survive underneath the boxes. Convert the page to an image first."
	}
	return fmt.Sprintf("%s documents cannot be redacted; only JPEG, PNG and GIF images are supported.", mediaType)
}

// ---- Detection over extracted text ----
//
// The regions themselves have to come from either the vetting model or the
// agent's mouse — this package does no OCR. What it does provide is
// classification of values the vetting pass already extracted, so the UI can
// tell the agent WHICH fields on this document are sensitive and should be
// covered, and pre-select the matching classes.

var (
	// South African ID: 13 digits, YYMMDD-prefixed.
	saIDRE = regexp.MustCompile(`\b\d{13}\b`)
	// Passport numbers vary by country; this is the common letter+digits shape.
	passportRE = regexp.MustCompile(`\b[A-Z]{1,2}\d{6,9}\b`)
	emailRE    = regexp.MustCompile(`\b[^@\s]+@[^@\s]+\.[A-Za-z]{2,}\b`)
	// SA mobile numbers, with or without country code.
	phoneRE = regexp.MustCompile(`(\+?27|0)\s?\d{2}\s?\d{3}\s?\d{4}\b`)
	// Long digit runs that look like bank/account numbers.
	accountRE = regexp.MustCompile(`\b\d{8,12}\b`)
	dateRE    = regexp.MustCompile(`\b(\d{4}[-/]\d{2}[-/]\d{2}|\d{2}[-/]\d{2}[-/]\d{4})\b`)
)

// ClassifyField maps an extracted field name and value to a PII class, or
// returns ("", false) when the field isn't sensitive.
//
// The field NAME is trusted first — a vetting pass that labels a value
// "idNumber" is more reliable than re-deriving the class from the digits, and
// avoids the ambiguity between a 13-digit SA ID and a long account number.
func ClassifyField(name, value string) (PIIKind, bool) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "idnumber", "id_number", "nationalid":
		return PIIIDNumber, true
	case "documentnumber", "document_number", "passportnumber", "passport_number":
		return PIIPassport, true
	case "dateofbirth", "date_of_birth", "dob":
		return PIIDOB, true
	case "address", "streetaddress", "residentialaddress":
		return PIIAddress, true
	case "phone", "mobile", "msisdn", "cellnumber":
		return PIIPhone, true
	case "email", "emailaddress":
		return PIIEmail, true
	case "accountnumber", "account_number", "iban":
		return PIIAccount, true
	}
	// No usable field name — fall back to the value's shape.
	return classifyValue(v)
}

// classifyValue infers a PII class from a value's shape. Ordered most specific
// first: an email also contains letters and digits, a 13-digit SA ID also
// matches the account-number pattern.
func classifyValue(v string) (PIIKind, bool) {
	switch {
	case emailRE.MatchString(v):
		return PIIEmail, true
	case saIDRE.MatchString(v):
		return PIIIDNumber, true
	case passportRE.MatchString(v):
		return PIIPassport, true
	case phoneRE.MatchString(v):
		return PIIPhone, true
	case accountRE.MatchString(v):
		return PIIAccount, true
	case dateRE.MatchString(v):
		return PIIDOB, true
	}
	return "", false
}

// SensitiveField is one field on a document that should be covered, surfaced to
// the agent doing the redaction.
type SensitiveField struct {
	Field string  `json:"field"`
	Kind  PIIKind `json:"kind"`
	// Preview is the value with all but its last characters masked, so the
	// redaction UI can identify the field without reprinting the PII it exists
	// to hide.
	Preview string `json:"preview"`
}

// SensitiveFields classifies a vetting pass's extracted values, returning what
// on this document is sensitive. Sorted by class for a stable UI.
func SensitiveFields(extracted map[string]string) []SensitiveField {
	var out []SensitiveField
	// Iterate a fixed field order rather than the map, so the list doesn't
	// reshuffle between requests.
	for _, f := range extractedFieldOrder {
		v, ok := extracted[f]
		if !ok {
			continue
		}
		if kind, sensitive := ClassifyField(f, v); sensitive {
			out = append(out, SensitiveField{Field: f, Kind: kind, Preview: maskValue(v)})
		}
	}
	return out
}

// extractedFieldOrder is the field order the vetting schema produces, used to
// keep output deterministic.
var extractedFieldOrder = []string{
	"fullName", "firstName", "lastName", "idNumber", "documentNumber",
	"dateOfBirth", "issueDate", "expiryDate", "issuingCountry", "nationality",
	"address", "streetAddress", "phone", "msisdn", "email", "accountNumber",
}

// maskValue keeps the last 4 characters and masks the rest — enough for an
// agent to confirm they're looking at the right field, not enough to be a
// leak of the value itself.
func maskValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) <= 4 {
		return strings.Repeat("•", len(v))
	}
	return strings.Repeat("•", len(v)-4) + v[len(v)-4:]
}

// DefaultRegions proposes redaction boxes for a document type.
//
// Without OCR there are no true coordinates, so these are conventional field
// positions for standard-layout documents — a starting point the agent drags
// into place, which is far faster than drawing from scratch. They are marked
// Auto so the audit record distinguishes a proposal that was accepted from a
// box a human placed deliberately.
func DefaultRegions(docType string) []Region {
	switch docType {
	case DocSAIDFrontType, DocSAIDBackType:
		return []Region{
			{X: 0.30, Y: 0.28, W: 0.60, H: 0.10, Kind: PIIIDNumber, Auto: true, Note: "ID number field"},
			{X: 0.04, Y: 0.20, W: 0.24, H: 0.55, Kind: PIIFace, Auto: true, Note: "photograph"},
		}
	case DocPassportType:
		return []Region{
			{X: 0.30, Y: 0.20, W: 0.55, H: 0.09, Kind: PIIPassport, Auto: true, Note: "passport number"},
			{X: 0.04, Y: 0.18, W: 0.24, H: 0.50, Kind: PIIFace, Auto: true, Note: "photograph"},
			// The machine-readable zone repeats every field on the page — a
			// redaction that misses it achieves nothing.
			{X: 0.02, Y: 0.82, W: 0.96, H: 0.16, Kind: PIIOther, Auto: true, Note: "machine-readable zone"},
		}
	case DocBankStatementType, DocPayslipType:
		return []Region{
			{X: 0.02, Y: 0.10, W: 0.50, H: 0.14, Kind: PIIAddress, Auto: true, Note: "name and address block"},
			{X: 0.50, Y: 0.10, W: 0.48, H: 0.10, Kind: PIIAccount, Auto: true, Note: "account number"},
		}
	default:
		return nil
	}
}

// Document type constants mirrored locally so this file doesn't depend on the
// upstream package for what is really presentation metadata.
const (
	DocSAIDFrontType     = "SA_ID_FRONT"
	DocSAIDBackType      = "SA_ID_BACK"
	DocPassportType      = "PASSPORT"
	DocBankStatementType = "BANK_STATEMENT"
	DocPayslipType       = "PAYSLIP"
)
