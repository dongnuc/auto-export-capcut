package capcutreg

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"E:/2026_Car_dongleg123/Final": "E:" + bs + "2026_Car_dongleg123" + bs + "Final",
		"E:/a/b/":                      "E:" + bs + "a" + bs + "b",
		"  C:/Users/x/Videos  ":        "C:" + bs + "Users" + bs + "x" + bs + "Videos",
		"":                             "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q)=%q want %q", in, got, want)
		}
	}
	if !SamePath("E:/A/B", "e:"+bs+"a"+bs+"b"+bs) {
		t.Error("SamePath should ignore case and slash style")
	}
}

// Reads a throw-away key shaped like CapCut's, so the real one is untouched.
func TestReadExportAt(t *testing.T) {
	sub := strings.Join([]string{"Software", "CapCutAutoExporterTest", "Export"}, bs)
	k, _, err := registry.CreateKey(registry.CURRENT_USER, sub, registry.SET_VALUE)
	if err != nil {
		t.Skip("cannot create a test registry key")
	}
	defer registry.DeleteKey(registry.CURRENT_USER, sub)
	defer registry.DeleteKey(registry.CURRENT_USER, strings.Join([]string{"Software", "CapCutAutoExporterTest"}, bs))
	k.SetStringValue("ExportPath", "D:/Videos/Out")
	k.SetStringValue("Resolution", "1080P")
	k.SetStringValue("codec", "HEVC")
	k.Close()
	e, err := ReadExportAt(sub)
	if err != nil || e.Path != "D:"+bs+"Videos"+bs+"Out" || e.Resolution != "1080P" || e.Codec != "HEVC" {
		t.Fatalf("%+v %v", e, err)
	}
	if _, err := ReadExportAt(sub + bs + "missing"); err == nil {
		t.Fatal("missing key must be an error")
	}
}

// The real key, read-only: skipped on machines without CapCut.
func TestReadRealExport(t *testing.T) {
	e, err := ReadExport()
	if err != nil {
		t.Skip("CapCut has no saved export folder here")
	}
	if e.Path == "" || strings.Contains(e.Path, "/") {
		t.Fatalf("unexpected path %q", e.Path)
	}
	t.Logf("CapCut export folder: %s (%s, %s)", e.Path, e.Resolution, e.Codec)
}
