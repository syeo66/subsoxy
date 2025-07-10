package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/syeo66/subsoxy/errors"
)

// encryptedCredential holds encrypted password data
type encryptedCredential struct {
	EncryptedPassword []byte `json:"encrypted_password"`
	Nonce            []byte `json:"nonce"`
}

type Manager struct {
	validCredentials map[string]encryptedCredential
	mutex            sync.RWMutex
	logger           *logrus.Logger
	upstreamURL      string
	encryptionKey    []byte
}

func New(logger *logrus.Logger, upstreamURL string) *Manager {
	// Generate a random key for this instance
	encryptionKey := generateEncryptionKey()
	
	return &Manager{
		validCredentials: make(map[string]encryptedCredential),
		logger:           logger,
		upstreamURL:      upstreamURL,
		encryptionKey:    encryptionKey,
	}
}

func (cm *Manager) ValidateAndStore(username, password string) error {
	if username == "" || password == "" {
		err := errors.ErrInvalidCredentials.WithContext("reason", "empty username or password")
		cm.logger.WithError(err).Warn("Invalid credentials provided")
		return err
	}

	cm.mutex.RLock()
	if storedCred, exists := cm.validCredentials[username]; exists {
		if decryptedPassword, err := cm.decryptPassword(storedCred); err == nil && decryptedPassword == password {
			cm.mutex.RUnlock()
			return nil
		}
	}
	cm.mutex.RUnlock()
	
	if err := cm.validate(username, password); err != nil {
		cm.logger.WithError(err).WithField("username", username).Warn("Invalid credentials provided")
		return err
	}

	encryptedCred, err := cm.encryptPassword(password)
	if err != nil {
		return errors.Wrap(err, errors.CategoryCredentials, "ENCRYPTION_FAILED", "failed to encrypt password")
	}
	
	cm.mutex.Lock()
	cm.validCredentials[username] = encryptedCred
	cm.mutex.Unlock()
	
	cm.logger.WithField("username", username).Info("Credentials validated and stored")
	return nil
}

func (cm *Manager) validate(username, password string) error {
	// Construct URL with proper encoding to prevent credential exposure in logs
	baseURL, err := url.Parse(cm.upstreamURL + "/rest/ping")
	if err != nil {
		return errors.Wrap(err, errors.CategoryCredentials, "VALIDATION_FAILED", "failed to parse upstream URL").
			WithContext("username", username).
			WithContext("upstream_url", cm.upstreamURL)
	}
	
	// Use URL query parameters to safely encode credentials
	params := url.Values{}
	params.Add("u", username)
	params.Add("p", password)
	params.Add("v", "1.15.0")
	params.Add("c", "subsoxy")
	params.Add("f", "json")
	baseURL.RawQuery = params.Encode()
	
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	
	resp, err := client.Get(baseURL.String())
	if err != nil {
		return errors.Wrap(err, errors.CategoryCredentials, "VALIDATION_FAILED", "failed to validate credentials").
			WithContext("username", username).
			WithContext("upstream_url", cm.upstreamURL)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return errors.ErrCredentialsValidation.WithContext("username", username).
			WithContext("status_code", resp.StatusCode).
			WithContext("reason", "non-200 response")
	}
	
	var pingResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&pingResp); err != nil {
		return errors.Wrap(err, errors.CategoryCredentials, "VALIDATION_FAILED", "failed to decode ping response").
			WithContext("username", username)
	}
	
	if subsonicResp, ok := pingResp["subsonic-response"].(map[string]interface{}); ok {
		if status, ok := subsonicResp["status"].(string); ok {
			if status == "ok" {
				cm.logger.WithField("username", username).Info("Successfully validated credentials")
				return nil
			} else {
				return errors.ErrInvalidCredentials.WithContext("username", username).
					WithContext("subsonic_status", status).
					WithContext("reason", "invalid username/password")
			}
		}
	}
	
	return errors.ErrCredentialsValidation.WithContext("username", username).
		WithContext("reason", "invalid response format from upstream server")
}

func (cm *Manager) GetValid() (string, string) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	
	for username, encryptedCred := range cm.validCredentials {
		if password, err := cm.decryptPassword(encryptedCred); err == nil {
			return username, password
		} else {
			// If decryption fails, skip this credential
			cm.logger.WithError(err).WithField("username", username).Warn("Failed to decrypt stored password")
		}
	}
	
	return "", ""
}

// GetAllValid returns all valid credentials as a map of username to password
func (cm *Manager) GetAllValid() map[string]string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	
	validCreds := make(map[string]string)
	for username, encryptedCred := range cm.validCredentials {
		if password, err := cm.decryptPassword(encryptedCred); err == nil {
			validCreds[username] = password
		} else {
			// If decryption fails, skip this credential
			cm.logger.WithError(err).WithField("username", username).Warn("Failed to decrypt stored password")
		}
	}
	
	return validCreds
}

func (cm *Manager) ClearInvalid() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	
	if len(cm.validCredentials) > 0 {
		cm.logger.Warn("Clearing potentially invalid credentials")
		// Clear encrypted credentials securely
		for username := range cm.validCredentials {
			// Zero out the encrypted data before clearing
			if cred, exists := cm.validCredentials[username]; exists {
				for i := range cred.EncryptedPassword {
					cred.EncryptedPassword[i] = 0
				}
				for i := range cred.Nonce {
					cred.Nonce[i] = 0
				}
			}
		}
		cm.validCredentials = make(map[string]encryptedCredential)
	}
}

// generateEncryptionKey creates a random 32-byte key for AES-256
func generateEncryptionKey() []byte {
	// Use a combination of random bytes and system entropy
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Fallback to deterministic key if crypto/rand fails
		// This should never happen in practice
		hash := sha256.Sum256([]byte("subsoxy-fallback-key"))
		return hash[:]
	}
	return key
}

// encryptPassword encrypts a password using AES-256-GCM
func (cm *Manager) encryptPassword(password string) (encryptedCredential, error) {
	block, err := aes.NewCipher(cm.encryptionKey)
	if err != nil {
		return encryptedCredential{}, err
	}
	
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return encryptedCredential{}, err
	}
	
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return encryptedCredential{}, err
	}
	
	encryptedPassword := gcm.Seal(nil, nonce, []byte(password), nil)
	
	return encryptedCredential{
		EncryptedPassword: encryptedPassword,
		Nonce:            nonce,
	}, nil
}

// decryptPassword decrypts a password using AES-256-GCM
func (cm *Manager) decryptPassword(cred encryptedCredential) (string, error) {
	block, err := aes.NewCipher(cm.encryptionKey)
	if err != nil {
		return "", err
	}
	
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	
	plaintext, err := gcm.Open(nil, cred.Nonce, cred.EncryptedPassword, nil)
	if err != nil {
		return "", err
	}
	
	return string(plaintext), nil
}

// GetEncryptionInfo returns information about the encryption setup (for testing)
func (cm *Manager) GetEncryptionInfo() string {
	return base64.StdEncoding.EncodeToString(cm.encryptionKey[:8]) // Only first 8 bytes for identification
}