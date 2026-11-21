package config

// Config is the typed view of the configuration document used by the
// semantic checks. The JSON schema is the authority on shape; fields not
// needed for cross-checks are left out.
type Config struct {
	Version       int           `yaml:"version"`
	Tenant        Tenant        `yaml:"tenant"`
	Company       Company       `yaml:"company"`
	Jurisdictions Jurisdictions `yaml:"jurisdictions"`
	Modules       Modules       `yaml:"modules"`
	DNS           DNS           `yaml:"dns"`
	Mail          Mail          `yaml:"mail"`
	Users         []User        `yaml:"users"`
	Groups        []Group       `yaml:"groups"`
	Structure     Structure     `yaml:"structure"`
	Baseline      Baseline      `yaml:"baseline"`
	Integrations  Integrations  `yaml:"integrations"`
}

type Tenant struct {
	Environment        string   `yaml:"environment"`
	TenantID           string   `yaml:"tenant_id"`
	DefaultPlan        string   `yaml:"default_plan"`
	BreakGlassAccounts []string `yaml:"break_glass_accounts"`
	FlowOwner          string   `yaml:"flow_owner"`
}

type Company struct {
	LegalName     string   `yaml:"legal_name"`
	TradingName   string   `yaml:"trading_name"`
	EntityWording string   `yaml:"entity_wording"`
	PrimaryDomain string   `yaml:"primary_domain"`
	DomainAliases []string `yaml:"domain_aliases"`
}

type Jurisdictions struct {
	Registered string   `yaml:"registered"`
	Operating  []string `yaml:"operating"`
}

type Modules struct {
	Records    bool `yaml:"records"`
	People     bool `yaml:"people"`
	Legal      bool `yaml:"legal"`
	Sales      bool `yaml:"sales"`
	Policies   bool `yaml:"policies"`
	Compliance bool `yaml:"compliance"`
	Devices    bool `yaml:"devices"`
}

type DNS struct {
	Provider string `yaml:"provider"`
	Zone     string `yaml:"zone"`
}

type Mail struct {
	Shared      []SharedMailbox   `yaml:"shared"`
	Lists       []DistList        `yaml:"lists"`
	AutoReplies map[string]string `yaml:"auto_replies"`
}

type SharedMailbox struct {
	Name      string   `yaml:"name"`
	Aliases   []string `yaml:"aliases"`
	Access    string   `yaml:"access"`
	AutoReply string   `yaml:"auto_reply"`
	Module    string   `yaml:"module"`
}

type DistList struct {
	Name    string   `yaml:"name"`
	Aliases []string `yaml:"aliases"`
	Members []string `yaml:"members"`
}

type User struct {
	ID           string   `yaml:"id"`
	Primary      string   `yaml:"primary"`
	GivenName    string   `yaml:"given_name"`
	FamilyName   string   `yaml:"family_name"`
	Plan         string   `yaml:"plan"`
	Roles        []string `yaml:"roles"`
	AccessGroups []string `yaml:"access_groups"`
	BreakGlass   bool     `yaml:"break_glass"`
}

type Group struct {
	Name    string   `yaml:"name"`
	Kind    string   `yaml:"kind"`
	Members []string `yaml:"members"`
}

type Structure struct {
	Hub   Hub    `yaml:"hub"`
	Sites []Site `yaml:"sites"`
}

type Hub struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

type Site struct {
	Name           string            `yaml:"name"`
	URL            string            `yaml:"url"`
	External       string            `yaml:"external"`
	ExternalReason string            `yaml:"external_reason"`
	Access         map[string]string `yaml:"access"`
	Libraries      []Library         `yaml:"libraries"`
}

type Library struct {
	Name         string            `yaml:"name"`
	Access       map[string]string `yaml:"access"`
	AccessReason string            `yaml:"access_reason"`
}

type Baseline struct {
	Preset    string     `yaml:"preset"`
	Overrides []Override `yaml:"overrides"`
}

type Override struct {
	ID     string `yaml:"id"`
	Reason string `yaml:"reason"`
}

type Integrations struct {
	InboundMailbox string `yaml:"inbound_mailbox"`
}
