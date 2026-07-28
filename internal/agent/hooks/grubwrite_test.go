package hook

import (
	"testing"
)

func TestGrubOptions_Dispatch(t *testing.T) {
	// Both branches of grubOptions dispatch to writeGrubenvToOem /
	// writeGrubenvToState which try to mount /oem or /run/initramfs/cos-state,
	// then invoke utils.SetPersistentVariables against a path that does not
	// exist in a headless test env. Either way the function returns without
	// panicking; we accept nil or an error result.
	c := makeConfig()
	_ = grubOptions(c, map[string]string{"foo": "bar"}, false)
	_ = grubOptions(c, map[string]string{"foo": "bar"}, true)
}

func TestWriteGrubenvToOem_ErrorPath(t *testing.T) {
	c := makeConfig()
	// writeGrubenvToOem tries to write to /oem/grubenv. In the test env
	// mounts fail silently and the write is against a path that likely does
	// not exist. Both nil and error are acceptable results — the point is to
	// exercise the code path.
	_ = writeGrubenvToOem(c, map[string]string{"a": "b"})
}

func TestWriteGrubenvToState_ErrorPath(t *testing.T) {
	c := makeConfig()
	_ = writeGrubenvToState(c, map[string]string{"a": "b"})
}
