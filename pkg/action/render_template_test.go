package action

import (
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("RenderTemplate action test", func() {

	It("renders the template with config and state", func() {
		config := agentConfig.NewConfig()
		config.Config = collector.Config{
			"testKey": "testValue",
		}
		runtime := state.Runtime{
			UUID: "123abcd1235467",
		}

		result, err := RenderTemplate("../../tests/fixtures/template/test.yaml", config, runtime)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(len(result)).ToNot(BeZero())

		var data map[string]string
		err = yaml.Unmarshal(result, &data)
		Expect(err).ToNot(HaveOccurred())
		Expect(data).To(HaveKeyWithValue("configTest", "TESTVALUE"))
		Expect(data["stateTest"]).To(MatchRegexp("^[0-9a-f]{8}$"))
	})

})
