package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type embyFolderManifest struct {
	Version  int           `json:"version"`
	DramaID  string        `json:"dramaId"`
	Chapters []embyChapter `json:"chapters"`
}

func validEmbyFolder(folder string) bool {
	return folder == "" || len(folder) <= 240 && folder != "." && folder != ".." && !strings.ContainsAny(folder, "/\\\x00\r\n") && filepath.Base(folder) == folder
}

func ensureEmbyDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(path, 0755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Emby 生成目录不是普通文件夹，已停止写入")
	}
	return nil
}

func syncEmbyDramaFiles(ctx context.Context, settings embySyncSettings, drama Drama, chapters []Chapter, folder string, key []byte, owner string) (string, int, int, error) {
	if !validEmbyFolder(folder) {
		return folder, 0, 0, errors.New("Emby 剧集目录无效")
	}
	if folder == "" {
		folder = embyFolderName(drama)
	}
	if err := os.MkdirAll(settings.OutputDir, 0755); err != nil {
		return folder, 0, 0, err
	}
	root, err := filepath.EvalSymlinks(settings.OutputDir)
	if err != nil {
		return folder, 0, 0, err
	}
	target := filepath.Join(root, folder)
	work := target
	var previous []embyChapter
	staged := false
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		work, err = os.MkdirTemp(root, ".juku-emby-")
		if err != nil {
			return folder, 0, 0, err
		}
		staged = true
		defer os.RemoveAll(work)
		if err = os.Chmod(work, 0755); err != nil {
			return folder, 0, 0, err
		}
	} else {
		if err != nil {
			return folder, 0, 0, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return folder, 0, 0, errors.New("Emby 剧集目录不是普通文件夹")
		}
		manifestPath := filepath.Join(work, ".juku-emby.json")
		info, err := os.Lstat(manifestPath)
		if err != nil || !info.Mode().IsRegular() {
			return folder, 0, 0, errors.New("同名目录不属于自动同步，原内容已保留；请使用单独的 Emby 输出目录")
		}
		var manifest embyFolderManifest
		if err = readEmbyJSON(manifestPath, &manifest, 8<<20); err != nil {
			return folder, 0, 0, err
		}
		if manifest.Version != 1 || manifest.DramaID != drama.ID {
			return folder, 0, 0, errors.New("Emby 目录所属剧集不匹配，原内容已保留")
		}
		previous = manifest.Chapters
	}
	records, err := embyChapterRecords(drama.ID, chapters, previous)
	if err != nil {
		return folder, 0, 0, err
	}
	if err = ensureEmbyDirectory(filepath.Join(work, "Season 01")); err != nil {
		return folder, 0, 0, err
	}
	written := 0
	err = writeEmbyFiles(drama, records, settings.BaseURL, key, owner, func(name string, body []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		changed, err := writeEmbyFile(filepath.Join(work, filepath.FromSlash(name)), body, 0644)
		if changed {
			written++
		}
		return err
	})
	if err == nil {
		body, marshalErr := json.MarshalIndent(embyFolderManifest{Version: 1, DramaID: drama.ID, Chapters: records}, "", "  ")
		err = marshalErr
		if err == nil {
			_, err = writeEmbyFile(filepath.Join(work, ".juku-emby.json"), append(body, '\n'), 0600)
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if staged {
		if err == nil {
			err = os.Rename(work, target)
		}
		if err != nil {
			written = 0
		}
	}
	return folder, written, len(records), err
}

func writeEmbyFile(path string, body []byte, mode os.FileMode) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return false, errors.New("Emby 目标文件不是普通文件或文件过大，已停止写入")
		}
		old, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		if bytes.Equal(old, body) {
			return false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".juku-emby-write-")
	if err != nil {
		return false, err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(body)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return false, fmt.Errorf("Emby 文件替换失败：%w", err)
	}
	return true, nil
}
