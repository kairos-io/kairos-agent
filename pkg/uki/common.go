package uki

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/foxboron/go-uefi/efi"
	"github.com/foxboron/go-uefi/efi/pecoff"
	"github.com/foxboron/go-uefi/efi/pkcs7"
	"github.com/foxboron/go-uefi/efi/util"
	"io"
	"os"
	"path/filepath"
	"strings"

	sdkTypes "github.com/kairos-io/kairos-sdk/types"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	sdkutils "github.com/kairos-io/kairos-sdk/utils"
	"github.com/sanity-io/litter"
)

const UnassignedArtifactRole = "norole"

// overwriteArtifactSetRole first deletes all artifacts prefixed with newRole
// (because they are going to be replaced, e.g. old "passive") and then installs
// the artifacts prefixed with oldRole as newRole.
// E.g. removes "passive" and moved "active" to "passive"
// This is a step that should happen before a new passive is installed on upgrades.
func overwriteArtifactSetRole(fs v1.FS, dir, oldRole, newRole string, logger sdkTypes.KairosLogger) error {
	if err := removeArtifactSetWithRole(fs, dir, newRole); err != nil {
		return fmt.Errorf("deleting role %s: %w", newRole, err)
	}

	if err := copyArtifactSetRole(fs, dir, oldRole, newRole, logger); err != nil {
		return fmt.Errorf("copying artifact set role from %s to %s: %w", oldRole, newRole, err)
	}

	return nil
}

// copy the source file but rename the base name to as
func copyArtifact(source, oldRole, newRole string) (string, error) {
	newName := strings.ReplaceAll(source, oldRole, newRole)
	return newName, copyFile(source, newName)
}

func removeArtifactSetWithRole(fs v1.FS, artifactDir, role string) error {
	return fsutils.WalkDirFs(fs, artifactDir, func(path string, info os.DirEntry, err error) error {
		if !info.IsDir() && strings.HasPrefix(info.Name(), role) {
			return os.Remove(path)
		}

		return nil
	})
}

func copyArtifactSetRole(fs v1.FS, artifactDir, oldRole, newRole string, logger sdkTypes.KairosLogger) error {
	return fsutils.WalkDirFs(fs, artifactDir, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasPrefix(info.Name(), oldRole) {
			return nil
		}

		newPath, err := copyArtifact(path, oldRole, newRole)
		if err != nil {
			return fmt.Errorf("copying artifact from %s to %s: %w", path, newPath, err)
		}
		if strings.HasSuffix(path, ".conf") {
			if err := replaceRoleInKey(newPath, "efi", oldRole, newRole, logger); err != nil {
				return err
			}
			if err := replaceConfTitle(newPath, newRole); err != nil {
				return err
			}
		}

		return nil
	})
}

func replaceRoleInKey(path, key, oldRole, newRole string, logger sdkTypes.KairosLogger) (err error) {
	// Extract the values
	conf, err := sdkutils.SystemdBootConfReader(path)
	if err != nil {
		logger.Errorf("Error reading conf file %s: %s", path, err)
		return err
	}
	logger.Debugf("Conf file %s has values %v", path, litter.Sdump(conf))

	_, hasKey := conf[key]
	if !hasKey {
		return fmt.Errorf("no %s entry in .conf file", key)
	}

	conf[key] = strings.ReplaceAll(conf[key], oldRole, newRole)
	newContents := ""
	for k, v := range conf {
		newContents = fmt.Sprintf("%s%s %s\n", newContents, k, v)
	}
	logger.Debugf("Conf file %s new values %v", path, litter.Sdump(conf))

	return os.WriteFile(path, []byte(newContents), os.ModePerm)
}

