/*
 * Copyright 2023 Venafi, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package installer

import (
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/Venafi/vcert/v5/pkg/certificate"
	"github.com/Venafi/vcert/v5/pkg/playbook/app/domain"
	"github.com/Venafi/vcert/v5/pkg/playbook/app/vcertutil"
	"github.com/Venafi/vcert/v5/pkg/playbook/util"
)

// PEMInstaller represents an installation that will use the PEM format for the certificate bundle
type PEMInstaller struct {
	domain.Installation
}

// NewPEMInstaller returns a new installer of type PEM with the values defined in inst
func NewPEMInstaller(inst domain.Installation) PEMInstaller {
	return PEMInstaller{inst}
}

// Check is the method in charge of making the validations to install a new certificate:
// 1. Does the certificate exists? > Install if it doesn't.
// 2. Does the certificate is about to expire? Renew if about to expire.
// Returns true if the certificate needs to be installed.
func (r PEMInstaller) Check(renewBefore string, _ domain.PlaybookRequest) (bool, error) {
	zap.L().Info("checking certificate health", zap.String("format", r.Type.String()), zap.String("location", r.File))

	// Check certificate bundle file exists
	certExists, err := util.FileExists(r.File)
	if err != nil {
		return false, err
	}
	if !certExists {
		return true, nil
	}

	// Load Certificate
	cert, err := loadPEMCertificate(r.File)
	if err != nil {
		return false, err
	}

	// Check certificate expiration
	renew := needRenewal(cert, renewBefore)

	return renew, nil
}

// Backup takes the certificate request and backs up the current version prior to overwriting
func (r PEMInstaller) Backup() error {
	zap.L().Debug("backing up certificate", zap.String("location", r.File))

	// Check certificate file exists
	certExists, err := util.FileExists(r.File)
	if err != nil {
		return err
	}

	// No cert file
	if !certExists {
		zap.L().Info("new certificate location specified, no back up taken")
		return nil
	}

	resources := []struct {
		oldLocation string
		newLocation string
	}{
		{oldLocation: r.File, newLocation: fmt.Sprintf("%s.bak", r.File)},
		{oldLocation: r.KeyFile, newLocation: fmt.Sprintf("%s.bak", r.KeyFile)},
		{oldLocation: r.ChainFile, newLocation: fmt.Sprintf("%s.bak", r.ChainFile)},
	}

	for _, resource := range resources {
		fileExists, err := util.FileExists(resource.oldLocation)
		if err != nil {
			return err
		} else if !fileExists {
			zap.L().Info(fmt.Sprintf("file %s does not exist, no backup taken", resource.oldLocation))
			continue
		}
		err = util.CopyFile(resource.oldLocation, resource.newLocation)
		if err != nil {
			return err
		}
		zap.L().Info("certificate resource backed up", zap.String("location", resource.oldLocation),
			zap.String("backupLocation", resource.newLocation))
	}

	return nil
}

// Install takes the certificate bundle and moves it to the location specified in the installer
func (r PEMInstaller) Install(_ domain.PlaybookRequest, pcc certificate.PEMCollection) error {
	zap.L().Debug("installing certificate", zap.String("location", r.File))

	//TODO: should we add support for PEM bundle?

	preppedPK := pcc.PrivateKey
	var err error
	// Needs to be encrypted again using legacy PEM
	if r.KeyPassword != "" {
		preppedPK, err = vcertutil.EncryptPrivateKeyPKCS1(pcc.PrivateKey, r.KeyPassword)
		if err != nil {
			zap.L().Error("failed to encrypt PrivateKey", zap.Error(err))
			return err
		}
	}

	resources := []struct {
		path    string
		content []byte
	}{
		{path: r.File, content: []byte(pcc.Certificate)},
		{path: r.KeyFile, content: []byte(preppedPK)},
		{path: r.ChainFile, content: []byte(strings.Join(pcc.Chain, ""))},
	}

	for _, resource := range resources {
		if len(resource.content) == 0 {
			continue
		}
		err = util.WriteFile(resource.path, resource.content)
		if err != nil {
			return err
		}
	}

	return nil
}

// AfterInstallActions runs any instructions declared in the Installer on a terminal.
//
// No validations happen over the content of the AfterAction string, so caution is advised
func (r PEMInstaller) AfterInstallActions() (string, error) {
	zap.L().Debug("running after-install actions", zap.String("location", r.File))

	result, err := util.ExecuteScript(r.AfterAction)
	return result, err
}

// InstallValidationActions runs any instructions declared in the Installer on a terminal and expects
// "0" for successful validation and "1" for a validation failure
// No validations happen over the content of the InstallValidation string, so caution is advised
func (r PEMInstaller) InstallValidationActions() (string, error) {
	zap.L().Debug("running install validation actions", zap.String("location", r.File))

	validationResult, err := util.ExecuteScript(r.InstallValidation)
	if err != nil {
		return "", err
	}

	return validationResult, err
}

func (r PEMInstaller) installAsBundle() bool {
	if r.KeyFile != "" && r.ChainFile != "" {
		return true
	}
	return false
}
