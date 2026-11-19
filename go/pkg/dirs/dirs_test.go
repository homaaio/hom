package dirs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootIsPortable(t *testing.T) {
	root := t.TempDir()
	d, err := Resolve(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Portable || d.Root != root || d.Config != root ||
		d.Data != filepath.Join(root, "hom_data") || d.Cache != filepath.Join(root, "hom_data", "dl") {
		t.Errorf("got %+v", d)
	}
}

func TestPortableMarkers(t *testing.T) {
	for _, marker := range []string{"hom.cfg", "hom.portable"} {
		exe := t.TempDir()
		os.WriteFile(filepath.Join(exe, marker), nil, 0o644)
		d, err := Resolve("", exe)
		if err != nil || !d.Portable || d.Config != exe {
			t.Errorf("%s: %+v, %v", marker, d, err)
		}
	}
}

func TestLegacyCfgFolder(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "cfg"), 0o755)
	d, _ := Resolve(root, "")
	if d.Config != filepath.Join(root, "cfg") {
		t.Errorf("an existing cfg folder keeps the settings, got %s", d.Config)
	}
}

func TestSystemFolders(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	d, err := Resolve("", t.TempDir()) // an exe folder without a marker
	if err != nil {
		t.Skip("no home folder here: ", err)
	}
	if d.Portable || d.Config == "" || d.Data == "" || d.Cache == "" {
		t.Errorf("got %+v", d)
	}
}

func TestSetData(t *testing.T) {
	d, _ := Resolve(t.TempDir(), "")
	d.SetData("/somewhere")
	if d.Data != "/somewhere" || d.Cache != filepath.Join("/somewhere", "dl") {
		t.Errorf("a cache inside the data folder follows it: %+v", d)
	}
	sys := &Dirs{Data: "/a", Cache: "/c"}
	sys.SetData("/b")
	if sys.Cache != "/c" {
		t.Errorf("a separate cache stays: %+v", sys)
	}
}
