package credentials

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/syeo66/subsoxy/errors"
)

// Encryption constants
const (
	AES256KeySize          = 32
	KeyIdentificationBytes = 8
	HTTPClientTimeout      = 10 * time.Second
	SubsonicAPIVersion     = "1.15.0"
	ClientName             = "subsoxy"
	// How long a secret confirmed by upstream is trusted without asking upstream again
	VerifiedSecretTTL = 15 * time.Minute
	// Upper bound on remembered secrets per user. Token clients may use a new salt for
	// every request, so this keeps the cache small no matter how many they send.
	MaxVerifiedSecretsPerUser = 16
	// How long a secret upstream rejected is refused without asking upstream again, so a
	// client retrying with a wrong password doesn't cost an upstream ping per request
	RejectedSecretTTL = 5 * time.Minute
	// Upper bound on remembered rejections across all users. Usernames come from requests,
	// so this keeps the cache small however many different ones are sent.
	MaxRejectedSecrets = 1024
)

// encryptedCredential holds encrypted password data
type encryptedCredential struct {
	EncryptedPassword []byte    `json:"encrypted_password"`
	Nonce             []byte    `json:"nonce"`
	ValidatedAt       time.Time `json:"-"`
}

type Manager struct {
	validCredentials map[string]encryptedCredential
	// Recently verified secrets per user, keyed by an HMAC of the secret, with their expiry
	verifiedSecrets map[string]map[string]time.Time
	// Secrets upstream recently rejected, keyed like verifiedSecrets, with their expiry
	rejectedSecrets map[string]time.Time
	mutex           sync.RWMutex
	logger          *logrus.Logger
	client          *http.Client
	upstreamURL     string
	encryptionKey   []byte
}

func New(logger *logrus.Logger, upstreamURL string) *Manager {
	// Generate a random key for this instance
	encryptionKey := generateEncryptionKey()

	return &Manager{
		validCredentials: make(map[string]encryptedCredential),
		verifiedSecrets:  make(map[string]map[string]time.Time),
		rejectedSecrets:  make(map[string]time.Time),
		logger:           logger,
		client:           &http.Client{Timeout: HTTPClientTimeout},
		upstreamURL:      upstreamURL,
		encryptionKey:    encryptionKey,
	}
}

// ValidateAndStore checks the credentials with upstream, unless they were checked recently,
// and stores them for sync. Cancelling ctx abandons the upstream check.
func (cm *Manager) ValidateAndStore(ctx context.Context, username, password string) (bool, error) {
	if username == "" || password == "" {
		err := errors.ErrInvalidCredentials.WithContext("reason", "empty username or password")
		cm.logger.WithError(err).Warn("Invalid credentials provided")
		return false, err
	}

	secretKey := cm.secretKey(username, password)
	if cm.isVerified(username, secretKey) {
		return false, nil // Recently verified, not new
	}

	if cm.isRejected(secretKey) {
		cm.logger.WithField("username", username).Debug("Credentials recently rejected by upstream, not validating again")
		return false, errors.ErrInvalidCredentials.WithContext("username", username).
			WithContext("reason", "recently rejected by upstream")
	}

	if err := cm.validate(ctx, username, password); err != nil {
		// Only upstream's own rejection is remembered. A network failure or unexpected
		// response says nothing about the credentials, so those are tried again next time.
		if errors.IsCode(err, errors.ErrInvalidCredentials.Code) {
			cm.rememberRejected(secretKey, time.Now())
		}
		cm.logger.WithError(err).WithField("username", username).Warn("Invalid credentials provided")
		return false, err
	}

	encryptedCred, err := cm.encryptPassword(password)
	if err != nil {
		return false, errors.Wrap(err, errors.CategoryCredentials, "ENCRYPTION_FAILED", "failed to encrypt password")
	}

	now := time.Now()
	encryptedCred.ValidatedAt = now

	cm.mutex.Lock()
	isNewCredential := len(cm.validCredentials) == 0 || cm.validCredentials[username].EncryptedPassword == nil
	cm.validCredentials[username] = encryptedCred
	cm.rememberVerified(username, secretKey, now)
	cm.mutex.Unlock()

	// Only log when credentials are actually new or changed
	if isNewCredential {
		// Log differently for token vs password auth
		if strings.HasPrefix(password, "TOKEN:") {
			cm.logger.WithField("username", username).Info("Token-based credentials validated and stored")
		} else {
			cm.logger.WithField("username", username).Info("Password-based credentials validated and stored")
		}
	}
	return isNewCredential, nil
}

