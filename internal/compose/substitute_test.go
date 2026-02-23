package compose

import (
	"testing"
)

func TestSubstituteVars_BraceSyntax(t *testing.T) {
	context := map[string]string{
		"DB_USER": "postgres",
		"DB_PASS": "secret",
	}

	env := map[string]string{ //nolint:gosec // test data, not real credentials
		"USER":     "${DB_USER}",
		"PASSWORD": "${DB_PASS}",
		"LITERAL":  "no-vars-here",
	}

	result := substituteVars(env, context)

	if result["USER"] != "postgres" {
		t.Errorf("USER: got %q, want %q", result["USER"], "postgres")
	}
	if result["PASSWORD"] != "secret" {
		t.Errorf("PASSWORD: got %q, want %q", result["PASSWORD"], "secret")
	}
	if result["LITERAL"] != "no-vars-here" {
		t.Errorf("LITERAL: got %q, want %q", result["LITERAL"], "no-vars-here")
	}
}

func TestSubstituteVars_DollarSyntax(t *testing.T) {
	context := map[string]string{
		"HOST": "localhost",
	}

	env := map[string]string{
		"DB_HOST": "$HOST",
	}

	result := substituteVars(env, context)

	if result["DB_HOST"] != "localhost" {
		t.Errorf("DB_HOST: got %q, want %q", result["DB_HOST"], "localhost")
	}
}

func TestSubstituteVars_DefaultValue(t *testing.T) {
	context := map[string]string{
		"SET_VAR": "actual",
	}

	env := map[string]string{
		"WITH_DEFAULT":     "${UNSET_VAR:-fallback}",
		"SET_WITH_DEFAULT": "${SET_VAR:-fallback}",
		"EMPTY_DEFAULT":    "${UNSET:-}",
	}

	result := substituteVars(env, context)

	if result["WITH_DEFAULT"] != "fallback" {
		t.Errorf("WITH_DEFAULT: got %q, want %q", result["WITH_DEFAULT"], "fallback")
	}
	if result["SET_WITH_DEFAULT"] != "actual" {
		t.Errorf("SET_WITH_DEFAULT: got %q, want %q", result["SET_WITH_DEFAULT"], "actual")
	}
	if result["EMPTY_DEFAULT"] != "" {
		t.Errorf("EMPTY_DEFAULT: got %q, want %q", result["EMPTY_DEFAULT"], "")
	}
}

func TestSubstituteVars_UnresolvedBecomesEmpty(t *testing.T) {
	context := map[string]string{}

	env := map[string]string{
		"MISSING_BRACE":  "${NONEXISTENT}",
		"MISSING_DOLLAR": "$NONEXISTENT",
	}

	result := substituteVars(env, context)

	if result["MISSING_BRACE"] != "" {
		t.Errorf("MISSING_BRACE: got %q, want empty", result["MISSING_BRACE"])
	}
	if result["MISSING_DOLLAR"] != "" {
		t.Errorf("MISSING_DOLLAR: got %q, want empty", result["MISSING_DOLLAR"])
	}
}

func TestSubstituteVars_MixedContent(t *testing.T) {
	context := map[string]string{
		"HOST": "db.local",
		"PORT": "5432",
	}

	env := map[string]string{
		"DSN": "postgres://${HOST}:${PORT}/mydb",
	}

	result := substituteVars(env, context)

	want := "postgres://db.local:5432/mydb"
	if result["DSN"] != want {
		t.Errorf("DSN: got %q, want %q", result["DSN"], want)
	}
}

func TestResolveValue_NoVars(t *testing.T) {
	result := resolveValue("plain-value", nil)
	if result != "plain-value" {
		t.Errorf("got %q, want %q", result, "plain-value")
	}
}
