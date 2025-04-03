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
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/kairos-io/kairos-sdk/utils"
)

type ImageExtractor interface {
	ExtractImage(imageRef, destination, platformRef string) error
	GetOCIImageSize(imageRef, platformRef string) (int64, error)
}

type OCIImageExtractor struct{}

var _ ImageExtractor = OCIImageExtractor{}

func (e OCIImageExtractor) ExtractImage(imageRef, destination, platformRef string) error {
	// If we pass a platform
	if platformRef != "" {
		// make sure its correct
		_, err := v1.ParsePlatform(platformRef)
		if err != nil {
			// and if we cannot properly parse it, then default to the current platform
			platformRef = utils.GetCurrentPlatform()
		}
	} else {
		// if we don't pass a platform, then default to the current platform
		platformRef = utils.GetCurrentPlatform()
	}
	img, err := utils.GetImage(imageRef, platformRef, nil, nil)
	if err != nil {
		return err
	}

	return utils.ExtractOCIImage(img, destination)
}

func (e OCIImageExtractor) GetOCIImageSize(imageRef, platformRef string) (int64, error) {
	return utils.GetOCIImageSize(imageRef, platformRef, nil, nil)
}
