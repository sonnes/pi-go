package sandbox

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFS_OpenAndWrite(t *testing.T) {
	dir := t.TempDir()
	sb := New(dir)

	require.NoError(t, sb.WriteFile("a.txt", []byte("hello"), 0o644))

	file, err := sb.Open("a.txt")
	require.NoError(t, err)
	defer file.Close() //nolint:errcheck

	got := make([]byte, 5)
	_, err = file.Read(got)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(got))
}

func TestFS_MkdirAllThenWrite(t *testing.T) {
	dir := t.TempDir()
	sb := New(dir)

	require.NoError(t, sb.MkdirAll("nested/dir", 0o755))
	require.NoError(t, sb.WriteFile("nested/dir/b.txt", []byte("x"), 0o644))

	_, err := os.Stat(filepath.Join(dir, "nested", "dir", "b.txt"))
	require.NoError(t, err)
}

func TestFS_Root(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, dir, New(dir).Root())
}

// By default the FS is flexible: a "../" name resolves outside the root.
func TestFS_Flexible_TraversesOutsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	require.NoError(t, os.Mkdir(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "outside.txt"), []byte("secret"), 0o644))

	sb := New(root)

	file, err := sb.Open("../outside.txt")
	require.NoError(t, err)
	defer file.Close() //nolint:errcheck

	got := make([]byte, 6)
	_, err = file.Read(got)
	require.NoError(t, err)
	assert.Equal(t, "secret", string(got))
}

// Strict mode confines access: "../" names are rejected.
func TestFS_Strict_RejectsEscape(t *testing.T) {
	sb := New(t.TempDir(), Strict())

	_, openErr := sb.Open("../escape")
	assert.ErrorIs(t, openErr, fs.ErrInvalid)

	writeErr := sb.WriteFile("../escape", []byte("x"), 0o644)
	assert.ErrorIs(t, writeErr, fs.ErrInvalid)

	mkErr := sb.MkdirAll("../escape", 0o755)
	assert.ErrorIs(t, mkErr, fs.ErrInvalid)
}

func TestResolve_Flexible(t *testing.T) {
	dir := "/work/project"

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "relative", path: "pkg/main.go", want: "pkg/main.go"},
		{name: "absolute inside", path: "/work/project/pkg/main.go", want: "pkg/main.go"},
		{name: "dot", path: ".", want: "."},
		{name: "parent traversal", path: "../secret", want: "../secret"},
		{name: "absolute outside", path: "/etc/passwd", want: "../../etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(dir, tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolve_EmptyIsError(t *testing.T) {
	_, err := Resolve("/work/project", "")
	assert.Error(t, err)
}

func TestResolve_Strict_RejectsEscape(t *testing.T) {
	dir := "/work/project"

	for _, path := range []string{"../secret", "/etc/passwd"} {
		t.Run(path, func(t *testing.T) {
			_, err := Resolve(dir, path, Strict())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "escapes working directory")
		})
	}
}

func TestResolve_Strict_AllowsInside(t *testing.T) {
	got, err := Resolve("/work/project", "/work/project/pkg/main.go", Strict())
	require.NoError(t, err)
	assert.Equal(t, "pkg/main.go", got)
}

func TestResolveDir_EmptyIsRoot(t *testing.T) {
	got, err := ResolveDir("/work/project", "")
	require.NoError(t, err)
	assert.Equal(t, ".", got)
}

func TestResolveDir_Flexible_AllowsEscape(t *testing.T) {
	got, err := ResolveDir("/work/project", "../sibling")
	require.NoError(t, err)
	assert.Equal(t, "../sibling", got)
}

func TestResolveDir_Strict_RejectsEscape(t *testing.T) {
	_, err := ResolveDir("/work/project", "../secret", Strict())
	assert.Error(t, err)
}

func TestResolve_Strict_AllowDir(t *testing.T) {
	root := "/work/project"
	extra := "/opt/shared"

	// A path inside the allowed dir resolves (as a name relative to root).
	got, err := Resolve(root, "/opt/shared/lib.go", Strict(), AllowDir(extra))
	require.NoError(t, err)
	assert.Equal(t, "../../opt/shared/lib.go", got)

	// A path outside both root and the allowed dir is still rejected.
	_, err = Resolve(root, "/etc/passwd", Strict(), AllowDir(extra))
	assert.Error(t, err)
}

func TestFS_Strict_AllowDir_ReadsExtraDir(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(extra, "shared.txt"), []byte("data"), 0o644))

	sb := New(root, Strict(), AllowDir(extra))

	name, err := sb.Resolve(filepath.Join(extra, "shared.txt"))
	require.NoError(t, err)

	file, err := sb.Open(name)
	require.NoError(t, err)
	defer file.Close() //nolint:errcheck

	got := make([]byte, 4)
	_, err = file.Read(got)
	require.NoError(t, err)
	assert.Equal(t, "data", string(got))
}

func TestFS_Strict_RejectsSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	require.NoError(t, os.Mkdir(root, 0o755))
	outside := filepath.Join(parent, "outside")
	require.NoError(t, os.Mkdir(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644))

	// A symlink inside root that points outside it must not grant access.
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))

	sb := New(root, Strict())

	_, err := sb.Open("link/secret.txt")
	assert.ErrorIs(t, err, fs.ErrInvalid)
}

func TestFS_Strict_AllowsSymlinkInsideRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "real"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "real", "f.txt"), []byte("ok"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")))

	sb := New(root, Strict())

	file, err := sb.Open("link/f.txt")
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func TestFS_Strict_AllowDir_StillRejectsOthers(t *testing.T) {
	sb := New(t.TempDir(), Strict(), AllowDir(t.TempDir()))

	_, err := sb.Resolve("/etc/passwd")
	assert.Error(t, err)

	_, err = sb.Open("../../../etc/passwd")
	assert.ErrorIs(t, err, fs.ErrInvalid)
}
