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

package action

import (
	"bytes"
	"github.com/Masterminds/sprig/v3"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/state"
	"gopkg.in/yaml.v3"
	"os"
	"text/template"
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
		"Config": config.Config,
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
