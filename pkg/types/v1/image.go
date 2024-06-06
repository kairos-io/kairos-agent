/*
Copyright © 2022 - 2023 SUSE LLC

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

package v1

import (
	"github.com/kairos-io/kairos-sdk/utils"
)

type ImageExtractor interface {
	ExtractImage(imageRef, destination, platformRef string) error
	GetOCIImageSize(imageRef, platformRef string) (int64, error)
}

type OCIImageExtractor struct{}

var _ ImageExtractor = OCIImageExtractor{}

func (e OCIImageExtractor) ExtractImage(imageRef, destination, platformRef string) error {
	img, err := utils.GetImage(imageRef, utils.GetCurrentPlatform(), nil, nil)
	if err != nil {
		return err
	}

	return utils.ExtractOCIImage(img, destination)
}

func (e OCIImageExtractor) GetOCIImageSize(imageRef, platformRef string) (int64, error) {
	return utils.GetOCIImageSize(imageRef, platformRef, nil, nil)
}
