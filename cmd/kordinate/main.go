// Command kordinate is MamaMoney's customer operations console: the replacement
// for the sunsetting claire-admin Laravel app.
//
// It keeps claire-admin's feature surface — customer search and 360 view,
// compliance documents, transactions, deposits and EFT reconciliation, vouchers,
// card operations, the device blocker — and adds what that app lacked: an
// explicit onboarding lifecycle with SLAs and an audit trail, one stitched
// activity timeline across every microservice, AI-assisted document vetting,
// and burnt-in PII redaction with a separate, logged permission for revealing
// an original.
//
// kordinate is a thin kore composition, and a child of the Kosmos product set
// like any other. Everything MamaMoney-specific sits behind a seam — the
// upstream service clients are interfaces with fakes, the brand is a literal,
// the role map is data — so a partner variant is a new composition, not a fork.
package main

import (
	"github.com/krogio/kore"
	"github.com/krogio/kore/brand"
	"github.com/krogio/kore/modules/accessadmin"
	"github.com/krogio/kore/modules/auditui"
	"github.com/krogio/kore/modules/feedback"
	"github.com/krogio/kore/modules/groupsadmin"
	"github.com/krogio/kore/modules/licencepage"
	"github.com/krogio/kore/modules/notifications"
	"github.com/krogio/kore/modules/profile"
	"github.com/krogio/kore/modules/settingsui"
	"github.com/krogio/kore/modules/usersadmin"

	"github.com/krogio/kordinate/internal/kordinate"
)

func main() {
	app := kore.MustNew(kore.Options{
		Product: "kordinate",
		BrandDefaults: brand.Brand{
			ProductName: "Kordinate",
			Tagline:     "Customer operations, end to end",
			PrimaryHex:  "#0b3d5c",
			AccentHex:   "#f2a03d",
			// Every screen here shows customer PII; the classification banner is
			// a standing reminder that this is not a scratch environment.
			Classification: "Confidential — Customer Data",
		},
	})

	d := app.Deps()
	app.Register(
		kordinate.New(d),
		// kore battery modules — the shared admin, settings and profile surface.
		settingsui.New(d),
		usersadmin.New(d),
		groupsadmin.New(d),
		accessadmin.New(d, app.Sections),
		auditui.New(d),
		licencepage.New(d),
		profile.New(d),
		notifications.New(d),
		feedback.New(d),
	)
	if err := app.Run(); err != nil {
		panic(err)
	}
}
