package mammotion

import (
    "sync"
    "time"
    "mammo/data/model"
    "mammo/data/mqtt"
)

type StateManager struct {
	Device                 *MowingDevice
	LastUpdatedAt          time.Time
	GetHashAckCallback     func(*model.NavGetHashListAck)
	GetCommonDataAckCallback func(interface{})
	OnNotificationCallback func(string, interface{})
	QueueCommandCallback   func(string, map[string]interface{}) ([]byte, error)
	OnPropertiesReceived   func() // New callback
	mu                     sync.Mutex
}

func NewStateManager(device *MowingDevice) *StateManager {
    return &StateManager{
        Device:        device,
        LastUpdatedAt: time.Now(),
    }
}

func (sm *StateManager) GetDevice() *MowingDevice {
    return sm.Device
}

func (sm *StateManager) SetDevice(device *MowingDevice) {
    sm.Device = device
}

func (sm *StateManager) Properties(properties *mqtt.ThingPropertiesMessage) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.Device.MQTTProperties = properties.Params
	if val, ok := properties.Params.Items.BatteryPercentage.Value.(float64); ok {
		sm.Device.BatteryPercentage = int(val)
		if sm.OnPropertiesReceived != nil {
			sm.OnPropertiesReceived()
		}
	}
}

func (sm *StateManager) Notification(message *LubaMsg) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    sm.LastUpdatedAt = time.Now()

    // The original python code has a dispatcher here, but we'll just do a switch for now
    // This is not fully implemented
}
