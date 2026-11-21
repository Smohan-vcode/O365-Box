package bootstrap

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestSameThumbprint(t *testing.T) {
	tp := "bf16972ebe201138d74fcdd0aa467a1df7145d5f"
	raw, _ := hex.DecodeString(tp)
	for _, id := range []string{"BF16972EBE201138D74FCDD0AA467A1DF7145D5F", tp, base64.StdEncoding.EncodeToString(raw)} {
		if !SameThumbprint(id, tp) {
			t.Errorf("%q should match", id)
		}
	}
	if SameThumbprint("AAAA", tp) || SameThumbprint("", tp) {
		t.Error("different identifier matched")
	}
}
