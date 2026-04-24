package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ToPascalCase
// ---------------------------------------------------------------------------

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"demo", "Demo"},
		{"combat_stats", "CombatStats"},
		{"Demo", "Demo"},
		{"a_b_c", "ABC"},
		{"health", "Health"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, ToPascalCase(tt.in))
		})
	}
}

// ---------------------------------------------------------------------------
// ParseConfigs - single file
// ---------------------------------------------------------------------------

func TestParseTOML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "attrs.toml")
	data := `[demo]
set_id = 1

[demo.fields]
Hp = { id = 0, type = "instant" }
Mana = { id = 1, type = "instant" }
MaxHp = { id = 2, type = "attribute" }
MaxMana = { id = 3, type = "attribute" }
Attack = { id = 4, type = "attribute" }
Defense = { id = 5, type = "attribute" }
`
	require.NoError(t, os.WriteFile(p, []byte(data), 0644))

	cfgs, err := ParseConfigs([]string{p})
	require.NoError(t, err)

	require.Contains(t, cfgs, "demo")
	cfg := cfgs["demo"]
	assert.Equal(t, uint32(1), cfg.SetID)
	assert.Len(t, cfg.Fields, 6)
	assert.Equal(t, FieldDef{ID: 0, Type: "instant"}, cfg.Fields["Hp"])
	assert.Equal(t, FieldDef{ID: 1, Type: "instant"}, cfg.Fields["Mana"])
	assert.Equal(t, FieldDef{ID: 2, Type: "attribute"}, cfg.Fields["MaxHp"])
	assert.Equal(t, FieldDef{ID: 5, Type: "attribute"}, cfg.Fields["Defense"])
}

// ---------------------------------------------------------------------------
// ParseConfigs - multiple files
// ---------------------------------------------------------------------------

func TestParseTOML_MultipleFiles(t *testing.T) {
	dir := t.TempDir()

	p1 := filepath.Join(dir, "health.toml")
	require.NoError(t, os.WriteFile(p1, []byte(`[health]
set_id = 1

[health.fields]
Hp = { id = 0, type = "instant" }
MaxHp = { id = 1, type = "attribute" }
`), 0644))

	p2 := filepath.Join(dir, "combat.toml")
	require.NoError(t, os.WriteFile(p2, []byte(`[combat]
set_id = 2

[combat.fields]
Attack = { id = 0, type = "attribute" }
Defense = { id = 1, type = "attribute" }
`), 0644))

	cfgs, err := ParseConfigs([]string{p1, p2})
	require.NoError(t, err)

	require.Contains(t, cfgs, "health")
	h := cfgs["health"]
	assert.Equal(t, uint32(1), h.SetID)
	assert.Len(t, h.Fields, 2)
	assert.Equal(t, FieldDef{ID: 0, Type: "instant"}, h.Fields["Hp"])
	assert.Equal(t, FieldDef{ID: 1, Type: "attribute"}, h.Fields["MaxHp"])

	require.Contains(t, cfgs, "combat")
	c := cfgs["combat"]
	assert.Equal(t, uint32(2), c.SetID)
	assert.Len(t, c.Fields, 2)
	assert.Equal(t, FieldDef{ID: 0, Type: "attribute"}, c.Fields["Attack"])
	assert.Equal(t, FieldDef{ID: 1, Type: "attribute"}, c.Fields["Defense"])
}

// ---------------------------------------------------------------------------
// ParseConfigs - duplicate section across files
// ---------------------------------------------------------------------------

func TestParseTOML_DuplicateSection(t *testing.T) {
	dir := t.TempDir()

	p1 := filepath.Join(dir, "a.toml")
	require.NoError(t, os.WriteFile(p1, []byte(`[demo]
set_id = 1

[demo.fields]
Hp = { id = 0, type = "instant" }
`), 0644))

	p2 := filepath.Join(dir, "b.toml")
	require.NoError(t, os.WriteFile(p2, []byte(`[demo]
set_id = 2

[demo.fields]
Mana = { id = 0, type = "instant" }
`), 0644))

	_, err := ParseConfigs([]string{p1, p2})
	require.Error(t, err)
}
// ---------------------------------------------------------------------------
// Validate - valid config
// ---------------------------------------------------------------------------

