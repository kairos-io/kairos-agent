// Copyright © 2022 Ettore Di Giacinto <mudler@c3os.io>
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, see <http://www.gnu.org/licenses/>.

package config_test

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	. "github.com/kairos-io/kairos-sdk/schema"
	. "github.com/kairos-io/kairos-agent/v2/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type TConfig struct {
	Kairos struct {
		OtherKey     string `yaml:"other_key"`
		NetworkToken string `yaml:"network_token"`
	} `yaml:"kairos"`
}

var _ = Describe("Config", func() {
	var d string
	BeforeEach(func() {
		d, _ = os.MkdirTemp("", "xxxx")
	})

	AfterEach(func() {
		if d != "" {
			os.RemoveAll(d)
		}
	})
})

func getTagName(s string) string {
	if len(s) < 1 {
		return ""
	}

	if s == "-" {
		return ""
	}

	f := func(c rune) bool {
		return c == '"' || c == ','
	}
	return s[:strings.IndexFunc(s, f)]
}

func structContainsField(f, t string, str interface{}) bool {
	values := reflect.ValueOf(str)
	types := values.Type()

	for j := 0; j < values.NumField(); j++ {
		tagName := getTagName(types.Field(j).Tag.Get("json"))
		if types.Field(j).Name == f || tagName == t {
			return true
		} else {
			if types.Field(j).Type.Kind() == reflect.Struct {
				if types.Field(j).Type.Name() != "" {
					model := reflect.New(types.Field(j).Type)
					if instance, ok := model.Interface().(OneOfModel); ok {
						for _, childSchema := range instance.JSONSchemaOneOf() {
							if structContainsField(f, t, childSchema) {
								return true
							}
						}
					}
				}
			}
		}
	}

	return false
}

func structFieldsContainedInOtherStruct(left, right interface{}) {
	leftValues := reflect.ValueOf(left)
	leftTypes := leftValues.Type()

	for i := 0; i < leftValues.NumField(); i++ {
		leftTagName := getTagName(leftTypes.Field(i).Tag.Get("yaml"))
		leftFieldName := leftTypes.Field(i).Name
		if leftTypes.Field(i).IsExported() {
			It(fmt.Sprintf("Checks that the new schema contians the field %s", leftFieldName), func() {
				Expect(
					structContainsField(leftFieldName, leftTagName, right),
				).To(BeTrue())
			})
		}
	}
}

var _ = Describe("Schema", func() {
	Context("NewConfigFromYAML", func() {
		Context("While the new Schema is not the single source of truth", func() {
			structFieldsContainedInOtherStruct(Config{}, RootSchema{})
		})
		Context("While the new InstallSchema is not the single source of truth", func() {
			structFieldsContainedInOtherStruct(Install{}, InstallSchema{})
		})
		Context("While the new BundleSchema is not the single source of truth", func() {
			structFieldsContainedInOtherStruct(Bundle{}, BundleSchema{})
		})
	})
})
