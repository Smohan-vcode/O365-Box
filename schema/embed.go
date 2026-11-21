// Package schema embeds the JSON schema for the configuration document.
package schema

import _ "embed"

// BoxConfig is schema/box-config.schema.json.
//
//go:embed box-config.schema.json
var BoxConfig []byte
