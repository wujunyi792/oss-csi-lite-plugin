/*
Copyright 2020 The Alibaba Cloud Authors.

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

package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth"
	"github.com/emirpasic/gods/sets/hashset"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	// ConfigPath the secret mount file
	ConfigPath = "/var/addon/token-config"
)

// AKInfo access key info
type AKInfo struct {
	// AccessKeyId access key id
	AccessKeyID string `json:"access.key.id"`
	// AccessKeySecret access key secret
	AccessKeySecret string `json:"access.key.secret"`
	// SecurityToken security token
	SecurityToken string `json:"security.token"`
	// Expiration expiration duration
	Expiration string `json:"expiration"`
	// Keyring key ring
	Keyring string `json:"keyring"`
	// RoleAccessKeyId key
	RoleAccessKeyID string `json:"role.access.key.id"`
	// RoleAccessKeySecret key
	RoleAccessKeySecret string `json:"role.access.key.secret"`
	// RoleArn key
	RoleArn string `json:"role.arn"`
}

// ManageTokens 定义资源账号 和 角色扮演账号
type ManageTokens struct {
	// AccessKeyId key
	AccessKeyID string
	// AccessKeySecret key
	AccessKeySecret string
	// SecurityToken key
	SecurityToken string

	// RoleAccessKeyId key
	RoleAccessKeyID string
	// RoleAccessKeySecret key
	RoleAccessKeySecret string
	// RoleArn key
	RoleArn string
}

// KeyPairArtifacts is cert struct
type KeyPairArtifacts struct {
	Cert    *x509.Certificate
	Key     *rsa.PrivateKey
	CertPEM []byte
	KeyPEM  []byte
}

// CertOption is cert option
type CertOption struct {
	CAName          string
	CAOrganizations []string
	DNSNames        []string
	CommonName      string
}

// AccessControlMode is int, represents different modes
type AccessControlMode int

// AccessControlMode includes AccessKey, ManagedToken, EcsRamRole, Credential, RoleArnToken, five types of access control
const (
	AccessKey AccessControlMode = iota
	ManagedToken
	EcsRAMRole
	Credential
	RoleArnToken
	OIDCToken
)

// AccessControl is access control option
type AccessControl struct {
	AccessKeyID     string
	AccessKeySecret string
	StsToken        string
	RoleArn         string
	Config          *sdk.Config
	Credential      auth.Credential
	UseMode         AccessControlMode
}

var (
	// cmdSet is support cmd set
	cmdSet = hashset.New("mount", "lctl", "umount", "nsenter", "findmnt", "chmod", "dd", "mkfs.ext4", "cat", "ps", "hostname", "sysctl")
	// cmdRegexp is not support cmd args
	cmdRegexp = "[|$&;`'<>()%+\\\\]"
)

func CheckCmdArgs(cmd string, args ...string) error {
	for _, element := range args {
		match, err := regexp.MatchString(cmdRegexp, element)
		if err != nil {
			return fmt.Errorf("Command %s is regexp is failed, args:%s, err:%s.", cmd, element, err)
		}
		if match {
			return fmt.Errorf("Command %s has illegal access, args:%s.", cmd, element)
		}
	}
	return nil
}

func CheckCmd(cmd string, name string) error {
	if !cmdSet.Contains(name) {
		return fmt.Errorf("Command %s has illegal access, base command:%s.", cmd, name)
	}
	return nil
}

// CheckRequestArgs is check string is valid in args map
func CheckRequestArgs(m map[string]string) (bool, error) {
	valid := true
	var msg string
	for _, value := range m {
		if strings.Contains(value, "&") || strings.Contains(value, "|") || strings.Contains(value, ";") ||
			strings.Contains(value, "$") || strings.Contains(value, "'") || strings.Contains(value, "`") ||
			strings.Contains(value, "(") || strings.Contains(value, ")") {
			valid = false
			msg = msg + fmt.Sprintf("Args %s has illegal access.", value)
		}
	}
	return valid, errors.New(msg)
}

// ValidatePath is check path string is valid
func ValidatePath(path string) (bool, error) {
	var msg string
	if strings.Contains(path, "../") || strings.Contains(path, "/..") || strings.Contains(path, "..") {
		msg = msg + fmt.Sprintf("Path %s has illegal access.", path)
		return false, errors.New(msg)
	}
	if strings.Contains(path, "./") || strings.Contains(path, "/.") {
		msg = msg + fmt.Sprintf("Path %s has illegal access.", path)
		return false, errors.New(msg)
	}

	return true, nil
}

func CheckRequest(m map[string]string, path string) (bool, error) {
	valid, err := CheckRequestArgs(m)
	if !valid {
		return valid, err
	}
	valid, err = ValidatePath(path)
	if !valid {
		return valid, err
	}
	return valid, nil
}

// GetEnvAK read ak from local ENV
func GetEnvAK() AccessControl {
	accessKeyID, accessSecret := "", ""
	accessKeyID = os.Getenv("ACCESS_KEY_ID")
	accessSecret = os.Getenv("ACCESS_KEY_SECRET")

	return AccessControl{AccessKeyID: strings.TrimSpace(accessKeyID), AccessKeySecret: strings.TrimSpace(accessSecret), UseMode: AccessKey}
}

// GetManagedToken get ak from csi secret
func getManagedToken() (tokens ManageTokens) {
	var akInfo AKInfo
	if _, err := os.Stat(ConfigPath); err == nil {
		encodeTokenCfg, err := ioutil.ReadFile(ConfigPath)
		if err != nil {
			log.Errorf("failed to read token config, err: %v", err)
			return ManageTokens{}
		}
		err = json.Unmarshal(encodeTokenCfg, &akInfo)
		if err != nil {
			log.Errorf("error unmarshal token config: %v", err)
			return ManageTokens{}
		}
		keyring := akInfo.Keyring
		ak, err := Decrypt(akInfo.AccessKeyID, []byte(keyring))
		if err != nil {
			log.Errorf("failed to decode ak, err: %v", err)
			return ManageTokens{}
		}

		sk, err := Decrypt(akInfo.AccessKeySecret, []byte(keyring))
		if err != nil {
			log.Errorf("failed to decode sk, err: %v", err)
			return ManageTokens{}
		}

		token, err := Decrypt(akInfo.SecurityToken, []byte(keyring))
		if err != nil {
			log.Errorf("failed to decode token, err: %v", err)
			return ManageTokens{}
		}
		layout := "2006-01-02T15:04:05Z"
		t, err := time.Parse(layout, akInfo.Expiration)
		if err != nil {
			log.Errorf("Parse expiration error: %s", err.Error())
		}
		if t.Before(time.Now()) {
			log.Errorf("invalid token which is expired, expiration as: %s", akInfo.Expiration)
		}
		tokens.AccessKeyID = string(ak)
		tokens.AccessKeySecret = string(sk)
		tokens.SecurityToken = string(token)

		if akInfo.RoleAccessKeyID != "" && akInfo.RoleAccessKeySecret != "" {
			roleAK, err := Decrypt(akInfo.RoleAccessKeyID, []byte(keyring))
			if err != nil {
				log.Errorf("failed to decode role ak, err: %v", err)
				return ManageTokens{}
			}
			roleSK, err := Decrypt(akInfo.RoleAccessKeySecret, []byte(keyring))
			if err != nil {
				log.Errorf("failed to decode role sk, err : %v", err)
				return ManageTokens{}
			}
			tokens.RoleAccessKeyID = string(roleAK)
			tokens.RoleAccessKeySecret = string(roleSK)
		}
		tokens.RoleArn = akInfo.RoleArn
	}
	return tokens
}

// PKCS5UnPadding get pkc
func PKCS5UnPadding(origData []byte) []byte {
	length := len(origData)
	unpadding := int(origData[length-1])
	return origData[:(length - unpadding)]
}

// Decrypt secret Decrypt
func Decrypt(s string, keyring []byte) ([]byte, error) {
	cdata, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		log.Errorf("failed to decode base64 string, err: %v", err)
		return nil, err
	}
	block, err := aes.NewCipher(keyring)
	if err != nil {
		log.Errorf("failed to new cipher, err: %v", err)
		return nil, err
	}
	blockSize := block.BlockSize()

	iv := cdata[:blockSize]
	blockMode := cipher.NewCBCDecrypter(block, iv)
	origData := make([]byte, len(cdata)-blockSize)

	blockMode.CryptBlocks(origData, cdata[blockSize:])

	origData = PKCS5UnPadding(origData)
	return origData, nil
}
