package config

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

// Options carries engine data the semantic checks compare against. A nil
// slice means "not available yet" and skips that check.
type Options struct {
	Roles         []string       // defaults/roles.yaml role names
	Policy        *PolicyCatalog // ids in policy/; nil skips the catalogue checks
	Jurisdictions []string       // defaults/jurisdictions/*.yaml codes
	Today         time.Time      // date exceptions are compared with; zero means now
}

// PolicyCatalog lists the ids the policy section may refer to.
type PolicyCatalog struct {
	Frameworks   []string // policy/frameworks/<id>.yaml
	Settings     []string // policy/settings/*.yaml setting ids
	Requirements []string // policy/requirements.yaml ids
}

// DateLayout is the form of policy.exceptions[].ends.
const DateLayout = "2006-01-02"

var levelRank = map[string]int{"visitor": 1, "member": 2, "owner": 3}

// Validate runs the schema and every semantic check (SPEC §6, §7, §9, §20, policy/README.md).
func Validate(d *Document, opt Options) ([]report.Issue, error) {
	issues, err := schemaIssues(d)
	if err != nil {
		return nil, err
	}
	v := &validator{d: d, c: &d.Config, opt: opt}
	v.run()
	issues = append(issues, v.issues...)
	report.Sort(issues)
	return issues, nil
}

type validator struct {
	d      *Document
	c      *Config
	opt    Options
	issues []report.Issue
}

func (v *validator) add(path, format string, a ...any) {
	v.issues = append(v.issues, report.Issue{Check: "config", Path: path, File: v.d.File,
		Line: v.d.Line(path), Message: fmt.Sprintf(format, a...)})
}

func (v *validator) run() {
	users := v.users()
	groups := v.groups()
	roles := v.roleGroups()
	v.tenant(users)
	v.company()
	v.modules()
	v.mail(users, groups)
	v.userRefs(groups)
	v.structure(groups, roles)
	v.policy(groups)
	v.jurisdictions()
}

func (v *validator) users() map[string]User {
	m := map[string]User{}
	addr := map[string]string{}
	for i, u := range v.c.Users {
		p := fmt.Sprintf("users[%d]", i)
		if u.ID == "" {
			continue
		}
		if _, dup := m[u.ID]; dup {
			v.add(p+".id", "duplicate user id %q", u.ID)
		}
		m[u.ID] = u
		if u.Primary != "" {
			if other, dup := addr[u.Primary]; dup {
				v.add(p+".primary", "address %q is already used by user %q", u.Primary, other)
			}
			addr[u.Primary] = u.ID
		}
	}
	return m
}

func (v *validator) groups() map[string]Group {
	m := map[string]Group{}
	for i, g := range v.c.Groups {
		if _, dup := m[g.Name]; dup {
			v.add(fmt.Sprintf("groups[%d].name", i), "duplicate group %q", g.Name)
		}
		m[g.Name] = g
	}
	return m
}

// roleGroups returns role-<name> for every role, or nil if roles are unknown.
func (v *validator) roleGroups() map[string]bool {
	if v.opt.Roles == nil {
		return nil
	}
	m := map[string]bool{}
	for _, r := range v.opt.Roles {
		m["role-"+r] = true
	}
	return m
}

