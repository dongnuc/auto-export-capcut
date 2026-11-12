package drafts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// ValidateName checks a new project name. CapCut uses the project name as the
// project's folder name and as the default export file name, so it must be a
// valid Windows file name and must not collide with another project or an
// existing folder.
func ValidateName(draftsDir string, d Draft, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("tên không được để trống")
	}
	if strings.ContainsAny(name, `<>:"/\|?*`) {
		return "", fmt.Errorf(`tên không được chứa các ký tự < > : " / \ | ? *`)
	}
	for _, r := range name {
		if r < 32 {
			return "", fmt.Errorf("tên không được chứa ký tự điều khiển")
		}
	}
	if strings.HasSuffix(name, ".") {
		return "", fmt.Errorf("tên không được kết thúc bằng dấu chấm")
	}
	if len([]rune(name)) > 100 {
		return "", fmt.Errorf("tên quá dài (tối đa 100 ký tự)")
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if reservedNames[base] {
		return "", fmt.Errorf("%q là tên dành riêng của Windows", name)
	}
	all, err := List(draftsDir)
	if err != nil {
		return "", err
	}
	for _, o := range all {
		if !sameFolder(o.Folder, d.Folder) && strings.EqualFold(o.Name, name) {
			return "", fmt.Errorf("đã có project khác tên %q", o.Name)
		}
	}
	target := filepath.Join(filepath.Dir(d.Folder), name)
	if !sameFolder(target, d.Folder) {
		if _, err := os.Stat(target); err == nil {
			return "", fmt.Errorf("đã có thư mục %q (có thể là project không nằm trong danh sách của CapCut)", filepath.Base(target))
		}
	}
	return name, nil
}

// Rename renames a project the way CapCut's own "Đổi tên" does (checked
// against CapCut 6.9 by renaming a project in its UI and diffing the files):
//
//   - the project FOLDER is renamed to the new name
//   - root_meta_info.json gets the new draft_name, draft_fold_path and
//     draft_json_file for that project
//
// Changing only draft_name does not work: when a project is opened CapCut
// takes its name from the folder and writes that name back.
//
// CapCut must be closed, otherwise it writes its own copy of the project
// list over the change (and the folder may be locked). On any failure
// everything already changed is put back.
func Rename(draftsDir string, d Draft, newName string) (err error) {
	rootPath := filepath.Join(draftsDir, rootMetaFile)
	root, err := readJSON(rootPath)
	if err != nil {
		return err
	}
	store, _ := root["all_draft_store"].([]any)
	var target map[string]any
	for _, it := range store {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if (d.ID != "" && str(m, "draft_id") == d.ID) ||
			(d.ID == "" && sameFolder(str(m, "draft_fold_path"), d.Folder)) {
			target = m
			break
		}
	}
	if target == nil {
		return fmt.Errorf("không tìm thấy project %q trong %s", d.Name, rootMetaFile)
	}

	oldFolder := d.Folder
	newFolder := filepath.Join(filepath.Dir(oldFolder), newName)

	// 1. the folder
	if oldFolder != newFolder {
		if err := os.Rename(oldFolder, newFolder); err != nil {
			return fmt.Errorf("không đổi tên được thư mục %q (CapCut hoặc ứng dụng khác đang dùng?): %w", filepath.Base(oldFolder), err)
		}
		defer func() {
			if err != nil {
				_ = os.Rename(newFolder, oldFolder)
			}
		}()
	}

	// 2. paths inside the root entry. They are stored with forward slashes in
	// draft_fold_path and mixed slashes after it, so the stored prefix is
	// replaced as text.
	oldStored := str(target, "draft_fold_path")
	newStored := oldStored
	if i := strings.LastIndexAny(oldStored, `/\`); i >= 0 {
		newStored = oldStored[:i+1] + newName
	}
	rewrite := func(key string) {
		v := str(target, key)
		if v != "" && oldStored != "" && len(v) >= len(oldStored) && strings.EqualFold(v[:len(oldStored)], oldStored) {
			target[key] = newStored + v[len(oldStored):]
		}
	}
	target["draft_name"] = newName
	target["draft_fold_path"] = newStored
	rewrite("draft_json_file")
	rewrite("draft_cover")

	// 3. the project's own meta file (CapCut rewrites it when it opens the
	// project; keeping it consistent means nothing stale is left behind)
	metaPath := filepath.Join(newFolder, draftMetaFile)
	if meta, merr := readJSON(metaPath); merr == nil {
		meta["draft_name"] = newName
		if v := str(meta, "draft_fold_path"); v != "" {
			if i := strings.LastIndexAny(v, `/\`); i >= 0 {
				meta["draft_fold_path"] = v[:i+1] + newName
			}
		}
		if werr := writeJSON(metaPath, meta); werr != nil {
			return fmt.Errorf("ghi %s: %w", draftMetaFile, werr)
		}
	}

	return writeJSON(rootPath, root)
}
