package model

type DeviceInfo struct {
    DeviceID string
    Info     map[string]string
}

func NewDeviceInfo(deviceID string, info map[string]string) *DeviceInfo {
    return &DeviceInfo{
        DeviceID: deviceID,
        Info:     info,
    }
}

func (di *DeviceInfo) GetInfo() map[string]string {
    return di.Info
}