// secretKey identifies a username/secret pair without keeping the secret itself
func (cm *Manager) secretKey(username, password string) string {
	mac := hmac.New(sha256.New, cm.encryptionKey)
	mac.Write([]byte(username))
	mac.Write([]byte{0})
	mac.Write([]byte(password))
	return string(mac.Sum(nil))
}

// isVerified reports whether upstream confirmed this secret for the user within VerifiedSecretTTL
func (cm *Manager) isVerified(username, secretKey string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	expiry, ok := cm.verifiedSecrets[username][secretKey]
	return ok && time.Now().Before(expiry)
}

// rememberVerified records a secret upstream just confirmed. The caller holds the write lock.
func (cm *Manager) rememberVerified(username, secretKey string, now time.Time) {
	secrets := cm.verifiedSecrets[username]
	if secrets == nil {
		secrets = make(map[string]time.Time)
		cm.verifiedSecrets[username] = secrets
	}

	if len(secrets) >= MaxVerifiedSecretsPerUser {
		var oldestKey string
		var oldestExpiry time.Time
		for key, expiry := range secrets {
			if !now.Before(expiry) {
				delete(secrets, key)
			} else if oldestKey == "" || expiry.Before(oldestExpiry) {
				oldestKey, oldestExpiry = key, expiry
			}
		}
		if len(secrets) >= MaxVerifiedSecretsPerUser {
			delete(secrets, oldestKey)
		}
	}

	secrets[secretKey] = now.Add(VerifiedSecretTTL)
}

// isRejected reports whether upstream rejected this secret within RejectedSecretTTL
func (cm *Manager) isRejected(secretKey string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	expiry, ok := cm.rejectedSecrets[secretKey]
	return ok && time.Now().Before(expiry)
}

// rememberRejected records a secret upstream just rejected. When the cache is full, expired
// entries are dropped first and then the one closest to expiring.
func (cm *Manager) rememberRejected(secretKey string, now time.Time) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if _, exists := cm.rejectedSecrets[secretKey]; !exists && len(cm.rejectedSecrets) >= MaxRejectedSecrets {
		var oldestKey string
		var oldestExpiry time.Time
		for key, expiry := range cm.rejectedSecrets {
			if !now.Before(expiry) {
				delete(cm.rejectedSecrets, key)
			} else if oldestKey == "" || expiry.Before(oldestExpiry) {
				oldestKey, oldestExpiry = key, expiry
			}
		}
		if len(cm.rejectedSecrets) >= MaxRejectedSecrets {
			delete(cm.rejectedSecrets, oldestKey)
		}
	}

	cm.rejectedSecrets[secretKey] = now.Add(RejectedSecretTTL)
}

// HasRecent reports whether the user's stored credential was confirmed by upstream within maxAge
func (cm *Manager) HasRecent(username string, maxAge time.Duration) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	cred, ok := cm.validCredentials[username]
	return ok && time.Since(cred.ValidatedAt) < maxAge
}

func (cm *Manager) validate(ctx context.Context, username, password string) error {
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

	// Check if this is token-based authentication
	if strings.HasPrefix(password, "TOKEN:") {
		// Extract token and salt from the special format: "TOKEN:token:salt"
		parts := strings.Split(password, ":")
		if len(parts) != 3 {
			return errors.ErrInvalidCredentials.WithContext("username", username).
				WithContext("reason", "invalid token format")
		}
		token := parts[1]
		salt := parts[2]

		params.Add("t", token)
		params.Add("s", salt)
		cm.logger.WithField("username", username).Debug("Validating token-based authentication")
	} else {
		// Traditional password-based authentication
		params.Add("p", password)
		cm.logger.WithField("username", username).Debug("Validating password-based authentication")
	}

	params.Add("v", SubsonicAPIVersion)
	params.Add("c", ClientName)
	params.Add("f", "json")
	baseURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return errors.Wrap(err, errors.CategoryCredentials, "VALIDATION_FAILED", "failed to create validation request").
			WithContext("username", username)
	}

	resp, err := cm.client.Do(req)
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

// Get returns the stored credential for one user
func (cm *Manager) Get(username string) (string, bool) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	encryptedCred, ok := cm.validCredentials[username]
	if !ok {
		return "", false
	}
	password, err := cm.decryptPassword(encryptedCred)
	if err != nil {
		cm.logger.WithError(err).WithField("username", username).Warn("Failed to decrypt stored password")
		return "", false
	}
	return password, true
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

// generateEncryptionKey creates a random 32-byte key for AES-256
func generateEncryptionKey() []byte {
	// Use a combination of random bytes and system entropy
	key := make([]byte, AES256KeySize)
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
		Nonce:             nonce,
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
	return base64.StdEncoding.EncodeToString(cm.encryptionKey[:KeyIdentificationBytes]) // Only first 8 bytes for identification
}
