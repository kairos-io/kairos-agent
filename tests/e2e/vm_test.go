//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/gofrs/uuid"
	process "github.com/mudler/go-processmanager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
	"github.com/spectrocloud/peg/pkg/machine"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

// startVM boots the ISO at $ISO in QEMU (user-mode networking) with a single
// blank 20G drive as the install target, and waits for SSH.
func startVM() VM {
	iso := os.Getenv("ISO")
	Expect(iso).ToNot(BeEmpty(), "ISO env var must point to the test ISO")
	_, err := os.Stat(iso)
	Expect(err).ToNot(HaveOccurred(), "ISO file must exist")

	stateDir, err := os.MkdirTemp("", "kairos-e2e-")
	Expect(err).ToNot(HaveOccurred())
	fmt.Printf("State dir: %s\n", stateDir)

	sshPort, err := freePort()
	Expect(err).ToNot(HaveOccurred())

	uid, _ := uuid.NewV4()

	memory := os.Getenv("MEMORY")
	if memory == "" {
		memory = "4000"
	}
	cpus := os.Getenv("CPUS")
	if cpus == "" {
		cpus = "2"
	}

	opts := []types.MachineOption{
		types.QEMUEngine,
		types.WithISO(iso),
		types.WithMemory(memory),
		types.WithCPU(cpus),
		types.WithSSHPort(strconv.Itoa(sshPort)),
		types.WithID(uid.String()),
		types.WithSSHUser("kairos"),
		types.WithSSHPass("kairos"),
		types.WithStateDir(stateDir),
		types.WithDriveSize("20000"),
		types.OnFailure(func(p *process.Process) {
			out, _ := os.ReadFile(p.StdoutPath())
			errOut, _ := os.ReadFile(p.StderrPath())
			serial, _ := os.ReadFile(path.Join(p.StateDir(), "serial.log"))
			status, _ := p.ExitCode()
			Fail(fmt.Sprintf("VM aborted.\nstdout: %s\nstderr: %s\nserial: %s\nexit: %s\n",
				out, errOut, serial, status))
		}),
		func(m *types.MachineConfig) error {
			m.Args = append(m.Args,
				"-chardev", fmt.Sprintf("stdio,mux=on,id=char0,logfile=%s,signal=off", path.Join(stateDir, "serial.log")),
				"-serial", "chardev:char0",
				"-mon", "chardev=char0",
			)
			return nil
		},
	}

	m, err := machine.New(opts...)
	Expect(err).ToNot(HaveOccurred())

	vm := NewVM(m, stateDir)
	_, err = vm.Start(context.Background())
	Expect(err).ToNot(HaveOccurred())
	return vm
}

// dumpSerial writes the VM serial log into ./logs on failure.
func dumpSerial(vm VM) {
	serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
	_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
	_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
	fmt.Println(string(serial))
}
