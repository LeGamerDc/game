package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

// FieldDef describes one field in an AttributeSet from the TOML config.
type FieldDef struct {
	ID   uint16 `toml:"id"`
	Type string `toml:"type"` // "instant" or "attribute"
}

// SetConfig describes one AttributeSet definition from the TOML config.
type SetConfig struct {
	SetID  uint32              `toml:"set_id"`
	Fields map[string]FieldDef `toml:"fields"`
}

// fieldInfo holds resolved metadata about a single field for code generation.
type fieldInfo struct {
	Name      string
	ID        uint16
	IsInstant bool
}

// resolveFields converts cfg.Fields to a sorted slice of fieldInfo.
// The returned slice is sorted by ascending field ID.
func resolveFields(cfg SetConfig) []fieldInfo {
	fields := make([]fieldInfo, 0, len(cfg.Fields))
	for name, def := range cfg.Fields {
		fields = append(fields, fieldInfo{
			Name:      name,
			ID:        def.ID,
			IsInstant: def.Type == "instant",
		})
	}
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].ID < fields[j].ID
	})
	return fields
}

// ParseConfigs reads and decodes each TOML file, merging all configs into one map.
// If the same section name appears in multiple files, an error is returned.
func ParseConfigs(paths []string) (map[string]SetConfig, error) {
	merged := make(map[string]SetConfig)
	for _, p := range paths {
		var file map[string]SetConfig
		if _, err := toml.DecodeFile(p, &file); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", p, err)
		}
		for name, cfg := range file {
			if _, exists := merged[name]; exists {
				return nil, fmt.Errorf("duplicate set name %q (found in %s)", name, p)
			}
			merged[name] = cfg
		}
	}
	return merged, nil
}

// Validate checks all validation rules on the merged config map.
func Validate(cfg map[string]SetConfig) error {
	seenIDs := make(map[uint32]string)
	for name, sc := range cfg {
		// Set name non-empty
		if name == "" {
			return fmt.Errorf("set name must be non-empty")
		}

		// set_id range: must be > 0 and <= 65535
		if sc.SetID == 0 || sc.SetID > 65535 {
			return fmt.Errorf("set %q: set_id must be > 0 and <= 65535, got %d", name, sc.SetID)
		}

		// No duplicate set_id across all sets
		if prev, ok := seenIDs[sc.SetID]; ok {
			return fmt.Errorf("duplicate set_id %d in sets %q and %q", sc.SetID, prev, name)
		}
		seenIDs[sc.SetID] = name

		// Total fields: must be > 0 and <= 64
		total := len(sc.Fields)
		if total == 0 {
			return fmt.Errorf("set %q: must have at least one field", name)
		}
		if total > 64 {
			return fmt.Errorf("set %q: total fields %d exceeds maximum of 64", name, total)
		}

		// Validate each field
		usedFieldIDs := make(map[uint16]string)
		for fname, fdef := range sc.Fields {
			// Field name validation
			if fname == "" {
				return fmt.Errorf("set %q: field name must be non-empty", name)
			}
			if !isValidGoExportedIdent(fname) {
				return fmt.Errorf("set %q: field %q is not a valid Go exported identifier", name, fname)
			}
			// Type validation
			if fdef.Type != "instant" && fdef.Type != "attribute" {
				return fmt.Errorf("set %q: field %q has invalid type %q (must be \"instant\" or \"attribute\")", name, fname, fdef.Type)
			}
			// ID range: must be < total
			if int(fdef.ID) >= total {
				return fmt.Errorf("set %q: field %q has id %d, but total fields is %d (ids must be 0..%d)", name, fname, fdef.ID, total, total-1)
			}
			// Duplicate field ID
			if prev, ok := usedFieldIDs[fdef.ID]; ok {
				return fmt.Errorf("set %q: duplicate field id %d in fields %q and %q", name, fdef.ID, prev, fname)
			}
			usedFieldIDs[fdef.ID] = fname
		}

		// Contiguous IDs check: all IDs 0..total-1 must be present
		for i := 0; i < total; i++ {
			if _, ok := usedFieldIDs[uint16(i)]; !ok {
				return fmt.Errorf("set %q: field id %d is missing (ids must be contiguous 0..%d)", name, i, total-1)
			}
		}
	}
	return nil
}

// isValidGoExportedIdent checks that s starts with an uppercase ASCII letter
// and the rest are ASCII alphanumeric characters.
func isValidGoExportedIdent(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if r < 'A' || r > 'Z' {
				return false
			}
		} else {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				return false
			}
		}
	}
	return true
}

// ToPascalCase converts a snake_case or lowercase string to PascalCase.
// Split by "_", capitalize first letter of each part, join.
func ToPascalCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, "")
}

