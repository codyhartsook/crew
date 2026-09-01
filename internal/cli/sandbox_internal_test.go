package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dir = "/Users/x/.multiplayer"

func TestAddWritableRoot(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		changed bool
		want    []string
	}{
		{
			name:    "appends to an existing list",
			in:      "[sandbox_workspace_write]\nwritable_roots = [\"/a\", \"/b\"]\n",
			changed: true,
			want:    []string{`writable_roots = ["/Users/x/.multiplayer", "/a", "/b"]`},
		},
		{
			name:    "handles an empty list",
			in:      "[sandbox_workspace_write]\nwritable_roots = []\n",
			changed: true,
			want:    []string{`writable_roots = ["/Users/x/.multiplayer"]`},
		},
		{
			name:    "adds the key when the section has none",
			in:      "[sandbox_workspace_write]\nexclude_tmpdir_env_var = true\n",
			changed: true,
			want:    []string{sandboxSection, `writable_roots = ["/Users/x/.multiplayer"]`, "exclude_tmpdir_env_var = true"},
		},
		{
			name:    "adds the section when absent",
			in:      "model = \"gpt-5.6\"\n",
			changed: true,
			want:    []string{`model = "gpt-5.6"`, sandboxSection, `writable_roots = ["/Users/x/.multiplayer"]`},
		},
		{
			name:    "already present",
			in:      "[sandbox_workspace_write]\nwritable_roots = [\"/Users/x/.multiplayer\"]\n",
			changed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := addWritableRoot(tc.in, dir)
			if changed != tc.changed {
				t.Fatalf("changed = %v, want %v\n%s", changed, tc.changed, got)
			}
			if !changed {
				if got != tc.in {
					t.Errorf("content changed when it should not have:\n%s", got)
				}
				return
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("result is missing %q:\n%s", want, got)
				}
			}
		})
	}
}

// Comments and unrelated settings are why this edits text instead of
// round-tripping through a TOML encoder.
func TestAddWritableRootPreservesTheRestOfTheFile(t *testing.T) {
	in := `# Routes through the local agentgateway proxy.
model = "gpt-5.6-terra"

[model_providers.agentgateway]
name = "Solo agentgateway"

[sandbox_workspace_write]
writable_roots = ["/a"]

[features]
hooks = true
`
	got, changed := addWritableRoot(in, dir)
	if !changed {
		t.Fatal("expected a change")
	}
	for _, want := range []string{
		"# Routes through the local agentgateway proxy.",
		`model = "gpt-5.6-terra"`,
		"[model_providers.agentgateway]",
		`name = "Solo agentgateway"`,
		"[features]",
		"hooks = true",
		`writable_roots = ["/Users/x/.multiplayer", "/a"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q:\n%s", want, got)
		}
	}
}

// A writable_roots under some other table is not ours to edit.
func TestAddWritableRootIgnoresOtherTables(t *testing.T) {
	in := "[other]\nwritable_roots = [\"/a\"]\n\n[sandbox_workspace_write]\nwritable_roots = [\"/b\"]\n"
	got, changed := addWritableRoot(in, dir)
	if !changed {
		t.Fatal("expected a change")
	}
	if !strings.Contains(got, "[other]\nwritable_roots = [\"/a\"]") {
		t.Errorf("the other table was modified:\n%s", got)
	}
	if !strings.Contains(got, `writable_roots = ["/Users/x/.multiplayer", "/b"]`) {
		t.Errorf("the sandbox table was not updated:\n%s", got)
	}
}

func TestRemoveWritableRoot(t *testing.T) {
	cases := map[string]struct{ before, after string }{
		"first of several": {`writable_roots = ["/Users/x/.multiplayer", "/a", "/b"]`, `writable_roots = ["/a", "/b"]`},
		"last of several":  {`writable_roots = ["/a", "/Users/x/.multiplayer"]`, `writable_roots = ["/a"]`},
		"the only one":     {`writable_roots = ["/Users/x/.multiplayer"]`, `writable_roots = []`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			body := "# comment\nmodel = \"x\"\n\n[sandbox_workspace_write]\n" + tc.before + "\n"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			removed, err := removeWritableRoot(path, dir, false)
			if err != nil || !removed {
				t.Fatalf("removeWritableRoot = (%v, %v), want removed", removed, err)
			}
			got, _ := os.ReadFile(path)
			if !strings.Contains(string(got), tc.after) {
				t.Errorf("got:\n%s\nwant a line matching %s", got, tc.after)
			}
			if !strings.Contains(string(got), "# comment") {
				t.Error("the comment was lost")
			}
			// Removing what is not there changes nothing.
			if removed, err := removeWritableRoot(path, dir, false); err != nil || removed {
				t.Errorf("second removal = (%v, %v), want no change", removed, err)
			}
		})
	}
}
