package uki

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Uki utils", Label("uki", "utils"), func() {
	It("Check valid signature in file", func() {
		checkArtifactsignatureIsValid()
		Expect(true).To(BeTrue())
	})
})
