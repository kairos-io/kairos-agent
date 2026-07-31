package common

import (
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("version", func() {
	Describe("GetVersion", func() {
		It("returns the non-empty VERSION constant", func() {
			Expect(GetVersion()).ToNot(BeEmpty())
			Expect(GetVersion()).To(Equal(VERSION))
		})
	})

	Describe("Get", func() {
		It("returns populated version info", func() {
			info := Get()
			Expect(info.Version).To(Equal(VERSION))
			Expect(info.GitCommit).ToNot(BeEmpty())
			Expect(info.GoVersion).To(Equal(runtime.Version()))
		})
	})
})
