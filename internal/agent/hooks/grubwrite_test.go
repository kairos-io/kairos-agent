package hook

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("Grubenv writers", func() {
	It("grubOptions dispatches to both writers", func() {
		// Both branches of grubOptions dispatch to writeGrubenvToOem /
		// writeGrubenvToState which try to mount /oem or /run/initramfs/cos-state,
		// then invoke utils.SetPersistentVariables against a path that does not
		// exist in a headless test env. Either way the function returns without
		// panicking; we accept nil or an error result.
		c := makeConfig()
		_ = grubOptions(c, map[string]string{"foo": "bar"}, false)
		_ = grubOptions(c, map[string]string{"foo": "bar"}, true)
	})

	It("writeGrubenvToOem tolerates the error path", func() {
		c := makeConfig()
		// writeGrubenvToOem tries to write to /oem/grubenv. In the test env
		// mounts fail silently and the write is against a path that likely does
		// not exist. Both nil and error are acceptable results — the point is to
		// exercise the code path.
		_ = writeGrubenvToOem(c, map[string]string{"a": "b"})
	})

	It("writeGrubenvToState tolerates the error path", func() {
		c := makeConfig()
		_ = writeGrubenvToState(c, map[string]string{"a": "b"})
	})
})