func TestValidate_Valid(t *testing.T) {
	cfgs := map[string]SetConfig{
		"demo": {
			SetID: 1,
			Fields: map[string]FieldDef{
				"Hp":      {ID: 0, Type: "instant"},
				"Mana":    {ID: 1, Type: "instant"},
				"MaxHp":   {ID: 2, Type: "attribute"},
				"MaxMana": {ID: 3, Type: "attribute"},
				"Attack":  {ID: 4, Type: "attribute"},
				"Defense": {ID: 5, Type: "attribute"},
			},
		},
	}
	assert.NoError(t, Validate(cfgs))
}

// ---------------------------------------------------------------------------
// Validate - set_id = 0
// ---------------------------------------------------------------------------

func TestValidate_SetIDZero(t *testing.T) {
	cfgs := map[string]SetConfig{
		"demo": {
			SetID: 0,
			Fields: map[string]FieldDef{
				"Hp": {ID: 0, Type: "instant"},
			},
		},
	}
	err := Validate(cfgs)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "set_id")
}

// ---------------------------------------------------------------------------
// Validate - set_id too large
// ---------------------------------------------------------------------------

func TestValidate_SetIDTooLarge(t *testing.T) {
	cfgs := map[string]SetConfig{
		"demo": {
			SetID: 65536,
			Fields: map[string]FieldDef{
				"Hp": {ID: 0, Type: "instant"},
			},
		},
	}
	err := Validate(cfgs)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Validate - too many fields (>64)
// ---------------------------------------------------------------------------

func TestValidate_TooManyFields(t *testing.T) {
	fields := make(map[string]FieldDef)
	for i := 0; i < 65; i++ {
		fields[fmt.Sprintf("F%d", i)] = FieldDef{ID: uint16(i), Type: "instant"}
	}
	cfgs := map[string]SetConfig{
		"big": {SetID: 1, Fields: fields},
	}
	err := Validate(cfgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "64")
}

// ---------------------------------------------------------------------------
// Validate - exactly 64 fields (max)
// ---------------------------------------------------------------------------

func TestValidate_ExactlyMaxFields(t *testing.T) {
	fields := make(map[string]FieldDef)
	for i := 0; i < 64; i++ {
		fields[fmt.Sprintf("F%d", i)] = FieldDef{ID: uint16(i), Type: "instant"}
	}
	cfgs := map[string]SetConfig{
		"full": {SetID: 1, Fields: fields},
	}
	assert.NoError(t, Validate(cfgs))
}

// ---------------------------------------------------------------------------
// Validate - no fields at all
// ---------------------------------------------------------------------------

func TestValidate_NoFields(t *testing.T) {
	cfgs := map[string]SetConfig{
		"empty": {
			SetID:  1,
			Fields: map[string]FieldDef{},
		},
	}
	err := Validate(cfgs)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Validate - duplicate set_id across sets
// ---------------------------------------------------------------------------

func TestValidate_DuplicateSetID(t *testing.T) {
	cfgs := map[string]SetConfig{
		"alpha": {
			SetID: 1,
			Fields: map[string]FieldDef{
				"Hp": {ID: 0, Type: "instant"},
			},
		},
		"beta": {
			SetID: 1,
			Fields: map[string]FieldDef{
				"Mana": {ID: 0, Type: "instant"},
			},
		},
	}
	err := Validate(cfgs)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "duplicate")
}

// ---------------------------------------------------------------------------
// Validate - duplicate field ID within a set
// ---------------------------------------------------------------------------

func TestValidate_DuplicateFieldID(t *testing.T) {
	cfgs := map[string]SetConfig{
		"dup": {
			SetID: 1,
			Fields: map[string]FieldDef{
				"Hp":   {ID: 0, Type: "instant"},
				"Mana": {ID: 0, Type: "instant"},
			},
		},
	}
	err := Validate(cfgs)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "duplicate")
}

