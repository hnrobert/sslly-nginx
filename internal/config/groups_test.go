package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeProxy(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, proxyConfigFile), []byte(content), 0666); err != nil {
		t.Fatal(err)
	}
}

func TestLoadGroupsFlatten(t *testing.T) {
	dir := t.TempDir()
	writeProxy(t, dir, `# mixed layout
1234:
  - top.example.com

class1:
  1234:
    - asd.asd.com        # same key as top level: listeners merge
  9090:
    - b.example.com

a.b:                     # flat dotted group == nested a: { b: { ... } }
  8080:
    - c.example.com

outer:
  inner:
    7070:
      - d.example.com
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Cross-group + top-level merge for 1234, in declaration order.
	if got := cfg.Ports["1234"]; !reflect.DeepEqual(got, []string{"top.example.com", "asd.asd.com"}) {
		t.Fatalf("1234 listeners = %v", got)
	}
	if got := cfg.Ports["9090"]; !reflect.DeepEqual(got, []string{"b.example.com"}) {
		t.Fatalf("9090 listeners = %v", got)
	}
	if got := cfg.Ports["8080"]; !reflect.DeepEqual(got, []string{"c.example.com"}) {
		t.Fatalf("8080 listeners = %v", got)
	}
	if got := cfg.Ports["7070"]; !reflect.DeepEqual(got, []string{"d.example.com"}) {
		t.Fatalf("7070 listeners = %v", got)
	}

	// OrderedPorts: flattened, first-appearance order, no group names.
	want := []string{"1234", "9090", "8080", "7070"}
	if !reflect.DeepEqual(cfg.OrderedPorts, want) {
		t.Fatalf("OrderedPorts = %v, want %v", cfg.OrderedPorts, want)
	}

	// EntryGroups: dotted paths, declaration order; top-level-only keys absent.
	if !reflect.DeepEqual(cfg.EntryGroups["1234"], []string{"class1"}) {
		t.Fatalf("EntryGroups[1234] = %v", cfg.EntryGroups["1234"])
	}
	if !reflect.DeepEqual(cfg.EntryGroups["8080"], []string{"a.b"}) {
		t.Fatalf("EntryGroups[8080] = %v", cfg.EntryGroups["8080"])
	}
	if !reflect.DeepEqual(cfg.EntryGroups["7070"], []string{"outer.inner"}) {
		t.Fatalf("EntryGroups[7070] = %v", cfg.EntryGroups["7070"])
	}
	if _, ok := cfg.EntryGroups["9090"]; !ok {
		t.Fatalf("EntryGroups[9090] missing: %+v", cfg.EntryGroups)
	}
}

func TestLoadGroupsFlatAndNestedMerge(t *testing.T) {
	dir := t.TempDir()
	writeProxy(t, dir, `
a:
  b:
    8080:
      - nested.example.com
a.b:
  9090:
    - flat.example.com
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg.EntryGroups["8080"], []string{"a.b"}) || !reflect.DeepEqual(cfg.EntryGroups["9090"], []string{"a.b"}) {
		t.Fatalf("flat and nested forms must both resolve to a.b: %+v", cfg.EntryGroups)
	}
}

func TestLoadGroupValidation(t *testing.T) {
	cases := map[string]string{
		"bad segment char": "bad-name!: \n  8080: [a.com]\n",
		"empty segment":    "a..b: \n  8080: [a.com]\n",
		"reserved nested":  "ok: \n  log: \n    8080: [a.com]\n",
		"reserved deep":    "ok: \n  fine: \n    no_trailing_slash: \n      8080: [a.com]\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeProxy(t, dir, "1234:\n  - a.com\n"+content)
			if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "invalid group") {
				t.Fatalf("expected invalid-group error, got %v", err)
			}
		})
	}
}

func TestLoadHostnameKeyWithDotsNotAGroup(t *testing.T) {
	dir := t.TempDir()
	writeProxy(t, dir, `
example-server.local:8080:
  - host.example.com
`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Ports["example-server.local:8080"]; !reflect.DeepEqual(got, []string{"host.example.com"}) {
		t.Fatalf("hostname upstream key mis-parsed: %v", got)
	}
	if len(cfg.EntryGroups) != 0 {
		t.Fatalf("no groups expected: %+v", cfg.EntryGroups)
	}
}

func TestSplitGroupPath(t *testing.T) {
	if segs, err := SplitGroupPath(""); err != nil || segs != nil {
		t.Fatalf("empty group: %v %v", segs, err)
	}
	if segs, err := SplitGroupPath("a.b-c_d"); err != nil || !reflect.DeepEqual(segs, []string{"a", "b-c_d"}) {
		t.Fatalf("a.b-c_d: %v %v", segs, err)
	}
	for _, bad := range []string{"a..b", ".a", "a.", "bad!", "cors"} {
		if _, err := SplitGroupPath(bad); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestEditorGroupSetAndDelete(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir, proxyExampleFile, proxyConfigFile)
	ed := NewEditor(dir)

	// Create entries in nested groups (canonical nested form on disk).
	if _, err := ed.SetProxyEntry("class1", "9099", []string{"a.example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.SetProxyEntry("x.y", "7070", []string{"deep.example.com"}); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(filepath.Join(dir, proxyConfigFile))
	s := string(out)
	for _, want := range []string{
		"class1:", "x:", "  y:", "9099:", "7070:", "deep.example.com",
		"# Format 1: Port only", // original comments survive
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q in output:\n%s", want, s)
		}
	}

	// Load flattens them with the right groups.
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.EntryGroups["9099"], []string{"class1"}) ||
		!reflect.DeepEqual(cfg.EntryGroups["7070"], []string{"x.y"}) {
		t.Fatalf("EntryGroups wrong: %+v", cfg.EntryGroups)
	}
	if got := cfg.Ports["7070"]; !reflect.DeepEqual(got, []string{"deep.example.com"}) {
		t.Fatalf("7070 listeners = %v", got)
	}

	// Deleting the only entry prunes the whole (now empty) group chain.
	if _, err := ed.DeleteProxyEntry("x.y", "7070"); err != nil {
		t.Fatal(err)
	}
	out, _ = os.ReadFile(filepath.Join(dir, proxyConfigFile))
	s = string(out)
	if strings.Contains(s, "7070") || strings.Contains(s, "deep.example.com") {
		t.Fatalf("deep group entry not deleted:\n%s", s)
	}
	if strings.Contains(s, "\ny:") { // the intermediate y: mapping must be pruned too
		t.Fatalf("empty intermediate group not pruned:\n%s", s)
	}

	// class1 keeps its entry; deleting a missing (group, key) is NotFound.
	if _, err := ed.DeleteProxyEntry("class1", "missing"); err == nil {
		t.Fatal("expected NotFound")
	}
	if _, err := ed.DeleteProxyEntry("nosuch", "9099"); err == nil {
		t.Fatal("expected NotFound for missing group")
	}

	// Set into an existing flat-dotted group merges into the same nested tree.
	if _, err := ed.SetProxyEntry("class1", "9099", []string{"updated.example.com"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Ports["9099"]; !reflect.DeepEqual(got, []string{"updated.example.com"}) {
		t.Fatalf("update in group failed: %v", got)
	}

	// Invalid group names are rejected.
	if _, err := ed.SetProxyEntry("bad!", "1", []string{"a.com"}); err == nil {
		t.Fatal("invalid group must be rejected")
	}
}
