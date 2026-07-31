package kairos_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestKairos(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kairos Suite")
}
