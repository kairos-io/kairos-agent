package phonehome_test

import (
	"errors"
	"os"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("phonehome.Uninstall", func() {
	var (
		systemctlCalls [][]string
		removed        []string
		restoreRun     func(string, ...string) ([]byte, error)
		restoreRm      func(string) error
	)

	BeforeEach(func() {
		systemctlCalls = nil
		removed = nil
	})

	AfterEach(func() {
		// Put the production runners back so later specs (and the config
		// suite's "unregister invokes stop" case) get a clean slate.
		phonehome.SetUninstallRunners(restoreRun, restoreRm)
	})

	Context("when every step succeeds", func() {
		BeforeEach(func() {
			restoreRun, restoreRm = phonehome.SetUninstallRunners(
				func(name string, args ...string) ([]byte, error) {
					Expect(name).To(Equal("systemctl"))
					systemctlCalls = append(systemctlCalls, args)
					return nil, nil
				},
				func(path string) error {
					removed = append(removed, path)
					return nil
				},
			)
		})

		It("walks the full teardown in the documented order", func() {
			summary, err := phonehome.Uninstall()
			Expect(err).ToNot(HaveOccurred(), summary)

			// systemctl stop -> disable -> daemon-reload. The unit-file rm
			// lands between `disable` and `daemon-reload`, so the three
			// systemctl calls come out in this exact sequence.
			Expect(systemctlCalls).To(Equal([][]string{
				{"stop", phonehome.ServiceName},
				{"disable", phonehome.ServiceName},
				{"daemon-reload"},
			}))

			// File removals: unit file first, then creds, then the two
			// cloud-config files under /oem.
			Expect(removed).To(Equal([]string{
				phonehome.ServicePath,
				phonehome.DefaultCredentialsPath,
				phonehome.CloudConfigPath,
				phonehome.RemoteCloudConfigPath,
			}))

			// The operator-facing summary names each meaningful path so the
			// remote `unregister` command's result string and the local CLI
			// output are self-describing.
			for _, tok := range []string{
				phonehome.ServiceName,
				phonehome.ServicePath,
				phonehome.DefaultCredentialsPath,
				phonehome.CloudConfigPath,
				phonehome.RemoteCloudConfigPath,
			} {
				Expect(summary).To(ContainSubstring(tok))
			}
		})
	})

	Context("when the node was only partially installed", func() {
		// A node that never ran the installer (or already went through
		// Uninstall once) has no service to stop and no files to remove.
		// Calling Uninstall again must converge silently — the remote
		// decommission flow runs it on every delete and we don't want a
		// second click to error out.
		BeforeEach(func() {
			restoreRun, restoreRm = phonehome.SetUninstallRunners(
				func(_ string, args ...string) ([]byte, error) {
					if len(args) > 0 && (args[0] == "stop" || args[0] == "disable") {
						return []byte("Unit " + phonehome.ServiceName + " not loaded."), errors.New("exit status 5")
					}
					return nil, nil
				},
				func(string) error { return os.ErrNotExist },
			)
		})

		It("returns no error and marks every path as already absent", func() {
			summary, err := phonehome.Uninstall()
			Expect(err).ToNot(HaveOccurred(), summary)

			for _, path := range []string{
				phonehome.ServicePath,
				phonehome.DefaultCredentialsPath,
				phonehome.CloudConfigPath,
				phonehome.RemoteCloudConfigPath,
			} {
				Expect(summary).To(ContainSubstring("already absent: " + path))
			}
		})
	})

	Context("when a real filesystem error happens", func() {
		// Permission-denied (running the CLI without sudo, a read-only /etc,
		// etc.) is the kind of failure the operator must be told about.
		// We mustn't silently swallow it into an "everything's fine" summary.
		BeforeEach(func() {
			restoreRun, restoreRm = phonehome.SetUninstallRunners(
				func(_ string, _ ...string) ([]byte, error) { return nil, nil },
				func(path string) error {
					if path == phonehome.DefaultCredentialsPath {
						return &os.PathError{Op: "remove", Path: path, Err: os.ErrPermission}
					}
					return nil
				},
			)
		})

		It("surfaces the error and names the offending path in the summary", func() {
			summary, err := phonehome.Uninstall()
			Expect(err).To(HaveOccurred())
			Expect(summary).To(ContainSubstring(phonehome.DefaultCredentialsPath))
			Expect(strings.ToLower(err.Error())).To(ContainSubstring("permission"))
		})
	})
})
