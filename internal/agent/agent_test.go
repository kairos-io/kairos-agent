package agent_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "github.com/kairos-io/kairos-agent/v2/internal/agent"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testProvider = `#!/bin/bash
event="$1"
payload=$(</dev/stdin)
echo "Received $event with $payload" >> exec.log
echo "{}"
`

// Serial: Run() calls bus.Reload(), which scans the package cwd for
// agent-provider-* binaries. This spec drops such a binary into the cwd and
// deletes it afterwards; if it ran alongside other parallel procs, their own
// bus.Manager.Initialize()/Reload() calls could register the binary and then
// os.Exit(1) (bus.go's errored-response handler) once the file disappears
// mid-publish. Serial specs run exclusively, so the binary never leaks.
var _ = Describe("Bootstrap provider", Serial, func() {
	Context("Config", func() {
		It("gets entire content", func() {
			f := GinkgoT().TempDir()

			wd, _ := os.Getwd()
			err := os.WriteFile(filepath.Join(wd, "agent-provider-test"), []byte(testProvider), 0777)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() { os.Remove(filepath.Join(wd, "agent-provider-test")) })

			// The provider script appends to exec.log relative to the test
			// process cwd; start clean and don't leave it behind.
			os.Remove(filepath.Join(wd, "exec.log"))
			DeferCleanup(func() { os.Remove(filepath.Join(wd, "exec.log")) })

			// Run() reloads the bus and registers the provider's handlers on
			// the shared Manager (including the os.Exit(1) error handler);
			// leave a fresh bus behind so they don't leak into later specs.
			DeferCleanup(func() { bus.Manager = bus.NewBus() })

			err = os.WriteFile(filepath.Join(f, "test.config.yaml"), []byte(`#cloud-config
doo: bar`), 0655)
			Expect(err).ToNot(HaveOccurred())

			err = Run(WithDirectory(f))

			Expect(err).ToNot(HaveOccurred())

			dat, err := os.ReadFile(filepath.Join(wd, "exec.log"))
			Expect(err).ToNot(HaveOccurred())

			fmt.Println(string(dat))
			Expect(string(dat)).To(ContainSubstring("Received"), string(dat))
			Expect(string(dat)).To(ContainSubstring("doo: bar"), string(dat))
		})
	})
})
