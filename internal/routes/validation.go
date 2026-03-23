package routes

import (
	"encoding/json"
	"fmt"
	"strings"

	domain "github.com/slidebolt/sb-domain"
)

func validateSegment(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.Contains(value, ".") {
		return fmt.Errorf("%s must not contain '.'", name)
	}
	return nil
}

func validateDeviceBody(body json.RawMessage, path DeviceKey) (domain.Device, error) {
	var dev domain.Device
	if err := json.Unmarshal(body, &dev); err != nil {
		return domain.Device{}, fmt.Errorf("decode device: %w", err)
	}
	if err := validateSegment("plugin", path.Plugin); err != nil {
		return domain.Device{}, err
	}
	if err := validateSegment("id", path.ID); err != nil {
		return domain.Device{}, err
	}
	if dev.Plugin == "" || dev.ID == "" || dev.Name == "" {
		return domain.Device{}, fmt.Errorf("device requires plugin, id, and name")
	}
	if dev.Plugin != path.Plugin || dev.ID != path.ID {
		return domain.Device{}, fmt.Errorf("device path/body mismatch")
	}
	if err := validateSegment("plugin", dev.Plugin); err != nil {
		return domain.Device{}, err
	}
	if err := validateSegment("id", dev.ID); err != nil {
		return domain.Device{}, err
	}
	return dev, nil
}

func validateEntityBody(body json.RawMessage, path EntityKey) (domain.Entity, error) {
	var ent domain.Entity
	if err := json.Unmarshal(body, &ent); err != nil {
		return domain.Entity{}, fmt.Errorf("decode entity: %w", err)
	}
	if err := validateSegment("plugin", path.Plugin); err != nil {
		return domain.Entity{}, err
	}
	if err := validateSegment("deviceID", path.DeviceID); err != nil {
		return domain.Entity{}, err
	}
	if err := validateSegment("id", path.EntityID); err != nil {
		return domain.Entity{}, err
	}
	if ent.Plugin == "" || ent.DeviceID == "" || ent.ID == "" || ent.Type == "" || ent.Name == "" {
		return domain.Entity{}, fmt.Errorf("entity requires plugin, deviceID, id, type, and name")
	}
	if ent.Plugin != path.Plugin || ent.DeviceID != path.DeviceID || ent.ID != path.EntityID {
		return domain.Entity{}, fmt.Errorf("entity path/body mismatch")
	}
	if err := validateSegment("plugin", ent.Plugin); err != nil {
		return domain.Entity{}, err
	}
	if err := validateSegment("deviceID", ent.DeviceID); err != nil {
		return domain.Entity{}, err
	}
	if err := validateSegment("id", ent.ID); err != nil {
		return domain.Entity{}, err
	}
	return ent, nil
}
