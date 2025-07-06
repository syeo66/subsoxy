package credentials

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/syeo66/subsoxy/errors"
)

type Manager struct {
	validCredentials map[string]string
	mutex            sync.RWMutex
	logger           *logrus.Logger
	upstreamURL      string
}

func New(logger *logrus.Logger, upstreamURL string) *Manager {
	return &Manager{
		validCredentials: make(map[string]string),
		logger:           logger,
		upstreamURL:      upstreamURL,
	}
}

func (cm *Manager) ValidateAndStore(username, password string) error {
	if username == "" || password == "" {
		err := errors.ErrInvalidCredentials.WithContext("reason", "empty username or password")
		cm.logger.WithError(err).Warn("Invalid credentials provided")
		return err
	}

	cm.mutex.RLock()
	if storedPassword, exists := cm.validCredentials[username]; exists && storedPassword == password {
		cm.mutex.RUnlock()
		return nil
	}
	cm.mutex.RUnlock()
	
	if err := cm.validate(username, password); err != nil {
		cm.logger.WithError(err).WithField("username", username).Warn("Invalid credentials provided")
		return err
	}

	cm.mutex.Lock()
	cm.validCredentials[username] = password
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
	
	for username, password := range cm.validCredentials {
		return username, password
	}
	
	return "", ""
}

func (cm *Manager) ClearInvalid() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	
	if len(cm.validCredentials) > 0 {
		cm.logger.Warn("Clearing potentially invalid credentials")
		cm.validCredentials = make(map[string]string)
	}
}