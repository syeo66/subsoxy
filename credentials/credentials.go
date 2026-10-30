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
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/syeo66/subsoxy/errors"
	"github.com/syeo66/subsoxy/models"
)

// Encryption constants
const (
	AES256KeySize          = 32
	KeyIdentificationBytes = 8
	HTTPClientTimeout      = 10 * time.Second
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

// Credential is the secret a Subsonic client authenticates with: a password, or a token
// (the MD5 of password and salt) together with its salt
type Credential struct {
	Password string `json:"p,omitempty"`
	Token    string `json:"t,omitempty"`
	Salt     string `json:"s,omitempty"`
}

// Password returns a password credential
func Password(password string) Credential {
	return Credential{Password: password}
}

// Token returns a token credential
func Token(token, salt string) Credential {
	return Credential{Token: token, Salt: salt}
}

// IsToken reports whether the credential is a token rather than a password
func (c Credential) IsToken() bool {
	return c.Password == ""
}

// IsZero reports whether the credential has nothing to authenticate with
func (c Credential) IsZero() bool {
	return c.Password == "" && (c.Token == "" || c.Salt == "")
}

// AddTo sets the credential's authentication parameters: "p", or "t" and "s"
func (c Credential) AddTo(params url.Values) {
	if c.IsToken() {
		params.Set("t", c.Token)
		params.Set("s", c.Salt)
	} else {
		params.Set("p", c.Password)
	}
}

// encryptedCredential holds an encrypted Credential
type encryptedCredential struct {
	Ciphertext  []byte
	Nonce       []byte
	ValidatedAt time.Time
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
func (cm *Manager) ValidateAndStore(ctx context.Context, username string, cred Credential) (bool, error) {
	if username == "" || cred.IsZero() {
		err := errors.ErrInvalidCredentials.WithContext("reason", "empty username or password")
		cm.logger.WithError(err).Warn("Invalid credentials provided")
		return false, err
	}

	secretKey := cm.secretKey(username, cred)
	if cm.isVerified(username, secretKey) {
		return false, nil // Recently verified, not new
	}

	if cm.isRejected(secretKey) {
		cm.logger.WithField("username", username).Debug("Credentials recently rejected by upstream, not validating again")
		return false, errors.ErrInvalidCredentials.WithContext("username", username).
			WithContext("reason", "recently rejected by upstream")
	}

	if err := cm.validate(ctx, username, cred); err != nil {
		// Only upstream's own rejection is remembered. A network failure or unexpected
		// response says nothing about the credentials, so those are tried again next time.
		if errors.IsCode(err, errors.ErrInvalidCredentials.Code) {
			cm.rememberRejected(secretKey, time.Now())
		}
		cm.logger.WithError(err).WithField("username", username).Warn("Invalid credentials provided")
		return false, err
	}

	encryptedCred, err := cm.encrypt(cred)
	if err != nil {
		return false, errors.Wrap(err, errors.CategoryCredentials, "ENCRYPTION_FAILED", "failed to encrypt credential")
	}

	now := time.Now()
	encryptedCred.ValidatedAt = now

	cm.mutex.Lock()
	_, known := cm.validCredentials[username]
	isNewCredential := !known
	cm.validCredentials[username] = encryptedCred
	cm.rememberVerified(username, secretKey, now)
	cm.mutex.Unlock()

	// Only log when credentials are actually new or changed
	if isNewCredential {
		// Log differently for token vs password auth
		if cred.IsToken() {
			cm.logger.WithField("username", username).Info("Token-based credentials validated and stored")
		} else {
			cm.logger.WithField("username", username).Info("Password-based credentials validated and stored")
		}
	}
	return isNewCredential, nil
}

// secretKey identifies a username/secret pair without keeping the secret itself
func (cm *Manager) secretKey(username string, cred Credential) string {
	mac := hmac.New(sha256.New, cm.encryptionKey)
	mac.Write([]byte(username))
	mac.Write([]byte{0})
	// The JSON encoding keeps a password and a token with the same text apart
	encoded, _ := json.Marshal(cred)
	mac.Write(encoded)
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

// Forget handles upstream rejecting a credential it accepted before, e.g. after a password
// change. The credential is no longer trusted as verified, is refused like any rejected one
// for RejectedSecretTTL, and is removed from storage if it's still the user's stored one, so
// a newer credential stored meanwhile is kept. It reports whether the stored one was removed.
func (cm *Manager) Forget(username string, cred Credential) bool {
	secretKey := cm.secretKey(username, cred)

	cm.mutex.Lock()
	delete(cm.verifiedSecrets[username], secretKey)
	removed := false
	if stored, ok := cm.validCredentials[username]; ok {
		if storedCred, err := cm.decrypt(stored); err != nil || storedCred == cred {
			delete(cm.validCredentials, username)
			removed = true
		}
	}
	cm.mutex.Unlock()

	cm.rememberRejected(secretKey, time.Now())
	return removed
}

// HasRecent reports whether the user's stored credential was confirmed by upstream within maxAge
func (cm *Manager) HasRecent(username string, maxAge time.Duration) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	cred, ok := cm.validCredentials[username]
	return ok && time.Since(cred.ValidatedAt) < maxAge
}

func (cm *Manager) validate(ctx context.Context, username string, cred Credential) error {
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

	cred.AddTo(params)
	if cred.IsToken() {
		cm.logger.WithField("username", username).Debug("Validating token-based authentication")
	} else {
		cm.logger.WithField("username", username).Debug("Validating password-based authentication")
	}

	params.Add("v", models.SubsonicAPIVersion)
	params.Add("c", models.ClientName)
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
func (cm *Manager) Get(username string) (Credential, bool) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	encryptedCred, ok := cm.validCredentials[username]
	if !ok {
		return Credential{}, false
	}
	cred, err := cm.decrypt(encryptedCred)
	if err != nil {
		cm.logger.WithError(err).WithField("username", username).Warn("Failed to decrypt stored credential")
		return Credential{}, false
	}
	return cred, true
}

// GetAllValid returns all valid credentials as a map of username to credential
func (cm *Manager) GetAllValid() map[string]Credential {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	validCreds := make(map[string]Credential)
	for username, encryptedCred := range cm.validCredentials {
		if cred, err := cm.decrypt(encryptedCred); err == nil {
			validCreds[username] = cred
		} else {
			// If decryption fails, skip this credential
			cm.logger.WithError(err).WithField("username", username).Warn("Failed to decrypt stored credential")
		}
	}

	return validCreds
}

// generateEncryptionKey creates a random 32-byte key for AES-256
func generateEncryptionKey() []byte {
	key := make([]byte, AES256KeySize)
	// Never fails: crypto/rand crashes the program if the system can't supply randomness
	rand.Read(key)
	return key
}

// encrypt encrypts a credential using AES-256-GCM
func (cm *Manager) encrypt(cred Credential) (encryptedCredential, error) {
	plaintext, err := json.Marshal(cred)
	if err != nil {
		return encryptedCredential{}, err
	}

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

	return encryptedCredential{
		Ciphertext: gcm.Seal(nil, nonce, plaintext, nil),
		Nonce:      nonce,
	}, nil
}

// decrypt decrypts a credential using AES-256-GCM
func (cm *Manager) decrypt(encrypted encryptedCredential) (Credential, error) {
	block, err := aes.NewCipher(cm.encryptionKey)
	if err != nil {
		return Credential{}, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Credential{}, err
	}

	plaintext, err := gcm.Open(nil, encrypted.Nonce, encrypted.Ciphertext, nil)
	if err != nil {
		return Credential{}, err
	}

	var cred Credential
	if err := json.Unmarshal(plaintext, &cred); err != nil {
		return Credential{}, err
	}
	return cred, nil
}

// GetEncryptionInfo returns information about the encryption setup (for testing)
func (cm *Manager) GetEncryptionInfo() string {
	return base64.StdEncoding.EncodeToString(cm.encryptionKey[:KeyIdentificationBytes]) // Only first 8 bytes for identification
}
