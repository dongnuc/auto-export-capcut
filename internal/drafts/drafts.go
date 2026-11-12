// Package drafts lists CapCut projects and can move one to the top of the
// CapCut home screen (which is ordered by last-modified time).
package drafts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const rootMetaFile = "root_meta_info.json"
const draftMetaFile = "draft_meta_info.json"

type Draft struct {
	ID         string
	Name       string
	Folder     string
	DurationUs int64
	Modified   time.Time
}

func (d Draft) Duration() time.Duration { return time.Duration(d.DurationUs) * time.Microsecond }

func readJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // keep µs timestamps exact
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return m, nil
}

func writeJSON(path string, m map[string]any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, bytes.TrimRight(buf.Bytes(), "\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func num(m map[string]any, k string) int64 {
	switch v := m[k].(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			f, _ := v.Float64()
			return int64(f)
		}
		return n
	case float64:
		return int64(v)
	}
	return 0
}

// List reads root_meta_info.json in draftsDir. Projects whose folder no longer
// exists or that are marked invisible are skipped. Newest first.
func List(draftsDir string) ([]Draft, error) {
	root, err := readJSON(filepath.Join(draftsDir, rootMetaFile))
	if err != nil {
		return nil, err
	}
	store, _ := root["all_draft_store"].([]any)
	var out []Draft
	for _, it := range store {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if b, _ := m["draft_is_invisible"].(bool); b {
			continue
		}
		folder := filepath.FromSlash(strings.ReplaceAll(str(m, "draft_fold_path"), `\`, "/"))
		if folder == "" {
			continue
		}
		if st, err := os.Stat(folder); err != nil || !st.IsDir() {
			continue
		}
		d := Draft{
			ID:         str(m, "draft_id"),
			Name:       str(m, "draft_name"),
			Folder:     folder,
			DurationUs: num(m, "tm_duration"),
			Modified:   time.UnixMicro(num(m, "tm_draft_modified")),
		}
		if d.Name == "" {
			d.Name = filepath.Base(folder)
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

// Backup copies root_meta_info.json next to itself once per run.
func Backup(draftsDir string) (string, error) {
	src := filepath.Join(draftsDir, rootMetaFile)
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(draftsDir, fmt.Sprintf("%s.autoexport-%s.bak", rootMetaFile, time.Now().Format("20060102-150405")))
	return dst, os.WriteFile(dst, data, 0o644)
}

// PromoteToTop gives the draft a modified time newer than every other draft,
// in both root_meta_info.json and the draft's own draft_meta_info.json, so it
// becomes the first card on the CapCut home screen. CapCut must be closed.
func PromoteToTop(draftsDir string, d Draft) error {
	rootPath := filepath.Join(draftsDir, rootMetaFile)
	root, err := readJSON(rootPath)
	if err != nil {
		return err
	}
	store, _ := root["all_draft_store"].([]any)
	var maxMod int64
	var target map[string]any
	for _, it := range store {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if t := num(m, "tm_draft_modified"); t > maxMod {
			maxMod = t
		}
		if (d.ID != "" && str(m, "draft_id") == d.ID) ||
			(d.ID == "" && sameFolder(str(m, "draft_fold_path"), d.Folder)) {
			target = m
		}
	}
	if target == nil {
		return fmt.Errorf("không tìm thấy project %q trong %s", d.Name, rootMetaFile)
	}
	now := time.Now().UnixMicro()
	if now <= maxMod {
		now = maxMod + 1_000_000
	}
	target["tm_draft_modified"] = json.Number(fmt.Sprint(now))
	if err := writeJSON(rootPath, root); err != nil {
		return err
	}

	metaPath := filepath.Join(d.Folder, draftMetaFile)
	if meta, err := readJSON(metaPath); err == nil {
		meta["tm_draft_modified"] = json.Number(fmt.Sprint(now))
		if err := writeJSON(metaPath, meta); err != nil {
			return err
		}
	}
	return nil
}

func sameFolder(a, b string) bool {
	clean := func(s string) string {
		return strings.ToLower(filepath.Clean(filepath.FromSlash(strings.ReplaceAll(s, `\`, "/"))))
	}
	return clean(a) == clean(b)
}
