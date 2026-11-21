// Package discovery reads what Microsoft already knows about a tenant, to
// pre-fill onboarding (SPEC §18.3). It only reads.
package discovery

import (
	"context"
	"errors"
	"sort"

	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

// Result is everything discovery found.
type Result struct {
	Organization       Organization `json:"organization"`
	Domains            []Domain     `json:"domains"`
	Plans              []Plan       `json:"plans"`
	Users              int          `json:"users"`
	Groups             int          `json:"groups"`
	Me                 Me           `json:"me"`
	SecurityDefaultsOn *bool        `json:"security_defaults_on,omitempty"`
	ConditionalAccess  *int         `json:"conditional_access_policies,omitempty"`
	Problems           []string     `json:"problems,omitempty"` // reads that failed
}

// Organization is the tenant's organisation profile.
type Organization struct {
	ID                string   `json:"id"`
	DisplayName       string   `json:"displayName"`
	Street            string   `json:"street"`
	City              string   `json:"city"`
	State             string   `json:"state"`
	PostalCode        string   `json:"postalCode"`
	CountryLetterCode string   `json:"countryLetterCode"`
	PreferredLanguage string   `json:"preferredLanguage"`
	TechnicalMails    []string `json:"technicalNotificationMails"`
	TenantType        string   `json:"tenantType"`
}

// Domain is one tenant domain.
type Domain struct {
	ID         string `json:"id"`
	IsDefault  bool   `json:"isDefault"`
	IsInitial  bool   `json:"isInitial"`
	IsVerified bool   `json:"isVerified"`
}

// Plan is one subscribed SKU.
type Plan struct {
	SKU      string `json:"skuPartNumber"`
	Enabled  int    `json:"enabled"`
	Consumed int    `json:"consumedUnits"`
}

// Me is the signed-in administrator.
type Me struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	GivenName   string `json:"givenName"`
	Surname     string `json:"surname"`
	UPN         string `json:"userPrincipalName"`
}

// Discover runs every read. A failed read is recorded in Problems rather
// than failing the whole discovery.
func Discover(ctx context.Context, c *graph.Client) (*Result, error) {
	r := &Result{}
	orgs, err := graph.List[Organization](ctx, c, "/organization")
	if err != nil {
		return nil, err
	}
	if len(orgs) == 0 {
		return nil, errors.New("no organisation returned")
	}
	r.Organization = orgs[0]
	problem := func(what string, err error) { r.Problems = append(r.Problems, what+": "+err.Error()) }
	if d, err := graph.List[Domain](ctx, c, "/domains"); err == nil {
		sort.Slice(d, func(i, j int) bool { return d[i].ID < d[j].ID })
		r.Domains = d
	} else {
		problem("domains", err)
	}
	type sku struct {
		SKU      string `json:"skuPartNumber"`
		Consumed int    `json:"consumedUnits"`
		Prepaid  struct {
			Enabled int `json:"enabled"`
		} `json:"prepaidUnits"`
	}
	if s, err := graph.List[sku](ctx, c, "/subscribedSkus"); err == nil {
		for _, x := range s {
			r.Plans = append(r.Plans, Plan{SKU: x.SKU, Enabled: x.Prepaid.Enabled, Consumed: x.Consumed})
		}
	} else {
		problem("subscribed plans", err)
	}
	if u, err := graph.List[struct{ ID string }](ctx, c, "/users?$select=id&$top=999"); err == nil {
		r.Users = len(u)
	} else {
		problem("users", err)
	}
	if g, err := graph.List[struct{ ID string }](ctx, c, "/groups?$select=id&$top=999"); err == nil {
		r.Groups = len(g)
	} else {
		problem("groups", err)
	}
	if err := c.Get(ctx, "/me?$select=id,displayName,givenName,surname,userPrincipalName", &r.Me); err != nil {
		problem("me", err)
	}
	var sd struct {
		IsEnabled bool `json:"isEnabled"`
	}
	if err := c.Get(ctx, "/policies/identitySecurityDefaultsEnforcementPolicy", &sd); err == nil {
		r.SecurityDefaultsOn = &sd.IsEnabled
	} else {
		problem("security defaults", err)
	}
	if p, err := graph.List[struct{ ID string }](ctx, c, "/identity/conditionalAccess/policies"); err == nil {
		n := len(p)
		r.ConditionalAccess = &n
	} else {
		problem("conditional access", err)
	}
	return r, nil
}

// Fields reports which organisation address fields are populated, without
// their values (for findings that go into the public repository).
func (o Organization) Fields() map[string]bool {
	return map[string]bool{"displayName": o.DisplayName != "", "street": o.Street != "", "city": o.City != "",
		"state": o.State != "", "postalCode": o.PostalCode != "", "countryLetterCode": o.CountryLetterCode != "",
		"technicalNotificationMails": len(o.TechnicalMails) > 0, "preferredLanguage": o.PreferredLanguage != ""}
}
