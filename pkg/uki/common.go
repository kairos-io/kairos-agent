package uki

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/foxboron/go-uefi/efi"
	"github.com/foxboron/go-uefi/efi/pecoff"
	"github.com/foxboron/go-uefi/efi/pkcs7"
	"github.com/foxboron/go-uefi/efi/signature"
	"github.com/foxboron/go-uefi/efi/util"
	"github.com/kairos-io/kairos-sdk/signatures"
	"io"
	"os"
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

// checkArtifactSignatureIsValid checks that a give efi artifact is signed properly with a signature that would allow it to
// boot correctly in the current node if secureboot is enabled
func checkArtifactSignatureIsValid(fs v1.FS, artifact string, logger sdkTypes.KairosLogger) error {
	var err error
	logger.Logger.Info().Str("what", artifact).Msg("Checking artifact for valid signature")
	o, err := fs.Open(artifact)
	if errors.Is(err, os.ErrNotExist) {
		logger.Warnf("%s does not exist", artifact)
		return fmt.Errorf("%s does not exist", artifact)
	} else if errors.Is(err, os.ErrPermission) {
		logger.Warnf("%s permission denied. Can't read file", artifact)
		return fmt.Errorf("%s permission denied. Can't read file", artifact)
	}

	ok, err := checkValidEfiHeader(o)
	_ = o.Close()
	if err != nil {
		logger.Error(fmt.Errorf("failed to read file %s: %s", artifact, err))
		return fmt.Errorf("failed to read file %s: %s", artifact, err)
	}
	if !ok {
		logger.Error("invalid pe header")
		return fmt.Errorf("invalid pe header")
	}

	// We need to read the current db database to have the proper certs to check against
	db, err := efi.Getdb()
	if err != nil {
		logger.Logger.Error().Err(err).Msg("getting DB certs")
		return err
	}

	dbCerts := signatures.ExtractCertsFromSignatureDatabase(db)

	f, err := fs.ReadFile(artifact)
	if err != nil {
		return fmt.Errorf("%s: %w", artifact, err)
	}
	sigs, err := pecoff.GetSignatures(f)
	if err != nil {
		return fmt.Errorf("%s: %w", artifact, err)
	}
	if len(sigs) == 0 {
		return fmt.Errorf("no signatures in the file %s", artifact)
	}

	// TODO: dbx check first, it has a different format
	for _, sig := range sigs {
		// Check to see if it's on the db
		for _, cert := range dbCerts {
			logger.Logger.Debug().Str("what", artifact).Str("subject", cert.Subject.CommonName).Msg("checking signature")
			ok, _ := pkcs7.VerifySignature(cert, sig.Certificate)
			if ok {
				logger.Logger.Info().Str("what", artifact).Str("subject", cert.Subject.CommonName).Msg("verified")
				return nil
			}
		}
	}
	return errors.New("not ok")
}

// isValidSignature identifies a signature based as a DER-encoded X.509 certificate which is the DB format
func isValidDBSignature(sign util.EFIGUID) bool {
	return sign == signature.CERT_X509_GUID
}

// Verify that the 2 first bytes of a file mark it as a DOS/PE file
// This is lifted from sbctl directly, but I did not want to bring the whole dependency for this
// few lines. If we ever bring sbctl as dep we can drop this and use the one in there directly
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
