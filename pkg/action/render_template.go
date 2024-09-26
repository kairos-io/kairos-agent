package action

import (
	"bytes"
	"os"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/state"
	"gopkg.in/yaml.v3"
)

func RenderTemplate(path string, config *config.Config, runtime state.Runtime) ([]byte, error) {
	// Marshal runtime to YAML then to Map so that it is consistent with the output of 'kairos-agent state'
	var runtimeMap map[string]interface{}
	err := yaml.Unmarshal([]byte(runtime.String()), &runtimeMap)
	if err != nil {
		return nil, err
	}

	tpl, err := loadTemplateFile(path)
	if err != nil {
		return nil, err
	}

	result := new(bytes.Buffer)
	err = tpl.Execute(result, map[string]interface{}{
		"Config": config.Config.Values,
		"State":  runtimeMap,
	})
	if err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

func loadTemplateFile(path string) (*template.Template, error) {
	tpl := template.New(path).Funcs(sprig.FuncMap())
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return tpl.Parse(string(content))
}
