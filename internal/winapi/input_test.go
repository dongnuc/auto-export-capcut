package winapi

import (
	"testing"
	"unsafe"
)

func TestParseKey(t *testing.T) {
	k, err := ParseKey("Ctrl+E")
	if err != nil || k.VK != 'E' || len(k.Mods) != 1 || k.Mods[0] != vkControl {
		t.Fatalf("%+v %v", k, err)
	}
	k, err = ParseKey("ctrl + shift + k")
	if err != nil || k.VK != 'K' || len(k.Mods) != 2 {
		t.Fatalf("%+v %v", k, err)
	}
	for _, s := range []string{"Enter", "Escape", "F5", "Ctrl+W", "Alt+1"} {
		if _, err := ParseKey(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{"", "Ctrl+", "Hyper+E", "Ctrl+Foo"} {
		if _, err := ParseKey(s); err == nil {
			t.Errorf("%q should fail", s)
		}
	}
}

func TestInputSize(t *testing.T) {
	if unsafe.Sizeof(keyInput{}) != 40 || unsafe.Sizeof(mouseInput{}) != 40 {
		t.Fatalf("INPUT size: key=%d mouse=%d", unsafe.Sizeof(keyInput{}), unsafe.Sizeof(mouseInput{}))
	}
}
