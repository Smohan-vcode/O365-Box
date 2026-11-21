package bootstrap

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

// Check compares the pipeline app with expected-state (box check identity).
// It needs a client that can read the directory: the pipeline app itself
// (Directory.Read.All) is enough.
func Check(ctx context.Context, c *graph.Client, pl *Pipeline, want Permissions) ([]string, error) {
	var problems []string
	have, err := graph.List[Assignment](ctx, c, "/servicePrincipals/"+pl.SPID+"/appRoleAssignments")
	if err != nil {
		return nil, err
	}
	names := map[string]string{} // resourceId/roleId -> resource:value
	resKeyByID := map[string]string{}
	for key, appID := range Resources {
		rsp, err := resourceByAppID(ctx, c, appID)
		if err != nil {
			return nil, err
		}
		resKeyByID[rsp.ID] = key
		for _, r := range rsp.AppRoles {
			names[rsp.ID+"/"+r.ID] = key + ":" + r.Value
		}
	}
	got := map[string]bool{}
	for _, a := range have {
		n := names[a.ResourceID+"/"+a.AppRoleID]
		if n == "" {
			n = a.ResourceApp + ":" + a.AppRoleID
		}
		got[n] = true
	}
	exp := map[string]bool{}
	for key, perms := range want.ByResource() {
		for _, p := range perms {
			exp[key+":"+p] = true
		}
	}
	for n := range got {
		if !exp[n] {
			problems = append(problems, "unexpected permission "+n)
		}
		for _, nv := range want.Never {
			if strings.HasSuffix(n, ":"+nv) {
				problems = append(problems, "forbidden permission "+n)
			}
		}
	}
	for n := range exp {
		if !got[n] {
			problems = append(problems, "missing permission "+n)
		}
	}
	roles, err := graph.List[struct {
		RoleDefinitionID string `json:"roleDefinitionId"`
	}](ctx, c, "/roleManagement/directory/roleAssignments?$filter="+url.QueryEscape("principalId eq '"+pl.SPID+"'"))
	if err != nil {
		return nil, err
	}
	gotRoles := map[string]bool{}
	for _, r := range roles {
		name := r.RoleDefinitionID
		for n, id := range RoleTemplates {
			if id == r.RoleDefinitionID {
				name = n
			}
		}
		gotRoles[name] = true
	}
	for _, r := range want.EntraRoles {
		if !gotRoles[r] {
			problems = append(problems, "missing role "+r)
		}
	}
	for r := range gotRoles {
		found := false
		for _, w := range want.EntraRoles {
			found = found || w == r
		}
		if !found {
			problems = append(problems, "unexpected role "+r)
		}
		for _, nv := range want.Never {
			if r == nv {
				problems = append(problems, "forbidden role "+r)
			}
		}
	}
	var app struct {
		Password []any `json:"passwordCredentials"`
		Key      []any `json:"keyCredentials"`
	}
	if err := c.Get(ctx, "/applications/"+pl.ObjectID+"?$select=passwordCredentials,keyCredentials", &app); err != nil {
		return nil, err
	}
	if len(app.Password) > 0 {
		problems = append(problems, fmt.Sprintf("%d client secret(s) exist", len(app.Password)))
	}
	if len(app.Key) == 0 {
		problems = append(problems, "no certificate")
	}
	sort.Strings(problems)
	return problems, nil
}