func (v *validator) tenant(users map[string]User) {
	t := v.c.Tenant
	if t.FlowOwner != "" {
		u, ok := users[t.FlowOwner]
		switch {
		case !ok:
			v.add("tenant.flow_owner", "flow owner %q is not a user", t.FlowOwner)
		case u.BreakGlass:
			v.add("tenant.flow_owner", "flow owner %q must not be a break-glass account", t.FlowOwner)
		case u.Plan == "none":
			v.add("tenant.flow_owner", "flow owner %q needs a plan: flows run on its connections", t.FlowOwner)
		}
	}
	for i, id := range t.BreakGlassAccounts {
		p := fmt.Sprintf("tenant.break_glass_accounts[%d]", i)
		u, ok := users[id]
		switch {
		case !ok:
			v.add(p, "break-glass account %q is not a user", id)
		case !u.BreakGlass:
			v.add(p, "user %q must set break_glass: true", id)
		case u.Plan != "none":
			v.add(p, "break-glass account %q must have plan none", id)
		}
	}
	for i, u := range v.c.Users {
		if u.BreakGlass && !contains(t.BreakGlassAccounts, u.ID) {
			v.add(fmt.Sprintf("users[%d].break_glass", i), "user %q is break-glass but not listed in tenant.break_glass_accounts", u.ID)
		}
		if u.BreakGlass && (len(u.Roles) > 0 || len(u.AccessGroups) > 0) {
			v.add(fmt.Sprintf("users[%d]", i), "break-glass account %q must hold no roles or access groups", u.ID)
		}
	}
}

func (v *validator) company() {
	c := v.c.Company
	if v.c.DNS.Zone != "" && c.PrimaryDomain != "" && v.c.DNS.Zone != c.PrimaryDomain && !contains(c.DomainAliases, v.c.DNS.Zone) {
		v.add("dns.zone", "zone %q is neither the primary domain nor an alias", v.c.DNS.Zone)
	}
}

func (v *validator) modules() {
	m := v.c.Modules
	need := func(on bool, name string, dep bool, depName string) {
		if on && !dep {
			v.add("modules."+name, "module %s needs module %s", name, depName)
		}
	}
	need(m.Legal, "legal", m.Records, "records")
	need(m.Sales, "sales", m.Records, "records")
	need(m.People, "people", m.Records, "records")
	need(m.Policies, "policies", m.Records, "records")
	need(m.Compliance, "compliance", m.Policies, "policies")
	for i, s := range v.c.Mail.Shared {
		if s.Module == "" {
			continue
		}
		on := map[string]bool{"records": m.Records, "people": m.People, "legal": m.Legal, "sales": m.Sales,
			"policies": m.Policies, "compliance": m.Compliance, "devices": m.Devices}
		enabled, known := on[s.Module]
		if !known {
			v.add(fmt.Sprintf("mail.shared[%d].module", i), "unknown module %q", s.Module)
		} else if !enabled {
			v.add(fmt.Sprintf("mail.shared[%d].module", i), "mailbox %q belongs to module %s, which is off", s.Name, s.Module)
		}
	}
	if m.Sales && v.c.Integrations.InboundMailbox == "" && !v.hasShared("inbound") {
		v.add("modules.sales", "module sales needs the inbound shared mailbox")
	}
}

func (v *validator) hasShared(name string) bool {
	for _, s := range v.c.Mail.Shared {
		if s.Name == name {
			return true
		}
	}
	return false
}

