package autoconfig

import (
	"automation-hub-nginxconfigmanager/internal/app/config"
	"automation-hub-nginxconfigmanager/internal/app/entities"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/IBM/sarama"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

type ConfigAction int

const (
	Add ConfigAction = iota
	Remove
	Update
)

func manageConfig(action ConfigAction, auto entities.Automation) error {
	log.Printf("Managing config: %v\n", auto)
	if err := auto.Validate(); err != nil {
		return err
	}
	var (
		changed bool
		err     error
	)
	switch action {
	case Add:
		changed, err = addConfig(auto)
	case Remove:
		changed, err = removeConfig(auto.URLPath)
	case Update:
		if auto.OldUrlPath == "" {
			return fmt.Errorf("oldUrlPath is required for update events")
		}
		changed, err = updateConfig(auto)
	default:
		return fmt.Errorf("invalid action")
	}

	if err != nil {
		return err
	}

	if !changed {
		log.Printf("Config already matches requested state for %s; reload skipped", auto.URLPath)
		return nil
	}
	return reloadNginx()
}

func addConfig(auto entities.Automation) (bool, error) {
	filePath, err := configPath(auto.URLPath)
	if err != nil {
		return false, err
	}
	tmpl, err := template.New("config").Parse(configTemplate)
	if err != nil {
		return false, err
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, auto); err != nil {
		return false, err
	}
	if existing, readErr := os.ReadFile(filePath); readErr == nil {
		if bytes.Equal(existing, rendered.Bytes()) {
			return false, nil
		}
	} else if !os.IsNotExist(readErr) {
		return false, readErr
	}

	file, err := os.CreateTemp(filepath.Dir(filePath), "."+filepath.Base(filePath)+".*.tmp")
	if err != nil {
		return false, err
	}
	tempPath := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := file.Write(rendered.Bytes()); err != nil {
		return false, err
	}
	if err := file.Sync(); err != nil {
		return false, err
	}
	if err := file.Chmod(0o644); err != nil {
		return false, err
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tempPath, filePath); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}

func removeConfig(name string) (bool, error) {
	filePath, err := configPath(name)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// The transactional outbox provides at-least-once delivery. A repeated
		// delete has already reached the requested state and is therefore a
		// successful idempotent operation.
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := os.Remove(filePath); err != nil {
		return false, err
	}
	return true, nil
}

func updateConfig(auto entities.Automation) (bool, error) {
	if auto.OldUrlPath == auto.URLPath {
		return addConfig(auto)
	}
	added, err := addConfig(auto)
	if err != nil {
		return false, err
	}
	removed, err := removeConfig(auto.OldUrlPath)
	if err != nil {
		return false, fmt.Errorf("new config written but old config could not be removed: %w", err)
	}
	return added || removed, nil
}

func reloadNginx() error {
	if !config.AppConfig.ReloadEnabled {
		log.Println("NGINX_RELOAD_ENABLED=false; config written but nginx reload skipped")
		return nil
	}
	return fmt.Errorf("NGINX_RELOAD_ENABLED=true is unsupported because Docker socket control is disabled; reload the gateway through an approved deployment operation")
}

func configPath(name string) (string, error) {
	base, err := filepath.Abs(filepath.Clean(config.AppConfig.ConfigDir))
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(base, name+".conf"))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("config path must stay inside %s", base)
	}
	return target, nil
}

func processMessage(msg *sarama.ConsumerMessage) {
	if err := applyMessage(msg); err != nil {
		log.Printf("Failed to process event: %v", err)
	}
}

func applyMessage(msg *sarama.ConsumerMessage) error {
	if msg == nil {
		return fmt.Errorf("Kafka message is required")
	}
	var event entities.AutomationEvent
	err := json.Unmarshal(msg.Value, &event)
	if err != nil {
		return fmt.Errorf("decode automation event: %w", err)
	}
	if event.Automation == nil {
		return fmt.Errorf("%s event has no automation payload", event.Type)
	}

	switch event.Type {
	case entities.CreateEvent:
		err = manageConfig(Add, *event.Automation)
	case entities.UpdateEvent:
		err = manageConfig(Update, *event.Automation)
	case entities.DeleteEvent:
		err = manageConfig(Remove, *event.Automation)
	default:
		return fmt.Errorf("unknown event type: %s", event.Type)
	}
	return err
}
