// Package sandbox resolves user-supplied paths against a base directory
// and provides a filesystem rooted at it. By default it is flexible:
// absolute paths, relative paths, and parent traversal ("..") all resolve
// to their target. Pass [Strict] to confine resolution and access to the
// base directory, rejecting anything that escapes it — the containment
// boundary shared by the filesystem tools (read, write, edit, find, grep).
// [AllowDir] opts specific extra directories back in under [Strict].
package sandbox

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Option configures resolution ([Resolve], [ResolveDir]) or a filesystem
// ([New]).
type Option func(*options)

type options struct {
	strict bool
	dirs   []string
}

// Strict confines resolution and filesystem access to the base directory,
// rejecting absolute or "../" paths that escape it.
func Strict() Option {
	return func(o *options) { o.strict = true }
}

// AllowDir opts additional directories into a [Strict] sandbox: paths that
// resolve inside dirs (or their subtrees) are permitted even though they
// lie outside the root. It has no effect without [Strict]. Pass absolute
// directories.
func AllowDir(dirs ...string) Option {
	return func(o *options) { o.dirs = append(o.dirs, dirs...) }
}

func apply(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// FS is a filesystem rooted at a real OS directory. It is the union of
// every filesystem surface the tools need: reading, writing, directory
// creation, and reporting its own root. Unless [Strict] is given, names
// may traverse outside the root via ".."; [AllowDir] opts specific extra
// directories back in under [Strict].
type FS struct {
	root   string
	strict bool
	dirs   []string
}

// New creates an [FS] rooted at root.
func New(root string, opts ...Option) *FS {
	o := apply(opts)
	return &FS{root: root, strict: o.strict, dirs: o.dirs}
}

// osPath maps a name to its on-disk path, reporting whether access is
// permitted under the filesystem's strictness.
func (f *FS) osPath(name string) (string, bool) {
	abs := filepath.Join(f.root, filepath.FromSlash(name))
	return abs, !f.strict || allowed(f.root, f.dirs, abs)
}

// Open implements [fs.FS], opening name relative to the root.
func (f *FS) Open(name string) (fs.File, error) {
	abs, ok := f.osPath(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return os.Open(abs)
}

// WriteFile creates or overwrites name with data.
func (f *FS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	abs, ok := f.osPath(name)
	if !ok {
		return &fs.PathError{Op: "writefile", Path: name, Err: fs.ErrInvalid}
	}
	return os.WriteFile(abs, data, perm)
}

// MkdirAll creates path, and any parents needed, under the root.
func (f *FS) MkdirAll(path string, perm fs.FileMode) error {
	abs, ok := f.osPath(path)
	if !ok {
		return &fs.PathError{Op: "mkdirall", Path: path, Err: fs.ErrInvalid}
	}
	return os.MkdirAll(abs, perm)
}

// Root returns the OS directory the filesystem is rooted at.
func (f *FS) Root() string {
	return f.root
}

// Resolve turns a user-supplied path into a name valid for this
// filesystem, resolving it against the root and applying the filesystem's
// own strictness. It saves callers from tracking the root separately.
func (f *FS) Resolve(path string) (string, error) {
	return resolve(f.root, f.dirs, path, true, f.strict)
}

// ResolveDir is like [FS.Resolve] but maps an empty path to "." (the
// root), for directory or search bases.
func (f *FS) ResolveDir(path string) (string, error) {
	return resolve(f.root, f.dirs, path, false, f.strict)
}

// Resolve turns a user-supplied path — absolute or relative — into a name
// relative to dir, suitable for an [FS] rooted at dir. By default the name
// may traverse outside dir (e.g. "../sibling"); with [Strict] such paths
// are rejected. An empty path is an error.
func Resolve(dir, path string, opts ...Option) (string, error) {
	o := apply(opts)
	return resolve(dir, o.dirs, path, true, o.strict)
}

// ResolveDir is like [Resolve] but maps an empty path to "." (the root),
// for directory or search bases.
func ResolveDir(dir, path string, opts ...Option) (string, error) {
	o := apply(opts)
	return resolve(dir, o.dirs, path, false, o.strict)
}

func resolve(dir string, dirs []string, path string, emptyErr, strict bool) (string, error) {
	if path == "" {
		if emptyErr {
			return "", fmt.Errorf("path is required")
		}
		return ".", nil
	}
	if dir == "" {
		dir = "."
	}

	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(dir, path))
	}

	if strict && !allowed(dir, dirs, abs) {
		return "", fmt.Errorf("path escapes working directory: %s", path)
	}

	rel, err := filepath.Rel(dir, abs)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path %s: %w", path, err)
	}

	return filepath.ToSlash(rel), nil
}

// allowed reports whether abs lies within root or any of the extra
// permitted directories (or their subtrees). Both sides are canonicalized
// through symlinks first, so a link inside the root that points outside it
// cannot be used to escape.
func allowed(root string, dirs []string, abs string) bool {
	real := realPath(abs)
	if within(realPath(root), real) {
		return true
	}
	for _, d := range dirs {
		if within(realPath(filepath.Clean(d)), real) {
			return true
		}
	}
	return false
}

// within reports whether abs is base itself or lies inside its subtree.
func within(base, abs string) bool {
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// realPath resolves symlinks along the longest existing prefix of p,
// leaving any not-yet-existing trailing components appended lexically. It
// lets containment checks follow symlinks without requiring the full path
// to exist — e.g. a file about to be created by write.
func realPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return filepath.Join(realPath(parent), filepath.Base(p))
}