func replaceConfTitle(path, role string) error {
	conf, err := sdkutils.SystemdBootConfReader(path)
	if err != nil {
		return err
	}

	if len(conf["title"]) == 0 {
		return errors.New("no title in .conf file")
	}

	newTitle, err := constants.BootTitleForRole(role, conf["title"])
	if err != nil {
		return err
	}

	conf["title"] = newTitle
	newContents := ""
	for k, v := range conf {
		newContents = fmt.Sprintf("%s%s %s\n", newContents, k, v)
	}

	return os.WriteFile(path, []byte(newContents), os.ModePerm)
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		panic(err)
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(dst)
	if err != nil {
		panic(err)
	}
	defer destinationFile.Close()

	if _, err = io.Copy(destinationFile, sourceFile); err != nil {
		return err
	}

	// Flushes any buffered data to the destination file
	if err = destinationFile.Sync(); err != nil {
		return err
	}

	if err = sourceFile.Close(); err != nil {
		return err
	}

	return destinationFile.Close()
}

// checkArtifactsignatureIsValid checks that a give efi artifact is signed properly with a signature that would allow it to
// boot correctly in the current node if secureboot is enabled
func checkArtifactsignatureIsValid(fs v1.FS, dir string, artifact string, logger sdkTypes.KairosLogger) error {
	var err error
	fullArtifact := filepath.Join(dir, artifact)
	logger.Logger.Info().Str("what", fullArtifact).Msg("Checking artifact")
	o, err := fs.Open(fullArtifact)
	if errors.Is(err, os.ErrNotExist) {
		logger.Warn("%s does not exist", fullArtifact)
		return fmt.Errorf("%s does not exist", fullArtifact)
	} else if errors.Is(err, os.ErrPermission) {
		logger.Warn("%s permission denied. Can't read file", fullArtifact)
		return fmt.Errorf("%s permission denied. Can't read file", fullArtifact)
	}

	ok, err := checkValidEfiHeader(o)
	_ = o.Close()
	if err != nil {
		logger.Error(fmt.Errorf("failed to read file %s: %s", fullArtifact, err))
		return fmt.Errorf("failed to read file %s: %s", fullArtifact, err)
	}
	if !ok {
		logger.Error("invalid pe header")
		return fmt.Errorf("invalid pe header")
	}

	// First check if its on dbx, if it matches, then it means that its not valid and we can return early
	dbx, err := efi.Getdbx()
	if err != nil {
		logger.Logger.Error().Err(err).Msg("getting DBx certs")
		return err
	}

	x509CertDbx, err := util.ReadCert(dbx.Bytes())
	if err != nil {
		return err
	}

	// We need to read the current db database to have the proper certs to check against
	db, err := efi.Getdb()
	if err != nil {
		logger.Logger.Error().Err(err).Msg("getting DB certs")
		return err
	}

	x509CertDb, err := util.ReadCert(db.Bytes())
	if err != nil {
		return err
	}

	f, err := fs.ReadFile(fullArtifact)
	if err != nil {
		return fmt.Errorf("%s: %w", fullArtifact, err)
	}
	sigs, err := pecoff.GetSignatures(f)
	if err != nil {
		return fmt.Errorf("%s: %w", fullArtifact, err)
	}
	if len(sigs) == 0 {
		return fmt.Errorf("no signatures in the file %s", fullArtifact)
	}

	for _, signature := range sigs {
		ok, err := pkcs7.VerifySignature(x509CertDbx, signature.Certificate)
		if err != nil {
			return err
		}
		// if its on dbx, return failure quick
		if ok {
			return fmt.Errorf("Signature is on the dbx list")
		} else {
			// Check to see if its on the db instead
			ok, err := pkcs7.VerifySignature(x509CertDb, signature.Certificate)
			if err != nil {
				return err
			}
			if ok {
				// Signature is not on DBX but its on DB, verified
				return nil
			}
		}
	}
	return errors.New("not ok")
}

func checkValidEfiHeader(r io.Reader) (bool, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		} else if errors.Is(err, io.ErrUnexpectedEOF) {
			return false, nil
		}
		return false, err
	}
	if !bytes.Equal(header[:], []byte{0x4d, 0x5a}) {
		return false, nil
	}
	return true, nil
}
