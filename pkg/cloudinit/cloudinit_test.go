/*
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cloudinit_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	. "github.com/kairos-io/kairos-agent/v2/pkg/cloudinit"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// Parted print sample output
const printOutput = `BYT;
/dev/loop0:50593792s:loopback:512:512:msdos:Loopback device:;
1:2048s:98303s:96256s:ext4::type=83;
2:98304s:29394943s:29296640s:ext4::boot, type=83;
3:29394944s:45019135s:15624192s:ext4::type=83;`

var _ = Describe("CloudRunner", Label("CloudRunner", "types", "cloud-init"), func() {
	// unit test stolen from yip
	Describe("loading yaml files", func() {
		logger := sdkTypes.NewNullLogger()

		It("executes commands", func() {

			fs2, cleanup2, err := vfst.NewTestFS(map[string]interface{}{})
			Expect(err).Should(BeNil())
			temp := fs2.TempDir()

			defer cleanup2()

			fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{
				"/some/yip/01_first.yaml": `
stages:
  test:
  - commands:
    - sed -i 's/boo/bar/g' ` + temp + `/tmp/test/bar
`,
				"/some/yip/02_second.yaml": `
stages:
  test:
  - commands:
    - sed -i 's/bar/baz/g' ` + temp + `/tmp/test/bar
`,
			})
			Expect(err).Should(BeNil())
			defer cleanup()

			err = fs2.Mkdir("/tmp", os.ModePerm)
			Expect(err).Should(BeNil())
			err = fs2.Mkdir("/tmp/test", os.ModePerm)
			Expect(err).Should(BeNil())

			err = fs2.WriteFile("/tmp/test/bar", []byte(`boo`), os.ModePerm)
			Expect(err).Should(BeNil())

			runner := NewYipCloudInitRunner(logger, &v1.RealRunner{}, fs)

			err = runner.Run("test", "/some/yip")
			Expect(err).Should(BeNil())
			file, err := os.Open(temp + "/tmp/test/bar")
			Expect(err).ShouldNot(HaveOccurred())

			b, err := ioutil.ReadAll(file)
			if err != nil {
				log.Fatal(err)
			}

			Expect(string(b)).Should(Equal("baz"))
		})
	})
	Describe("layout plugin execution", func() {
		var runner *v1mock.FakeRunner
		var afs *vfst.TestFS
		var device, cmdFail string
		var partNum int
		var cleanup func()
		var logs *bytes.Buffer
		var logger sdkTypes.KairosLogger
		BeforeEach(func() {
			logs = &bytes.Buffer{}
			logger = sdkTypes.NewBufferLogger(logs)

			afs, cleanup, _ = vfst.NewTestFS(nil)
			err := fsutils.MkdirAll(afs, "/some/yip", constants.DirPerm)
			Expect(err).To(BeNil())
			_ = fsutils.MkdirAll(afs, "/dev", constants.DirPerm)
			device = "/dev/device"
			_, err = afs.Create(device)
			Expect(err).To(BeNil())

			runner = v1mock.NewFakeRunner()

			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == cmdFail {
					return []byte{}, errors.New("command error")
				}
				switch cmd {
				case "parted":
					return []byte(printOutput), nil
				default:
					return []byte{}, nil
				}
			}
		})
		AfterEach(func() {
			cleanup()
		})
		It("Does nothing if no changes are defined", func() {
			err := afs.WriteFile("/some/yip/layout.yaml", []byte(fmt.Sprintf(`
stages:
  test:
  - name: Nothing to do
    layout:
  - name: Empty device, does nothing
    layout:
      device:
        label: ""
        path: ""
  - name: Defined device without partitions, does nothing
    layout:
      device:
        path: %s
  - name: Defined already existing partition, does nothing
    layout:
      device:
        label: DEV_LABEL
      add_partitions:
      - fsLabel: DEV_LABEL
        pLabel: partLabel
`, device)), constants.FilePerm)
			Expect(err).To(BeNil())
			ghwTest := ghwMock.GhwMock{}
			disk := sdkTypes.Disk{Name: "device", Partitions: []*sdkTypes.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "DEV_LABEL",
				},
			}}
			ghwTest.AddDisk(disk)
			ghwTest.CreateDevices()
			defer ghwTest.Clean()
			cloudRunner := NewYipCloudInitRunner(logger, runner, afs)
			Expect(cloudRunner.Run("test", "/some/yip")).To(BeNil())
		})
		It("Expands last partition on a MSDOS disk", func() {
			partNum = 3
			_, err := afs.Create(fmt.Sprintf("%s%d", device, partNum))
			Expect(err).To(BeNil())
			err = afs.WriteFile("/some/yip/layout.yaml", []byte(fmt.Sprintf(`
stages:
  test:
  - name: Expanding last partition
    layout:
      device:
        path: %s
      expand_partition:
        size: 0
`, device)), constants.FilePerm)
			Expect(err).To(BeNil())
			ghwTest := ghwMock.GhwMock{}
			disk := sdkTypes.Disk{Name: "device", Partitions: []*sdkTypes.Partition{
				{
					Name: fmt.Sprintf("device%d", partNum),
					FS:   "ext4",
				},
			}}
			ghwTest.AddDisk(disk)
			ghwTest.CreateDevices()
			defer ghwTest.Clean()
			cloudRunner := NewYipCloudInitRunner(logger, runner, afs)
			Expect(cloudRunner.Run("test", "/some/yip")).To(BeNil())
		})
		It("Adds a partition on a MSDOS disk", func() {
			partNum = 4
			_, err := afs.Create(fmt.Sprintf("%s%d", device, partNum))
			Expect(err).To(BeNil())
			err = afs.WriteFile("/some/yip/layout.yaml", []byte(fmt.Sprintf(`
stages:
  test:
  - name: Adding new partition
    layout:
      device:
        path: %s
      add_partitions: 
      - fsLabel: SOMELABEL
        pLabel: somelabel
`, device)), constants.FilePerm)
			Expect(err).To(BeNil())
			cloudRunner := NewYipCloudInitRunner(logger, runner, afs)
			Expect(cloudRunner.Run("test", "/some/yip")).To(BeNil())
		})
		It("Fails to add a partition on a MSDOS disk", func() {
			cmdFail = "mkfs.ext4"
			partNum = 4
			_, err := afs.Create(fmt.Sprintf("%s%d", device, partNum))
			Expect(err).To(BeNil())
			err = afs.WriteFile("/some/yip/layout.yaml", []byte(fmt.Sprintf(`
stages:
  test:
  - name: Adding new partition
    layout:
      device:
        path: %s
      add_partitions: 
      - fsLabel: SOMELABEL
        pLabel: somelabel
`, device)), constants.FilePerm)
			Expect(err).To(BeNil())
			cloudRunner := NewYipCloudInitRunner(logger, runner, afs)
			err = cloudRunner.Run("test", "/some/yip")
			Expect(err).ToNot(HaveOccurred())
			Expect(logs.String()).To(MatchRegexp("Could not verify /dev/device is a block device"))
		})
		It("Fails to expand last partition", func() {
			partNum = 3
			cmdFail = "resize2fs"
			_, err := afs.Create(fmt.Sprintf("%s%d", device, partNum))
			Expect(err).To(BeNil())
			err = afs.WriteFile("/some/yip/layout.yaml", []byte(fmt.Sprintf(`
stages:
  test:
  - name: Expanding last partition
    layout:
      device:
        path: %s
      expand_partition:
        size: 0
`, device)), constants.FilePerm)
			Expect(err).To(BeNil())
			cloudRunner := NewYipCloudInitRunner(logger, runner, afs)
			err = cloudRunner.Run("test", "/some/yip")
			Expect(err).ToNot(HaveOccurred())
			// TODO: Is this the error we should be expecting?
			Expect(logs.String()).To(MatchRegexp("Could not verify /dev/device is a block device"))
		})
		It("Fails to find device by path", func() {
			err := afs.WriteFile("/some/yip/layout.yaml", []byte(`
stages:
  test:
  - name: Missing device path
    layout:
      device:
        path: /whatever
`), constants.FilePerm)
			Expect(err).To(BeNil())
			cloudRunner := NewYipCloudInitRunner(logger, runner, afs)
			err = cloudRunner.Run("test", "/some/yip")
			Expect(err).ToNot(HaveOccurred())
			Expect(logs.String()).To(MatchRegexp("Could not verify /whatever is a block device"))
		})
		It("Fails to find device by label", func() {
			err := afs.WriteFile("/some/yip/layout.yaml", []byte(`
stages:
  test:
  - name: Missing device label
    layout:
      device:
        label: IM_NOT_THERE
`), constants.FilePerm)
			Expect(err).To(BeNil())
			cloudRunner := NewYipCloudInitRunner(logger, runner, afs)
			err = cloudRunner.Run("test", "/some/yip")
			Expect(err).ToNot(HaveOccurred())
			Expect(logs.String()).To(MatchRegexp("Could not find device for the given label"))
		})
	})
})
