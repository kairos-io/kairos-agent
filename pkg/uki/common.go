package uki

import (
	"bytes"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/edsrzf/mmap-go"
	"github.com/foxboron/go-uefi/authenticode"
	"github.com/foxboron/go-uefi/efi"
	"github.com/foxboron/go-uefi/efi/signature"
	"github.com/foxboron/go-uefi/pkcs7"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/signatures"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	sdkutils "github.com/kairos-io/kairos-sdk/utils"
	peparser "github.com/saferwall/pe"
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

// checkArtifactSignatureIsValid checks that a given efi artifact is signed properly with a signature that would allow it to
// boot correctly in the current node if secureboot is enabled
func checkArtifactSignatureIsValid(fs v1.FS, artifact string, logger sdkTypes.KairosLogger) error {
	var err error
	logger.Logger.Info().Str("what", artifact).Msg("Checking artifact for valid signature")
	info, err := fs.Stat(artifact)
	if errors.Is(err, os.ErrNotExist) {
		logger.Warnf("%s does not exist", artifact)
		return fmt.Errorf("%s does not exist", artifact)
	} else if errors.Is(err, os.ErrPermission) {
		logger.Warnf("%s permission denied. Can't read file", artifact)
		return fmt.Errorf("%s permission denied. Can't read file", artifact)
	} else if err != nil {
		return err
	}
	if info.Size() == 0 {
		logger.Warnf("%s file is empty denied", artifact)
		return fmt.Errorf("%s file has zero size", artifact)
	}
	logger.Logger.Debug().Str("what", artifact).Msg("Reading artifact")

	// MMAP the file, seems to save memory rather than reading the full file
	// Unfortunately we have to do some type conversion to keep using the v1.Fs
	f, err := fs.Open(artifact)
	defer f.Close()
	if err != nil {
		return err
	}
	// type conversion, ugh
	fOS := f.(*os.File)
	data, err := mmap.Map(fOS, mmap.RDONLY, 0)
	defer data.Unmap()
	if err != nil {
		return err
	}

	// Get sha256 of the artifact
	// Note that this is a PEFile, so it's a bit different from a normal file as there are some sections that need to be
	// excluded when calculating the sha
	logger.Logger.Debug().Str("what", artifact).Msg("Parsing PE artifact")
	file, _ := peparser.NewBytes(data, &peparser.Options{Fast: true})
	err = file.Parse()
	if err != nil {
		logger.Logger.Error().Err(err).Msg("parsing PE file for hash")
		return err
	}

	logger.Logger.Debug().Str("what", artifact).Msg("Checking if its an EFI file")
	// Check for proper header in the efi file
	if file.DOSHeader.Magic != peparser.ImageDOSZMSignature && file.DOSHeader.Magic != peparser.ImageDOSSignature {
		logger.Error(fmt.Errorf("no pe file header: %d", file.DOSHeader.Magic))
		return fmt.Errorf("no pe file header: %d", file.DOSHeader.Magic)
	}

	// Get hash to compare in dbx if we have hashes
	hashArtifact := hex.EncodeToString(file.Authentihash())

	logger.Logger.Debug().Str("what", artifact).Msg("Getting DB certs")
	// We need to read the current db database to have the proper certs to check against
	db, err := efi.Getdb()
	if err != nil {
		logger.Logger.Error().Err(err).Msg("Getting DB certs")
		return err
	}

	dbCerts := signatures.ExtractCertsFromSignatureDatabase(db)

	logger.Logger.Debug().Str("what", artifact).Msg("Getting signatures from artifact")
	// Get signatures from the artifact
	binary, err := authenticode.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: %w", artifact, err)
	}
	if binary.Datadir.Size == 0 {
		return fmt.Errorf("no signatures in the file %s", artifact)
	}

	sigs, err := binary.Signatures()
	if err != nil {
		return fmt.Errorf("%s: %w", artifact, err)
	}

	logger.Logger.Debug().Str("what", artifact).Msg("Getting DBX certs")
	dbx, err := efi.Getdbx()
	if err != nil {
		logger.Logger.Error().Err(err).Msg("getting DBX certs")
		return err
	}

	// First check the dbx database as it has precedence, on match, return immediately
	for _, k := range *dbx {
		switch k.SignatureType {
		case signature.CERT_SHA256_GUID: // SHA256 hash
			// Compare it against the dbx
			for _, k1 := range k.Signatures {
				shaSign := hex.EncodeToString(k1.Data)
				logger.Logger.Debug().Str("artifact", string(hashArtifact)).Str("signature", shaSign).Msg("Comparing hashes")
				if hashArtifact == shaSign {
					return fmt.Errorf("hash appears on DBX: %s", hashArtifact)
				}

			}
		case signature.CERT_X509_GUID: // Certificate
			var result []*x509.Certificate
			for _, k1 := range k.Signatures {
				certificates, err := x509.ParseCertificates(k1.Data)
				if err != nil {
					continue
				}
				result = append(result, certificates...)
			}
			for _, sig := range sigs {
				for _, cert := range result {
					logger.Logger.Debug().Str("what", artifact).Str("subject", cert.Subject.CommonName).Msg("checking signature")
					p, err := pkcs7.ParsePKCS7(sig.Certificate)
					if err != nil {
						logger.Logger.Info().Str("error", err.Error()).Msg("parsing signature")
						return err
					}
					ok, _ := p.Verify(cert)
					// If cert matches then it means its blacklisted so return error
					if ok {
						return fmt.Errorf("artifact is signed with a blacklisted cert")
					}

				}
			}
		default:
			logger.Logger.Debug().Str("what", artifact).Str("cert type", string(signature.ValidEFISignatureSchemes[k.SignatureType])).Msg("not supported type of cert")
		}
	}

	// Now check against the DB to see if its allowed
	for _, sig := range sigs {
		for _, cert := range dbCerts {
			logger.Logger.Debug().Str("what", artifact).Str("subject", cert.Subject.CommonName).Msg("checking signature")
			p, err := pkcs7.ParsePKCS7(sig.Certificate)
			if err != nil {
				logger.Logger.Info().Str("error", err.Error()).Msg("parsing signature")
				return err
			}
			ok, _ := p.Verify(cert)
			if ok {
				logger.Logger.Info().Str("what", artifact).Str("subject", cert.Subject.CommonName).Msg("verified")
				return nil
			}
		}
	}
	// If we reach this point, we need to fail as we haven't matched anything, so default is to fail
	return fmt.Errorf("could not find a signature in EFIVars DB that matches the artifact")
}
