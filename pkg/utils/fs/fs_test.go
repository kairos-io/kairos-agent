package fsutils_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	sdkFS "github.com/kairos-io/kairos-sdk/types/fs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("FsUtils", func() {
	var tfs sdkFS.KairosFS
	var cleanup func()
	var err error

	BeforeEach(func() {
		tfs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(cleanup)
	})

	Describe("Exists", func() {
		It("returns true for an existing file", func() {
			Expect(tfs.WriteFile("/file.txt", []byte("hello"), 0644)).To(Succeed())
			exists, err := fsutils.Exists(tfs, "/file.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})
		It("returns false for a missing file", func() {
			exists, err := fsutils.Exists(tfs, "/nope.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
		})
	})

	Describe("IsDir", func() {
		It("returns true for a directory", func() {
			Expect(fsutils.MkdirAll(tfs, "/somedir", 0755)).To(Succeed())
			isDir, err := fsutils.IsDir(tfs, "/somedir")
			Expect(err).ToNot(HaveOccurred())
			Expect(isDir).To(BeTrue())
		})
		It("returns false for a file", func() {
			Expect(tfs.WriteFile("/file.txt", []byte("hi"), 0644)).To(Succeed())
			isDir, err := fsutils.IsDir(tfs, "/file.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(isDir).To(BeFalse())
		})
		It("returns an error for a missing path", func() {
			_, err := fsutils.IsDir(tfs, "/missing")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("MkdirAll", func() {
		It("creates nested directories", func() {
			Expect(fsutils.MkdirAll(tfs, "/a/b/c", 0755)).To(Succeed())
			exists, err := fsutils.Exists(tfs, "/a/b/c")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
			isDir, err := fsutils.IsDir(tfs, "/a/b/c")
			Expect(err).ToNot(HaveOccurred())
			Expect(isDir).To(BeTrue())
		})
	})

	Describe("DirSize", func() {
		It("sums the size of all files in the tree", func() {
			Expect(fsutils.MkdirAll(tfs, "/data/sub", 0755)).To(Succeed())
			Expect(tfs.WriteFile("/data/a.txt", []byte("12345"), 0644)).To(Succeed())          // 5 bytes
			Expect(tfs.WriteFile("/data/sub/b.txt", []byte("1234567890"), 0644)).To(Succeed()) // 10 bytes
			size, err := fsutils.DirSize(tfs, "/data")
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(15)))
		})
		It("returns an error for a missing path", func() {
			_, err := fsutils.DirSize(tfs, "/missing")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("TempDir", func() {
		It("creates a predictable temp dir on the test fs", func() {
			name, err := fsutils.TempDir(tfs, "/tmp", "myprefix")
			Expect(err).ToNot(HaveOccurred())
			Expect(name).To(ContainSubstring("myprefix"))
			exists, err := fsutils.Exists(tfs, name)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
			isDir, err := fsutils.IsDir(tfs, name)
			Expect(err).ToNot(HaveOccurred())
			Expect(isDir).To(BeTrue())
		})
	})

	Describe("TempFile", func() {
		// Note: on the vfst test fs, the returned *os.File is backed by a real
		// host file, so f.Name() is the host raw path (not the vfs-relative
		// path). We therefore assert on the basename pattern and verify the
		// backing file exists on the host fs via os.Stat.
		It("creates a temp file matching the pattern", func() {
			Expect(fsutils.MkdirAll(tfs, "/tmp", 0755)).To(Succeed())
			f, err := fsutils.TempFile(tfs, "/tmp", "pre-*.log")
			Expect(err).ToNot(HaveOccurred())
			Expect(f).ToNot(BeNil())
			name := f.Name()
			Expect(f.Close()).To(Succeed())
			Expect(filepath.Base(name)).To(HavePrefix("pre-"))
			Expect(name).To(HaveSuffix(".log"))
			info, err := os.Stat(name)
			Expect(err).ToNot(HaveOccurred())
			Expect(info.IsDir()).To(BeFalse())
		})
	})

	Describe("Copy", func() {
		It("copies the content of src to dst", func() {
			content := []byte("the quick brown fox")
			Expect(tfs.WriteFile("/src.txt", content, 0644)).To(Succeed())
			Expect(fsutils.Copy(tfs, "/src.txt", "/dst.txt")).To(Succeed())
			got, err := tfs.ReadFile("/dst.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(content))
		})
		It("returns ErrInvalid when src equals dst", func() {
			err := fsutils.Copy(tfs, "/same.txt", "/same.txt")
			Expect(err).To(MatchError(os.ErrInvalid))
		})
		It("errors when src does not exist", func() {
			err := fsutils.Copy(tfs, "/missing.txt", "/dst.txt")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GlobFs", func() {
		It("returns only the matching files in the directory", func() {
			Expect(fsutils.MkdirAll(tfs, "/glob/sub", 0755)).To(Succeed())
			Expect(tfs.WriteFile("/glob/a.txt", []byte("a"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/glob/b.txt", []byte("b"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/glob/c.log", []byte("c"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/glob/sub/d.txt", []byte("d"), 0644)).To(Succeed())

			matches, err := fsutils.GlobFs(tfs, "/glob/*.txt")
			Expect(err).ToNot(HaveOccurred())
			sort.Strings(matches)
			Expect(matches).To(Equal([]string{"/glob/a.txt", "/glob/b.txt"}))
		})
	})

	Describe("WalkDirFs", func() {
		It("visits every entry in the tree", func() {
			Expect(fsutils.MkdirAll(tfs, "/walk/sub", 0755)).To(Succeed())
			Expect(tfs.WriteFile("/walk/a.txt", []byte("a"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/walk/sub/b.txt", []byte("b"), 0644)).To(Succeed())

			var visited []string
			err := fsutils.WalkDirFs(tfs, "/walk", func(path string, _ fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				visited = append(visited, path)
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(visited).To(ContainElements("/walk", "/walk/a.txt", "/walk/sub", "/walk/sub/b.txt"))
		})
		It("honors SkipDir to prune subtrees", func() {
			Expect(fsutils.MkdirAll(tfs, "/walk2/skip", 0755)).To(Succeed())
			Expect(tfs.WriteFile("/walk2/keep.txt", []byte("k"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/walk2/skip/hidden.txt", []byte("h"), 0644)).To(Succeed())

			var visited []string
			err := fsutils.WalkDirFs(tfs, "/walk2", func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && strings.HasSuffix(path, "/skip") {
					return filepath.SkipDir
				}
				visited = append(visited, path)
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(visited).To(ContainElement("/walk2/keep.txt"))
			Expect(visited).ToNot(ContainElement("/walk2/skip/hidden.txt"))
		})
	})
})