// Generate writes the Go source code for one AttributeSet to w.
func Generate(w io.Writer, pkg string, name string, cfg SetConfig) error {
	prefix := ToPascalCase(name)
	typeName := prefix + "AttributeSet"

	// Resolve fields sorted by ID.
	fields := resolveFields(cfg)

	// Split into instant and attribute for struct layout grouping.
	var instants, attributes []fieldInfo
	for _, f := range fields {
		if f.IsInstant {
			instants = append(instants, f)
		} else {
			attributes = append(attributes, f)
		}
	}

	p := func(format string, args ...interface{}) {
		fmt.Fprintf(w, format, args...)
	}

	// ---- Header ----
	p("// Code generated by mk_attr. DO NOT EDIT.\n\n")
	p("package %s\n\n", pkg)

	// ---- Import ----
	p("import (\n")
	p("\t\"github.com/legamerdc/game/gas\"\n")
	p(")\n\n")

	// ---- Compile-time interface check ----
	p("// Compile-time interface check.\n")
	p("var _ gas.AttributeSet = (*%s)(nil)\n\n", typeName)

	// ---- Set ID ----
	p("// ---------- Set ID ----------\n\n")
	p("const %sSetID uint32 = %d\n\n", prefix, cfg.SetID)

	// ---- Field Indices (sorted by ID, explicit values) ----
	p("// ---------- Field Indices ----------\n\n")
	p("const (\n")
	for _, f := range fields {
		p("\t%sField_%s uint16 = %d\n", prefix, f.Name, f.ID)
	}
	p("\t%sFieldCount uint16 = %d\n", prefix, len(fields))
	p(")\n\n")

	// ---- Global AttrKeys ----
	p("// ---------- Global AttrKeys (SetID<<16 | FieldIndex) ----------\n\n")
	p("const (\n")
	for _, f := range fields {
		p("\t%sAttrKey_%s uint32 = (%sSetID << 16) | uint32(%sField_%s)\n",
			prefix, f.Name, prefix, prefix, f.Name)
	}
	p(")\n\n")

	// ---- Dirty Bits ----
	p("// ---------- Dirty Bits ----------\n\n")
	p("const (\n")
	for _, f := range fields {
		p("\t%sDirty_%s uint64 = 1 << %sField_%s\n", prefix, f.Name, prefix, f.Name)
	}
	p(")\n\n")

	// ---- Struct (grouped by type: instant first, then attribute) ----
	p("// ---------- Struct ----------\n\n")
	p("type %s struct {\n", typeName)
	p("\tdirty uint64\n")
	if len(instants) > 0 {
		p("\n\t// InstantValue\n")
		for _, f := range instants {
			p("\t%s float64\n", f.Name)
		}
	}
	if len(attributes) > 0 {
		p("\n\t// AttributeValue\n")
		for _, f := range attributes {
			p("\t%s gas.AttributeValue\n", f.Name)
		}
	}
	p("}\n\n")

	// ---- Bind ----
	p("// ---------- Bind ----------\n\n")
	p("// Get%sAttrs retrieves the typed *%s from an AttrMap.\n", prefix, typeName)
	p("// Returns nil if the set is not registered.\n")
	p("func Get%sAttrs(m *gas.AttrMap) *%s {\n", prefix, typeName)
	p("\tif s := m.Get(%sSetID); s != nil {\n", prefix)
	p("\t\treturn s.(*%s)\n", typeName)
	p("\t}\n")
	p("\treturn nil\n")
	p("}\n\n")

	// ---- Interface methods ----
	p("// ---------- Interface: gas.AttributeSet ----------\n\n")
	p("func (s *%s) SetID() uint32 { return %sSetID }\n", typeName, prefix)
	p("func (s *%s) FieldCount() uint16 { return %sFieldCount }\n", typeName, prefix)
	p("func (s *%s) Dirty() uint64 { return s.dirty }\n", typeName)
	p("func (s *%s) IsDirty() bool { return s.dirty != 0 }\n", typeName)
	p("func (s *%s) IsFieldDirty(bit uint64) bool { return s.dirty&bit != 0 }\n", typeName)
	p("func (s *%s) ClearDirty() { s.dirty = 0 }\n\n", typeName)

	// ---- Typed Accessors (grouped: instant first, then attribute) ----
	p("// ==============================================================\n")
	p("// Typed Accessors\n")
	p("// ==============================================================\n\n")

	for _, f := range instants {
		p("// ---------- %s (InstantValue) ----------\n\n", f.Name)
		p("func (s *%s) Get%s() float64 { return s.%s }\n", typeName, f.Name, f.Name)
		p("func (s *%s) Set%s(v float64) {\n", typeName, f.Name)
		p("\ts.dirty |= %sDirty_%s\n", prefix, f.Name)
		p("\ts.%s = v\n", f.Name)
		p("}\n\n")
	}
	for _, f := range attributes {
		p("// ---------- %s (AttributeValue) ----------\n\n", f.Name)
		p("func (s *%s) Get%s() gas.AttributeValue { return s.%s }\n", typeName, f.Name, f.Name)
		p("func (s *%s) Get%sBase() float64 { return s.%s.Base }\n", typeName, f.Name, f.Name)
		p("func (s *%s) Get%sCurrent() float64 { return s.%s.Current }\n", typeName, f.Name, f.Name)
		p("func (s *%s) Set%sBase(v float64) {\n", typeName, f.Name)
		p("\ts.dirty |= %sDirty_%s\n", prefix, f.Name)
		p("\ts.%s.Base = v\n", f.Name)
		p("}\n")
		p("func (s *%s) Set%sCurrent(v float64) {\n", typeName, f.Name)
		p("\ts.dirty |= %sDirty_%s\n", prefix, f.Name)
		p("\ts.%s.Current = v\n", f.Name)
		p("}\n\n")
	}

	// ---- Generic Field Access (sorted by ID) ----
	p("// ==============================================================\n")
	p("// Generic Field Access (by FieldIndex)\n")
	p("// ==============================================================\n\n")

	// GetCurrent
	p("// GetCurrent returns the effective value: InstantValue -> value, AttributeValue -> Current.\n")
	p("func (s *%s) GetCurrent(field uint16) (float64, bool) {\n", typeName)
	p("\tswitch field {\n")
	for _, f := range fields {
		if f.IsInstant {
			p("\tcase %sField_%s:\n", prefix, f.Name)
			p("\t\treturn s.%s, true\n", f.Name)
		} else {
			p("\tcase %sField_%s:\n", prefix, f.Name)
			p("\t\treturn s.%s.Current, true\n", f.Name)
		}
	}
	p("\t}\n")
	p("\treturn 0, false\n")
	p("}\n\n")

	// GetBase
	p("// GetBase returns the base value: InstantValue -> value, AttributeValue -> Base.\n")
	p("func (s *%s) GetBase(field uint16) (float64, bool) {\n", typeName)
	p("\tswitch field {\n")
	for _, f := range fields {
		if f.IsInstant {
			p("\tcase %sField_%s:\n", prefix, f.Name)
			p("\t\treturn s.%s, true\n", f.Name)
		} else {
			p("\tcase %sField_%s:\n", prefix, f.Name)
			p("\t\treturn s.%s.Base, true\n", f.Name)
		}
	}
	p("\t}\n")
	p("\treturn 0, false\n")
	p("}\n\n")

	// SetBase
	p("// SetBase sets the base value and marks the field dirty.\n")
	p("func (s *%s) SetBase(field uint16, v float64) bool {\n", typeName)
	p("\tswitch field {\n")
	for _, f := range fields {
		if f.IsInstant {
			p("\tcase %sField_%s:\n", prefix, f.Name)
			p("\t\ts.Set%s(v)\n", f.Name)
			p("\t\treturn true\n")
		} else {
			p("\tcase %sField_%s:\n", prefix, f.Name)
			p("\t\ts.Set%sBase(v)\n", f.Name)
			p("\t\treturn true\n")
		}
	}
	p("\t}\n")
	p("\treturn false\n")
	p("}\n\n")

	// SetCurrent
	p("// SetCurrent sets the current value and marks the field dirty.\n")
	p("func (s *%s) SetCurrent(field uint16, v float64) bool {\n", typeName)
	p("\tswitch field {\n")
	for _, f := range fields {
		if f.IsInstant {
			p("\tcase %sField_%s:\n", prefix, f.Name)
			p("\t\ts.Set%s(v)\n", f.Name)
			p("\t\treturn true\n")
		} else {
			p("\tcase %sField_%s:\n", prefix, f.Name)
			p("\t\ts.Set%sCurrent(v)\n", f.Name)
			p("\t\treturn true\n")
		}
	}
	p("\t}\n")
	p("\treturn false\n")
	p("}\n")

	return nil
}

// GenerateFile generates the Go source for one AttributeSet, formats it, and writes it to disk.
func GenerateFile(pkg, outDir, name string, cfg SetConfig) error {
	var buf bytes.Buffer
	if err := Generate(&buf, pkg, name, cfg); err != nil {
		return err
	}

	src, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("formatting generated code for %s: %w\n---raw---\n%s", name, err, buf.String())
	}

	outPath := filepath.Join(outDir, name+"_attr.go")
	return os.WriteFile(outPath, src, 0644)
}

func main() {
	pkg := flag.String("pkg", "", "package name for generated code (required)")
	outDir := flag.String("out", ".", "output directory for generated files")
	flag.Parse()

	if *pkg == "" {
		fmt.Fprintln(os.Stderr, "error: -pkg is required")
		flag.Usage()
		os.Exit(1)
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: at least one TOML file path is required")
		flag.Usage()
		os.Exit(1)
	}

	configs, err := ParseConfigs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := Validate(configs); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	for name, cfg := range configs {
		if err := GenerateFile(*pkg, *outDir, name, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error generating %s: %v\n", name, err)
			os.Exit(1)
		}
		fmt.Printf("generated %s\n", filepath.Join(*outDir, name+"_attr.go"))
	}
}
