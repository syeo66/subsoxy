package credentials

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
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

func (cm *Manager) ValidateAndStore(username, password string) {
	cm.mutex.RLock()
	if storedPassword, exists := cm.validCredentials[username]; exists && storedPassword == password {
		cm.mutex.RUnlock()
		return
	}
	cm.mutex.RUnlock()
	
	if cm.validate(username, password) {
		cm.mutex.Lock()
		cm.validCredentials[username] = password
		cm.mutex.Unlock()
		
		cm.logger.WithField("username", username).Info("Credentials validated and stored")
	} else {
		cm.logger.WithField("username", username).Warn("Invalid credentials provided")
	}
}

func (cm *Manager) validate(username, password string) bool {
	url := fmt.Sprintf("%s/rest/ping?u=%s&p=%s&v=1.15.0&c=subsoxy&f=json", 
		cm.upstreamURL, username, password)
	
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	
	resp, err := client.Get(url)
	if err != nil {
		cm.logger.WithError(err).WithField("username", username).Error("Failed to validate credentials")
		return false
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		cm.logger.WithFields(logrus.Fields{
			"username": username,
			"status_code": resp.StatusCode,
		}).Warn("Non-200 response when validating credentials")
		return false
	}
	
	var pingResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&pingResp); err != nil {
		cm.logger.WithError(err).WithField("username", username).Error("Failed to decode ping response")
		return false
	}
	
	if subsonicResp, ok := pingResp["subsonic-response"].(map[string]interface{}); ok {
		if status, ok := subsonicResp["status"].(string); ok {
			if status == "ok" {
				cm.logger.WithField("username", username).Info("Successfully validated credentials")
				return true
			} else {
				cm.logger.WithField("username", username).Warn("Credentials validation failed - invalid username/password")
				return false
			}
		}
	}
	
	cm.logger.WithField("username", username).Error("Invalid response format from upstream server")
	return false
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