func (v *validator) mail(users map[string]User, groups map[string]Group) {
	names := map[string]string{}
	claim := func(path, name, owner string) {
		if name == "" {
			return
		}
		if prev, dup := names[name]; dup {
			v.add(path, "address %q is already used by %s", name, prev)
			return
		}
		names[name] = owner
	}
	for _, u := range v.c.Users {
		claim("users", u.Primary, "user "+u.ID)
	}
	shared := map[string]bool{}
	for i, s := range v.c.Mail.Shared {
		p := fmt.Sprintf("mail.shared[%d]", i)
		claim(p+".name", s.Name, "shared mailbox "+s.Name)
		for j, a := range s.Aliases {
			claim(fmt.Sprintf("%s.aliases[%d]", p, j), a, "shared mailbox "+s.Name)
		}
		shared[s.Name] = true
		if g, ok := groups[s.Access]; !ok {
			v.add(p+".access", "access group %q is not defined in groups", s.Access)
		} else if g.Kind != "access" {
			v.add(p+".access", "group %q is not an access group", s.Access)
		}
		if s.AutoReply != "" {
			if _, ok := v.c.Mail.AutoReplies[s.AutoReply]; !ok {
				v.add(p+".auto_reply", "auto-reply %q has no text in mail.auto_replies", s.AutoReply)
			}
		}
	}
	for i, l := range v.c.Mail.Lists {
		p := fmt.Sprintf("mail.lists[%d]", i)
		claim(p+".name", l.Name, "list "+l.Name)
		for j, a := range l.Aliases {
			claim(fmt.Sprintf("%s.aliases[%d]", p, j), a, "list "+l.Name)
		}
		for j, m := range l.Members {
			if _, isUser := users[m]; !isUser && !shared[m] {
				v.add(fmt.Sprintf("%s.members[%d]", p, j), "member %q is neither a user id nor a shared mailbox", m)
			}
		}
	}
	if r := v.c.Mail.AutoReplies; r != nil {
		used := map[string]bool{}
		for _, s := range v.c.Mail.Shared {
			used[s.AutoReply] = true
		}
		for _, k := range sortedKeys(r) {
			if !used[k] {
				v.add("mail.auto_replies."+k, "auto-reply %q is not used by any shared mailbox", k)
			}
		}
	}
}

func (v *validator) userRefs(groups map[string]Group) {
	roles := map[string]bool{}
	for _, r := range v.opt.Roles {
		roles[r] = true
	}
	for i, u := range v.c.Users {
		for j, g := range u.AccessGroups {
			if gr, ok := groups[g]; !ok {
				v.add(fmt.Sprintf("users[%d].access_groups[%d]", i, j), "access group %q is not defined in groups", g)
			} else if gr.Kind != "access" {
				v.add(fmt.Sprintf("users[%d].access_groups[%d]", i, j), "group %q is not an access group", g)
			}
		}
		if v.opt.Roles != nil {
			for j, r := range u.Roles {
				if !roles[r] {
					v.add(fmt.Sprintf("users[%d].roles[%d]", i, j), "role %q is not in defaults/roles.yaml", r)
				}
			}
		}
	}
	if v.opt.Roles != nil {
		owner := false
		for _, u := range v.c.Users {
			owner = owner || contains(u.Roles, "owner")
		}
		if !owner && len(v.c.Users) > 0 {
			v.add("users", "at least one user must hold the owner role")
		}
	}
}

func (v *validator) structure(groups map[string]Group, roles map[string]bool) {
	urls := map[string]bool{v.c.Structure.Hub.URL: true}
	admin := false
	for i, s := range v.c.Structure.Sites {
		p := fmt.Sprintf("structure.sites[%d]", i)
		if urls[s.URL] {
			v.add(p+".url", "site url %q is already used", s.URL)
		}
		urls[s.URL] = true
		if s.Name == "Admin" {
			admin = true
		}
		if s.External != "" && s.External != "off" && s.ExternalReason == "" {
			v.add(p+".external", "external sharing on site %q needs external_reason", s.Name)
		}
		v.access(p+".access", s.Access, groups, roles)
		libs := map[string]bool{}
		for j, l := range s.Libraries {
			lp := fmt.Sprintf("%s.libraries[%d]", p, j)
			if libs[l.Name] {
				v.add(lp+".name", "library %q appears twice in site %q", l.Name, s.Name)
			}
			libs[l.Name] = true
			if l.Access == nil {
				continue
			}
			v.access(lp+".access", l.Access, groups, roles)
			if looser := loosened(s.Access, l.Access); len(looser) > 0 && l.AccessReason == "" {
				v.add(lp+".access", "library %q gives %s more access than site %q; add access_reason",
					l.Name, strings.Join(looser, ", "), s.Name)
			}
		}
	}
	if len(v.c.Structure.Sites) > 0 && !admin {
		v.add("structure.sites", "the Admin site cannot be removed: it holds the register and the configuration store")
	}
}

