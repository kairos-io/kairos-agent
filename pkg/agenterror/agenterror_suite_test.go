package agenterror_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAgentError(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AgentError Suite")
}
