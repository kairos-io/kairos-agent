package action

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("extensionFileNameFromURI", func() {
	DescribeTable("derives the on-disk filename from the URI path",
		func(uri, expected string) {
			Expect(extensionFileNameFromURI(uri)).To(Equal(expected))
		},
		Entry("plain http URL", "http://host/path/foo.sysext.raw", "foo.sysext.raw"),
		// The query must not be baked into the name, or `.raw` detection breaks.
		Entry("strips a token query string", "http://host/path/foo.sysext.raw?token=abc123", "foo.sysext.raw"),
		Entry("nested https path with multiple query params", "https://h/a/b/c/bar.confext.raw?x=1&y=2", "bar.confext.raw"),
		Entry("bare filename", "foo.sysext.raw", "foo.sysext.raw"),
		Entry("absolute local path", "/var/lib/x/foo.sysext.raw", "foo.sysext.raw"),
	)
})