// ---------------------------------------------------------------------------
// Validate - non-contiguous field IDs
// ---------------------------------------------------------------------------

func TestValidate_NonContiguousIDs(t *testing.T) {
	cfgs := map[string]SetConfig{
		"gap": {
			SetID: 1,
			Fields: map[string]FieldDef{
				"Hp":   {ID: 0, Type: "instant"},
				"Mana": {ID: 2, Type: "instant"},
			},
		},
	}
	err := Validate(cfgs)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Validate - invalid field type
// ---------------------------------------------------------------------------

func TestValidate_InvalidFieldType(t *testing.T) {
	cfgs := map[string]SetConfig{
		"bad": {
			SetID: 1,
			Fields: map[string]FieldDef{
				"Hp": {ID: 0, Type: "bogus"},
			},
		},
	}
	err := Validate(cfgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid type")
}

// ---------------------------------------------------------------------------
// Validate - unexported (lowercase) field name
// ---------------------------------------------------------------------------

func TestValidate_UnexportedFieldName(t *testing.T) {
	cfgs := map[string]SetConfig{
		"bad": {
			SetID: 1,
			Fields: map[string]FieldDef{
				"hp": {ID: 0, Type: "instant"},
			},
		},
	}
	err := Validate(cfgs)
	require.Error(t, err)
}
// ---------------------------------------------------------------------------
// Generate - valid Go output for demo config
// ---------------------------------------------------------------------------

func TestGenerate_ValidGo(t *testing.T) {
	cfg := SetConfig{
		SetID: 1,
		Fields: map[string]FieldDef{
			"Hp":      {ID: 0, Type: "instant"},
			"Mana":    {ID: 1, Type: "instant"},
			"MaxHp":   {ID: 2, Type: "attribute"},
			"MaxMana": {ID: 3, Type: "attribute"},
			"Attack":  {ID: 4, Type: "attribute"},
			"Defense": {ID: 5, Type: "attribute"},
		},
	}

	var buf bytes.Buffer
	err := Generate(&buf, "demo", "demo", cfg)
	require.NoError(t, err)

	src := buf.String()

	// Must be valid Go.
	_, fmtErr := format.Source(buf.Bytes())
	assert.NoError(t, fmtErr, "generated code is not valid Go:\n%s", src)

	// Key symbols must appear.
	for _, want := range []string{
		"DemoAttributeSet",
		"DemoSetID",
		"DemoField_Hp",
		"DemoAttrKey_Hp",
		"DemoDirty_Hp",
		"GetDemoAttrs",
		"gas.AttributeSet",
		"gas.AttributeValue",
		"gas.AttrMap",
	} {
		assert.Contains(t, src, want, "missing expected string %q in generated output", want)
	}
}

// ---------------------------------------------------------------------------
// Generate - only instant values
// ---------------------------------------------------------------------------

func TestGenerate_OnlyInstant(t *testing.T) {
	cfg := SetConfig{
		SetID: 1,
		Fields: map[string]FieldDef{
			"Score": {ID: 0, Type: "instant"},
			"Level": {ID: 1, Type: "instant"},
		},
	}

	var buf bytes.Buffer
	err := Generate(&buf, "score", "score", cfg)
	require.NoError(t, err)

	src := buf.String()

	_, fmtErr := format.Source(buf.Bytes())
	assert.NoError(t, fmtErr, "generated code is not valid Go:\n%s", src)

	assert.Contains(t, src, "ScoreAttributeSet")

	// The struct should not contain gas.AttributeValue fields.
	structIdx := strings.Index(src, "type ScoreAttributeSet struct")
	if structIdx >= 0 {
		rest := src[structIdx:]
		openBrace := strings.Index(rest, "{")
		if openBrace >= 0 {
			closeBrace := strings.Index(rest[openBrace:], "}")
			if closeBrace >= 0 {
				structBody := rest[openBrace : openBrace+closeBrace+1]
				assert.NotContains(t, structBody, "gas.AttributeValue",
					"struct body should not contain gas.AttributeValue when there are no attribute fields")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Generate - only attribute values
// ---------------------------------------------------------------------------

func TestGenerate_OnlyAttribute(t *testing.T) {
	cfg := SetConfig{
		SetID: 2,
		Fields: map[string]FieldDef{
			"Strength": {ID: 0, Type: "attribute"},
			"Agility":  {ID: 1, Type: "attribute"},
		},
	}

	var buf bytes.Buffer
	err := Generate(&buf, "stats", "stats", cfg)
	require.NoError(t, err)

	src := buf.String()

	_, fmtErr := format.Source(buf.Bytes())
	assert.NoError(t, fmtErr, "generated code is not valid Go:\n%s", src)

	assert.Contains(t, src, "gas.AttributeValue",
		"output should contain AttributeValue fields")
}

// ---------------------------------------------------------------------------
// Generate - explicit field IDs in output
// ---------------------------------------------------------------------------

func TestGenerate_ExplicitFieldIDs(t *testing.T) {
	cfg := SetConfig{
		SetID: 1,
		Fields: map[string]FieldDef{
			"Hp":    {ID: 0, Type: "instant"},
			"MaxHp": {ID: 1, Type: "attribute"},
		},
	}

	var buf bytes.Buffer
	err := Generate(&buf, "test", "test", cfg)
	require.NoError(t, err)

	src := buf.String()

	_, fmtErr := format.Source(buf.Bytes())
	assert.NoError(t, fmtErr, "generated code is not valid Go:\n%s", src)

	// Should contain explicit numeric IDs, not iota
	assert.Contains(t, src, "TestField_Hp uint16 = 0")
	assert.Contains(t, src, "TestField_MaxHp uint16 = 1")
	assert.Contains(t, src, "TestFieldCount uint16 = 2")
	assert.NotContains(t, src, "iota", "should use explicit IDs, not iota")
}

// ---------------------------------------------------------------------------
// Generate - struct groups by type regardless of ID order
// ---------------------------------------------------------------------------

func TestGenerate_StructGroupsByType(t *testing.T) {
	// Attribute has ID 0, Instant has ID 1 - struct should still group by type
	cfg := SetConfig{
		SetID: 1,
		Fields: map[string]FieldDef{
			"MaxHp": {ID: 0, Type: "attribute"},
			"Hp":    {ID: 1, Type: "instant"},
		},
	}

	var buf bytes.Buffer
	err := Generate(&buf, "mixed", "mixed", cfg)
	require.NoError(t, err)

	src := buf.String()

	_, fmtErr := format.Source(buf.Bytes())
	assert.NoError(t, fmtErr, "generated code is not valid Go:\n%s", src)

	// In the struct, InstantValue section should come before AttributeValue section
	instantIdx := strings.Index(src, "// InstantValue")
	attrIdx := strings.Index(src, "// AttributeValue")
	require.Greater(t, instantIdx, 0, "should have InstantValue comment")
	require.Greater(t, attrIdx, 0, "should have AttributeValue comment")
	assert.Less(t, instantIdx, attrIdx, "InstantValue should come before AttributeValue in struct")
}

// ---------------------------------------------------------------------------
// GenerateFile - writes file to disk
// ---------------------------------------------------------------------------

func TestGenerateFile_WritesFile(t *testing.T) {
	dir := t.TempDir()
	cfg := SetConfig{
		SetID: 1,
		Fields: map[string]FieldDef{
			"Hp":      {ID: 0, Type: "instant"},
			"Mana":    {ID: 1, Type: "instant"},
			"MaxHp":   {ID: 2, Type: "attribute"},
			"MaxMana": {ID: 3, Type: "attribute"},
			"Attack":  {ID: 4, Type: "attribute"},
			"Defense": {ID: 5, Type: "attribute"},
		},
	}

	err := GenerateFile("demo", dir, "demo", cfg)
	require.NoError(t, err)

	outPath := filepath.Join(dir, "demo_attr.go")
	data, err := os.ReadFile(outPath)
	require.NoError(t, err, "expected output file %s to exist", outPath)

	_, fmtErr := format.Source(data)
	assert.NoError(t, fmtErr, "generated file is not valid Go:\n%s", string(data))
}
