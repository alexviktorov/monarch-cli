package monarch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monarch", "session.json")
	store := NewFileStore(path)

	in := &Session{Token: "tok", Email: "a@b.c", UserID: "u1", DeviceUUID: "dev-1"}
	if err := store.Save(in); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("session file perm = %04o, want 0600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("created session dir perm = %04o, want 0700", perm)
	}

	out, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if *out != *in {
		t.Errorf("Load = %+v, want %+v", out, in)
	}
}

func TestFileStoreLoadMissing(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "nope.json"))
	if _, err := store.Load(); !errors.Is(err, ErrNoSession) {
		t.Errorf("err = %v, want ErrNoSession", err)
	}
}

func TestFileStoreLoadCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path).Load(); err == nil {
		t.Error("expected error for corrupt session file")
	}
}

func TestFileStoreLoadEmptyToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, []byte(`{"token":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path).Load(); !errors.Is(err, ErrNoSession) {
		t.Error("token-less session should read as ErrNoSession")
	}
}

func TestFileStoreRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(link)
	if err := store.Save(&Session{Token: "t"}); err == nil {
		t.Error("Save through a symlink must be refused")
	}
	if _, err := store.Load(); err == nil {
		t.Error("Load through a symlink must be refused")
	}
}

func TestFileStoreRefusesWritableParentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "loose")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil { // sidestep umask
		t.Fatal(err)
	}
	store := NewFileStore(filepath.Join(dir, "session.json"))
	if err := store.Save(&Session{Token: "t"}); err == nil {
		t.Error("Save into a group/world-writable dir must be refused")
	}
}

func TestFileStoreDeleteIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	store := NewFileStore(path)
	if err := store.Save(&Session{Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(); err != nil {
		t.Errorf("second Delete = %v, want nil", err)
	}
}