// access applies the permission guardrails of SPEC §7 to one access map.
func (v *validator) access(path string, m map[string]string, groups map[string]Group, roles map[string]bool) {
	for _, who := range sortedKeys(m) {
		p := path + "." + who
		lower := strings.ToLower(who)
		switch {
		case strings.HasPrefix(lower, "everyone"):
			v.add(p, "%q is never granted access (SPEC §7)", who)
		case strings.HasPrefix(who, "grp-"):
			if g, ok := groups[who]; !ok {
				v.add(p, "group %q is not defined in groups", who)
			} else if g.Kind != "access" {
				v.add(p, "group %q is not an access group", who)
			}
		case strings.HasPrefix(who, "role-"):
			if roles != nil && !roles[who] {
				v.add(p, "role group %q has no role in defaults/roles.yaml", who)
			}
		default:
			v.add(p, "access is granted to groups only (grp-* or role-*), not to %q", who)
		}
	}
}

func loosened(site, lib map[string]string) []string {
	var out []string
	for _, who := range sortedKeys(lib) {
		if levelRank[lib[who]] > levelRank[site[who]] {
			out = append(out, who)
		}
	}
	return out
}

func (v *validator) policy(groups map[string]Group) {
	p := v.c.Policy
	if g := p.Rollout.PilotGroup; g != "" {
		if gr, ok := groups[g]; !ok {
			v.add("policy.rollout.pilot_group", "pilot group %q is not defined in groups", g)
		} else if gr.Kind != "access" {
			v.add("policy.rollout.pilot_group", "group %q is not an access group", g)
		}
	}
	today := v.opt.Today
	if today.IsZero() {
		today = time.Now()
	}
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	var fws, ids map[string]bool
	if cat := v.opt.Policy; cat != nil {
		fws = set(cat.Frameworks)
		ids = set(cat.Settings)
		for _, r := range cat.Requirements {
			ids[r] = true
		}
		for i, f := range p.Frameworks {
			if !fws[f] {
				v.add(fmt.Sprintf("policy.frameworks[%d]", i), "framework %q is not in policy/frameworks", f)
			}
		}
	}
	seen := map[string]bool{}
	for i, e := range p.Exceptions {
		path := fmt.Sprintf("policy.exceptions[%d]", i)
		if e.ID != "" {
			if seen[e.ID] {
				v.add(path+".id", "exception for %s appears twice", e.ID)
			}
			seen[e.ID] = true
			if ids != nil && !ids[e.ID] {
				v.add(path+".id", "%q is neither a setting nor a requirement in policy/", e.ID)
			}
		}
		if e.Ends == "" {
			continue
		}
		end, err := time.Parse(DateLayout, e.Ends)
		switch {
		case err != nil:
			v.add(path+".ends", "%q is not a date (YYYY-MM-DD)", e.Ends)
		case end.Before(today):
			v.add(path+".ends", "exception for %s expired on %s; renew it with a new end date or remove it", e.ID, e.Ends)
		}
	}
}

func set(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, x := range s {
		m[x] = true
	}
	return m
}

func (v *validator) jurisdictions() {
	j := v.c.Jurisdictions
	if j.Registered != "" && !contains(j.Operating, j.Registered) {
		v.add("jurisdictions.operating", "the place of registration %q must also be listed as operating", j.Registered)
	}
	if v.opt.Jurisdictions == nil {
		return
	}
	known := map[string]bool{}
	for _, c := range v.opt.Jurisdictions {
		known[c] = true
	}
	if j.Registered != "" && !known[j.Registered] {
		v.add("jurisdictions.registered", "no rules file defaults/jurisdictions/%s.yaml", j.Registered)
	}
	for i, c := range j.Operating {
		if !known[c] {
			v.add(fmt.Sprintf("jurisdictions.operating[%d]", i), "no rules file defaults/jurisdictions/%s.yaml", c)
		}
	}
}

func contains(s []string, x string) bool {
	for _, e := range s {
		if e == x {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
