package drafts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) string {
	root := t.TempDir()
	for _, n := range []string{"A", "Bóng đá"} {
		os.MkdirAll(filepath.Join(root, n), 0o755)
		os.WriteFile(filepath.Join(root, n, "draft_meta_info.json"), []byte(`{"draft_name":"`+n+`","tm_draft_modified":100,"x":"a&b"}`), 0o644)
	}
	fp := func(n string) string { return filepath.ToSlash(filepath.Join(root, n)) }
	js := `{"all_draft_store":[` +
		`{"draft_id":"1","draft_name":"A","draft_fold_path":"` + fp("A") + `","tm_duration":5000000,"tm_draft_modified":1794489260172709,"draft_is_invisible":false},` +
		`{"draft_id":"2","draft_name":"Bóng đá","draft_fold_path":"` + fp("Bóng đá") + `","tm_duration":1180000000,"tm_draft_modified":1694489260172709,"draft_is_invisible":false},` +
		`{"draft_id":"3","draft_name":"Gone","draft_fold_path":"` + fp("Gone") + `","tm_duration":1,"tm_draft_modified":1,"draft_is_invisible":false}` +
		`],"draft_ids":3,"root_path":"` + filepath.ToSlash(root) + `"}`
	os.WriteFile(filepath.Join(root, rootMetaFile), []byte(js), 0o644)
	return root
}

func TestListAndPromote(t *testing.T) {
	root := setup(t)
	ds, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[0].Name != "A" {
		t.Fatalf("got %+v", ds)
	}
	if ds[1].Duration().Seconds() != 1180 {
		t.Fatalf("duration %v", ds[1].Duration())
	}
	if _, err := Backup(root); err != nil {
		t.Fatal(err)
	}
	if err := PromoteToTop(root, ds[1]); err != nil {
		t.Fatal(err)
	}
	ds2, _ := List(root)
	if ds2[0].Name != "Bóng đá" {
		t.Fatalf("not promoted: %+v", ds2)
	}
	// Untouched fields and big integers must survive the rewrite.
	raw, _ := os.ReadFile(filepath.Join(root, rootMetaFile))
	if !strings.Contains(string(raw), `"tm_draft_modified":1794489260172709`) || !strings.Contains(string(raw), `"draft_ids":3`) {
		t.Fatalf("lost data: %s", raw)
	}
	meta, _ := os.ReadFile(filepath.Join(root, "Bóng đá", "draft_meta_info.json"))
	if strings.Contains(string(meta), `"tm_draft_modified":100`) || !strings.Contains(string(meta), `"a&b"`) {
		t.Fatalf("meta not updated correctly: %s", meta)
	}
}

func TestRename(t *testing.T) {
	root := setup(t)
	// add the fields CapCut stores that must follow a rename
	rp := filepath.Join(root, rootMetaFile)
	raw, _ := os.ReadFile(rp)
	fp := filepath.ToSlash(filepath.Join(root, "Bóng đá"))
	txt := strings.Replace(string(raw), `"draft_id":"2",`, `"draft_id":"2","draft_json_file":"`+fp+`\\draft_content.json","draft_cover":"`+fp+`\\draft_cover.jpg",`, 1)
	os.WriteFile(rp, []byte(txt), 0o644)

	ds, _ := List(root)
	var b Draft
	for _, d := range ds {
		if d.Name == "Bóng đá" {
			b = d
		}
	}
	if _, err := ValidateName(root, b, "A"); err == nil {
		t.Fatal("duplicate name accepted")
	}
	for _, bad := range []string{"x/y", "con", "NUL.txt", "a.", "  ", "a	b"} {
		if _, err := ValidateName(root, b, bad); err == nil {
			t.Fatalf("bad name %q accepted", bad)
		}
	}
	os.MkdirAll(filepath.Join(root, "Orphan"), 0o755)
	if _, err := ValidateName(root, b, "Orphan"); err == nil {
		t.Fatal("name of an existing unlisted folder accepted")
	}
	name, err := ValidateName(root, b, "  Bóng đá Final  ")
	if err != nil || name != "Bóng đá Final" {
		t.Fatalf("%q %v", name, err)
	}
	if err := Rename(root, b, name); err != nil {
		t.Fatal(err)
	}

	// the folder moved, the old one is gone
	if _, err := os.Stat(filepath.Join(root, "Bóng đá")); err == nil {
		t.Fatal("old folder still there")
	}
	nf := filepath.Join(root, "Bóng đá Final")
	if _, err := os.Stat(filepath.Join(nf, "draft_meta_info.json")); err != nil {
		t.Fatalf("meta not in new folder: %v", err)
	}
	// the list shows the new name and folder
	ds2, _ := List(root)
	found := false
	for _, d := range ds2 {
		if d.Name == "Bóng đá Final" && d.Folder == nf {
			found = true
		}
	}
	if !found {
		t.Fatalf("rename not visible: %+v", ds2)
	}
	// root entry paths follow, other fields and big integers survive
	got, _ := os.ReadFile(rp)
	g := string(got)
	for _, want := range []string{
		`"draft_name":"Bóng đá Final"`,
		`"draft_fold_path":"` + filepath.ToSlash(nf) + `"`,
		`"draft_json_file":"` + filepath.ToSlash(nf) + `\\draft_content.json"`,
		`"draft_cover":"` + filepath.ToSlash(nf) + `\\draft_cover.jpg"`,
		`"tm_draft_modified":1694489260172709`, `"draft_ids":3`,
	} {
		if !strings.Contains(g, want) {
			t.Errorf("root meta lacks %s\n%s", want, g)
		}
	}
	meta, _ := os.ReadFile(filepath.Join(nf, "draft_meta_info.json"))
	if !strings.Contains(string(meta), `"draft_name":"Bóng đá Final"`) || !strings.Contains(string(meta), `"a&b"`) {
		t.Fatalf("meta: %s", meta)
	}
}

func TestRenameCaseOnlyAndRollback(t *testing.T) {
	root := setup(t)
	ds, _ := List(root)
	var a Draft
	for _, d := range ds {
		if d.Name == "A" {
			a = d
		}
	}
	if _, err := ValidateName(root, a, "a"); err != nil {
		t.Fatalf("case-only rename rejected: %v", err)
	}
	// Failure after the folder moved (unwritable root meta) puts the folder back.
	rp := filepath.Join(root, rootMetaFile)
	os.Chmod(rp, 0o444)
	defer os.Chmod(rp, 0o644)
	if err := os.WriteFile(filepath.Join(root, "probe"), nil, 0o644); err != nil {
		t.Skip("cannot create files")
	}
	if err := Rename(root, a, "A2"); err == nil {
		// Windows lets os.Rename replace a read-only file; then there is nothing to roll back.
		t.Skip("root meta was writable on this platform")
	}
	if _, err := os.Stat(a.Folder); err != nil {
		t.Fatalf("folder not restored after failed rename: %v", err)
	}
}
