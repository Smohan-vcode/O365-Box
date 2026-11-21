package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
	"github.com/Smohan-vcode/O365-Box/schema"
)

const schemaURL = "https://github.com/Smohan-vcode/O365-Box/schema/box-config.schema.json"

var (
	compileOnce sync.Once
	compiled    *jsonschema.Schema
	compileErr  error
	printer     = message.NewPrinter(language.English)
)

func boxSchema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.BoxConfig))
		if err != nil {
			compileErr = fmt.Errorf("schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(schemaURL, doc); err != nil {
			compileErr = fmt.Errorf("schema: %w", err)
			return
		}
		compiled, compileErr = c.Compile(schemaURL)
	})
	return compiled, compileErr
}

// schemaIssues validates the document against schema/box-config.schema.json.
func schemaIssues(d *Document) ([]report.Issue, error) {
	sch, err := boxSchema()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(d.Raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", d.File, err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	err = sch.Validate(inst)
	if err == nil {
		return nil, nil
	}
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return nil, err
	}
	var out []report.Issue
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			p := pointerToPath(e.InstanceLocation)
			out = append(out, report.Issue{Check: "schema", Path: p, File: d.File, Line: d.Line(p),
				Message: e.ErrorKind.LocalizedString(printer)})
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	return dedupe(out), nil
}

func pointerToPath(parts []string) string {
	var sb strings.Builder
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err == nil {
			sb.WriteString("[" + p + "]")
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte('.')
		}
		sb.WriteString(p)
	}
	return sb.String()
}

func dedupe(in []report.Issue) []report.Issue {
	seen := map[string]bool{}
	var out []report.Issue
	for _, i := range in {
		k := i.Path + "\x00" + i.Message
		if !seen[k] {
			seen[k] = true
			out = append(out, i)
		}
	}
	return out
}
