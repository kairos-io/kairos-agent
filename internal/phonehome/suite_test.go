package phonehome_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPhoneHome(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PhoneHome Suite")
}
