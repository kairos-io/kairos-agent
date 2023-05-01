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

package live

import (
	"fmt"
	"path/filepath"

	"strings"

	"github.com/kairos-io/kairos/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos/v2/pkg/types/v1"
	"github.com/kairos-io/kairos/v2/pkg/utils"
)

type GreenLiveBootLoader struct {
	buildCfg *v1.BuildConfig
	spec     *v1.LiveISO
}

func NewGreenLiveBootLoader(cfg *v1.BuildConfig, spec *v1.LiveISO) *GreenLiveBootLoader {
	return &GreenLiveBootLoader{buildCfg: cfg, spec: spec}
}

func (g *GreenLiveBootLoader) PrepareEFI(rootDir, uefiDir string) error {
	const (
		grubEfiImagex86   = "/usr/share/grub2/x86_64-efi/grub.efi"
		grubEfiImageArm64 = "/usr/share/grub2/arm64-efi/grub.efi"
	)

	err := utils.MkdirAll(g.buildCfg.Fs, filepath.Join(uefiDir, efiBootPath), constants.DirPerm)
	if err != nil {
		return err
	}

	switch g.buildCfg.Arch {
	case constants.ArchAmd64, constants.Archx86:
		err = utils.CopyFile(
			g.buildCfg.Fs,
			filepath.Join(rootDir, grubEfiImagex86),
			filepath.Join(uefiDir, grubEfiImagex86Dest),
		)
	case constants.ArchArm64:
		err = utils.CopyFile(
			g.buildCfg.Fs,
			filepath.Join(rootDir, grubEfiImageArm64),
			filepath.Join(uefiDir, grubEfiImageArm64Dest),
		)
	default:
		err = fmt.Errorf("Not supported architecture: %v", g.buildCfg.Arch)
	}
	if err != nil {
		return err
	}

	return g.buildCfg.Fs.WriteFile(filepath.Join(uefiDir, efiBootPath, grubCfg), []byte(grubEfiCfg), constants.FilePerm)
}

func (g *GreenLiveBootLoader) PrepareISO(rootDir, imageDir string) error {
	const (
		grubFont          = "/usr/share/grub2/unicode.pf2"
		grubBootHybridImg = "/usr/share/grub2/i386-pc/boot_hybrid.img"
		syslinuxFiles     = "/usr/share/syslinux/isolinux.bin " +
			"/usr/share/syslinux/menu.c32 " +
			"/usr/share/syslinux/chain.c32 " +
			"/usr/share/syslinux/mboot.c32"
	)

	err := utils.MkdirAll(g.buildCfg.Fs, filepath.Join(imageDir, grubPrefixDir), constants.DirPerm)
	if err != nil {
		return err
	}

	switch g.buildCfg.Arch {
	case constants.ArchAmd64, constants.Archx86:
		// Create eltorito image
		eltorito, err := g.BuildEltoritoImg(rootDir)
		if err != nil {
			return err
		}

		// Inlude loaders in expected paths
		loaderDir := filepath.Join(imageDir, isoLoaderPath)
		err = utils.MkdirAll(g.buildCfg.Fs, loaderDir, constants.DirPerm)
		if err != nil {
			return err
		}
		loaderFiles := []string{eltorito, grubBootHybridImg}
		loaderFiles = append(loaderFiles, strings.Split(syslinuxFiles, " ")...)
		for _, f := range loaderFiles {
			err = utils.CopyFile(g.buildCfg.Fs, filepath.Join(rootDir, f), loaderDir)
			if err != nil {
				return err
			}
		}
		fontsDir := filepath.Join(loaderDir, "/grub2/fonts")
		err = utils.MkdirAll(g.buildCfg.Fs, fontsDir, constants.DirPerm)
		if err != nil {
			return err
		}
		err = utils.CopyFile(g.buildCfg.Fs, filepath.Join(rootDir, grubFont), fontsDir)
		if err != nil {
			return err
		}
	case constants.ArchArm64:
		// TBC
	default:
		return fmt.Errorf("Not supported architecture: %v", g.buildCfg.Arch)
	}

	// Write grub.cfg file
	err = g.buildCfg.Fs.WriteFile(
		filepath.Join(imageDir, grubPrefixDir, grubCfg),
		[]byte(fmt.Sprintf(grubCfgTemplate, g.spec.GrubEntry, g.spec.Label)),
		constants.FilePerm,
	)
	if err != nil {
		return err
	}

	// Include EFI contents in iso root too
	return g.PrepareEFI(rootDir, imageDir)
}

func (g *GreenLiveBootLoader) BuildEltoritoImg(rootDir string) (string, error) {
	const (
		grubBiosTarget  = "i386-pc"
		grubI386BinDir  = "/usr/share/grub2/i386-pc"
		grubBiosImg     = grubI386BinDir + "/core.img"
		grubBiosCDBoot  = grubI386BinDir + "/cdboot.img"
		grubEltoritoImg = grubI386BinDir + "/eltorito.img"
		//TODO this list could be optimized
		grubModules = "ext2 iso9660 linux echo configfile search_label search_fs_file search search_fs_uuid " +
			"ls normal gzio png fat gettext font minicmd gfxterm gfxmenu all_video xfs btrfs lvm luks " +
			"gcry_rijndael gcry_sha256 gcry_sha512 crypto cryptodisk test true loadenv part_gpt " +
			"part_msdos biosdisk vga vbe chain boot"
	)
	var args []string
	args = append(args, "-O", grubBiosTarget)
	args = append(args, "-o", grubBiosImg)
	args = append(args, "-p", grubPrefixDir)
	args = append(args, "-d", grubI386BinDir)
	args = append(args, strings.Split(grubModules, " ")...)

	chRoot := utils.NewChroot(rootDir, &g.buildCfg.Config)
	out, err := chRoot.Run("grub2-mkimage", args...)
	if err != nil {
		g.buildCfg.Logger.Errorf("grub2-mkimage failed: %s", string(out))
		g.buildCfg.Logger.Errorf("Error: %v", err)
		return "", err
	}

	concatFiles := func() error {
		return utils.ConcatFiles(
			g.buildCfg.Fs, []string{grubBiosCDBoot, grubBiosImg},
			grubEltoritoImg,
		)
	}
	err = chRoot.RunCallback(concatFiles)
	if err != nil {
		return "", err
	}
	return grubEltoritoImg, nil
}
