package model

type DeviceConfig struct {
    DeviceID string
    Config   map[string]string
}

func NewDeviceConfig(deviceID string, config map[string]string) *DeviceConfig {
    return &DeviceConfig{
        DeviceID: deviceID,
        Config:   config,
    }
}

func (dc *DeviceConfig) GetConfig() map[string]string {
    return dc.Config
}
