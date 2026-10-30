package lxsource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A Store keeps the scripts the listener imported, in a directory of its
// own: the script's code as <id>.js, and what its header says as <id>.json,
// beside it, so the settings list them without running any.
//
// A script is run only when it is asked something: reading the directory
// touches no runtime.
type Store struct {
	dir  string
	http *http.Client
}

// NewStore returns the store in dir, making it if it is not there.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("音源目录: %w", err)
	}
	return &Store{dir: dir, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// ErrNotFound is returned for a script the store does not have.
var ErrNotFound = errors.New("没有这个音源")

// ErrBadURL is returned for a remote address the store will not fetch.
var ErrBadURL = errors.New("音源地址无效：只从 http 或 https 取")

// List returns what the store has, enabled or not, as they were imported.
func (s *Store) List() ([]Info, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			continue
		}
		var info Info
		if json.Unmarshal(b, &info) != nil || info.ID == "" {
			continue
		}
		out = append(out, info)
	}
	// The order is the order they were imported, so that the list does not
	// jump about as scripts are turned on and off.
	sortInfos(out)
	return out, nil
}

// sortInfos orders scripts by when they were imported, earliest first, and
// by name where that is the same.
func sortInfos(list []Info) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0; j-- {
			a, b := list[j-1], list[j]
			if a.Imported.Before(b.Imported) || (a.Imported.Equal(b.Imported) && a.Name <= b.Name) {
				break
			}
			list[j-1], list[j] = b, a
		}
	}
}

// Source returns the code of the script info describes.
func (s *Store) Source(info Info) (string, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, info.ID+".js"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	return string(b), nil
}

// Import adds a script from code, as imported from a file, and returns it
// as the store now has it. A script already there, with the same code, is
// kept rather than added again.
func (s *Store) Import(script string) (Info, error) {
	return s.add(script, "")
}

// Update fetches a script again from the address it was imported from, and
// replaces the entry id has with what the address now serves: its code,
// and what its header now says. The entry keeps its id, whether it is on,
// and where in the listener's order it was, which is what updating means
// rather than importing again.
func (s *Store) Update(id string) (Info, error) {
	old, ok := s.get(id)
	if !ok {
		return Info{}, ErrNotFound
	}
	if old.URL == "" {
		// Imported from a file: there is nothing to fetch it from.
		return Info{}, ErrNoAddress
	}
	script, err := s.fetch(old.URL)
	if err != nil {
		return Info{}, err
	}
	info, err := ParseScript(script)
	if err != nil {
		return Info{}, err
	}
	info.ID = old.ID
	info.URL = old.URL
	info.Imported = old.Imported
	info.Enabled = old.Enabled
	if err := s.write(info, script); err != nil {
		return Info{}, err
	}
	return info, nil
}

// ErrNoAddress is returned for a script imported from a file, which has no
// address to fetch it from again.
var ErrNoAddress = errors.New("这个音源是从本地文件导入的：请重新选择文件")

// fetch returns the script at addr. Only http and https are fetched: a
// script is code, and the store runs what it is given.
func (s *Store) fetch(addr string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(addr))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", ErrBadURL
	}
	res, err := s.http.Get(addr)
	if err != nil {
		return "", fmt.Errorf("取音源: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("取音源: %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxScript+1))
	if err != nil {
		return "", fmt.Errorf("取音源: %w", err)
	}
	if len(b) > maxScript {
		return "", ErrTooLarge
	}
	return string(b), nil
}

// ImportURL fetches script at url, and adds it. Only http and https are
// fetched: a script is code, and the store runs what it is given.
func (s *Store) ImportURL(addr string) (Info, error) {
	script, err := s.fetch(addr)
	if err != nil {
		return Info{}, err
	}
	return s.add(script, addr)
}

// add reads a script, and keeps it if it is one, under an ID made from its
// code: importing the same script twice keeps one.
func (s *Store) add(script, from string) (Info, error) {
	info, err := ParseScript(script)
	if err != nil {
		return Info{}, err
	}
	info.ID = scriptID(script)
	info.URL = from
	// A script already there keeps when it was imported, whether it was
	// turned on, and what it played: its id is made from its code, so the
	// same id is the same code, and there is nothing new to ask of it.
	if old, ok := s.get(info.ID); ok {
		info.Imported, info.Enabled = old.Imported, old.Enabled
		info.Platforms = old.Platforms
	}
	if info.Imported.IsZero() {
		info.Imported = time.Now().UTC()
		info.Enabled = true
	}
	if err := s.write(info, script); err != nil {
		return Info{}, err
	}
	return info, nil
}

// scriptID names a script by its code, so that the same script fetched
// again is the same entry.
func scriptID(script string) string {
	sum := sha256.Sum256([]byte(script))
	return hex.EncodeToString(sum[:8])
}

// get returns the entry of id, if the store has one.
func (s *Store) get(id string) (Info, bool) {
	b, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return Info{}, false
	}
	var info Info
	if json.Unmarshal(b, &info) != nil || info.ID == "" {
		return Info{}, false
	}
	return info, true
}

// SetEnabled turns a script on or off: the app asks only the scripts on
// for streams.
func (s *Store) SetEnabled(id string, on bool) (Info, error) {
	info, ok := s.get(id)
	if !ok {
		return Info{}, ErrNotFound
	}
	if info.Enabled == on {
		return info, nil
	}
	info.Enabled = on
	script, err := s.Source(info)
	if err != nil {
		return Info{}, err
	}
	if err := s.write(info, script); err != nil {
		return Info{}, err
	}
	return info, nil
}

// SetPlatforms keeps the services the script id was found to play, so that
// the settings tag it without starting its runtime to ask again.
func (s *Store) SetPlatforms(id string, platforms []Source) error {
	info, ok := s.get(id)
	if !ok {
		return ErrNotFound
	}
	if samePlatforms(info.Platforms, platforms) {
		return nil
	}
	info.Platforms = platforms
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(s.dir, id+".json"), b)
}

// samePlatforms reports whether two lists name the same services in the
// same order.
func samePlatforms(a, b []Source) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Remove deletes a script, and what was written of it.
func (s *Store) Remove(id string) error {
	if _, ok := s.get(id); !ok {
		return ErrNotFound
	}
	// The metadata first: a listing that fails to read the code still
	// leaves the script out.
	if err := os.Remove(filepath.Join(s.dir, id+".json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(filepath.Join(s.dir, id+".js")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// write keeps the script and its entry, the entry last so that a listing
// never finds an entry without code.
func (s *Store) write(info Info, script string) error {
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(s.dir, info.ID+".js"), []byte(script)); err != nil {
		return err
	}
	return writeFile(filepath.Join(s.dir, info.ID+".json"), b)
}

// writeFile replaces path with data, through a temporary file beside it, so
// that an interrupted write leaves the old one whole.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
