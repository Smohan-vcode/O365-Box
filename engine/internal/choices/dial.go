package choices

import "fmt"

// Dial is the "How careful?" question (SPEC §25.3): one setting that picks
// the policy tier and the default of every choice.
type Dial struct {
	Question string  `yaml:"question" json:"question"`
	Help     string  `yaml:"help" json:"help,omitempty"`
	Levels   []Level `yaml:"levels" json:"levels"`
}

// Level is one position of the dial.
type Level struct {
	Value       string `yaml:"value" json:"value"`
	Label       string `yaml:"label" json:"label"`
	Detail      string `yaml:"detail" json:"detail"`
	Tier        string `yaml:"tier" json:"tier"` // policy.tier: core | enhanced
	Pick        string `yaml:"pick" json:"pick"` // per-choice answer: essential | recommended | strict
	Recommended bool   `yaml:"recommended" json:"recommended,omitempty"`
}

var picks = map[string]bool{"essential": true, "recommended": true, "strict": true}

func (d Dial) validate() []string {
	if len(d.Levels) == 0 {
		return nil
	}
	var out []string
	recs, seen := 0, map[string]bool{}
	for _, l := range d.Levels {
		if l.Value == "" || seen[l.Value] {
			out = append(out, "missing or duplicate level value")
		}
		seen[l.Value] = true
		if l.Label == "" || l.Detail == "" {
			out = append(out, fmt.Sprintf("level %s needs a label and a detail", l.Value))
		}
		if l.Tier != "core" && l.Tier != "enhanced" {
			out = append(out, fmt.Sprintf("level %s: tier %q is not core or enhanced", l.Value, l.Tier))
		}
		if !picks[l.Pick] {
			out = append(out, fmt.Sprintf("level %s: pick %q is not essential, recommended or strict", l.Value, l.Pick))
		}
		if l.Recommended {
			recs++
		}
	}
	if recs != 1 {
		out = append(out, fmt.Sprintf("exactly one level must be recommended (found %d)", recs))
	}
	return out
}

// Level returns a dial level by value, or nil.
func (d Dial) Level(v string) *Level {
	for i := range d.Levels {
		if d.Levels[i].Value == v {
			return &d.Levels[i]
		}
	}
	return nil
}

// Recommended is the preselected level, or nil when the dial is empty.
func (d Dial) Recommended() *Level {
	for i := range d.Levels {
		if d.Levels[i].Recommended {
			return &d.Levels[i]
		}
	}
	return nil
}

// DefaultAt is a choice's default at a dial level: the option the level
// names for it, else the choice's own default.
func (ch *Choice) DefaultAt(l *Level, ctx Context) string {
	if l != nil {
		v := ""
		switch l.Pick {
		case "essential":
			v = ch.Essential
		case "strict":
			v = ch.Strict
		}
		if v != "" && ch.Option(v) != nil {
			return v
		}
	}
	return ch.Default(ctx)
}

// DialAnswers is what setting the dial to a level writes: the policy tier
// and an explicit answer for every choice that applies to the company.
func (c *Catalogue) DialAnswers(level string, ctx Context) (string, map[string]string, error) {
	l := c.Dial.Level(level)
	if l == nil {
		return "", nil, fmt.Errorf("unknown dial level %q", level)
	}
	answers := map[string]string{}
	for i := range c.Choices {
		ch := &c.Choices[i]
		if ch.Applies(ctx) {
			answers[ch.ID] = ch.DefaultAt(l, ctx)
		}
	}
	return l.Tier, answers, nil
}
