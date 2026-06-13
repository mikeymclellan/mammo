package mammotion

import (
    "sync"
    "time"
    "mammo/data/model"
    "mammo/data/mqtt"
)

type HashListData struct {
	Hashes []int64
}

type MapData struct {
	Type         int32
	Hash         int64
	DataCouple   []float32 // X,Y pairs in meters, full float precision
	TotalFrame   int32
	CurrentFrame int32
	Action       int32
	DataLen      int32
	AreaLabel    string
}

// ZigZagData is one frame of the mower's planned coverage path (the back-and-
// forth mowing route) for the current task, pushed by the device per zone.
type ZigZagData struct {
	JobId        uint64
	CurrentZone  int32
	TotalZoneNum int32
	CurrentFrame int32
	TotalFrame   int32
	CurrentHash  uint64
	DataCouple   []float32 // X,Y pairs in meters
	SubCmd       int32
}

type StateManager struct {
	Device                 *MowingDevice
	LastUpdatedAt          time.Time
	GetHashAckCallback     func(*model.NavGetHashListAck)
	GetCommonDataAckCallback func(interface{})
	OnNotificationCallback func(string, interface{})
	QueueCommandCallback   func(string, map[string]interface{}) ([]byte, error)
	OnPropertiesReceived   func() // Battery/properties callback
	OnPositionUpdate       func(x, y float32, angle int32, posType int32) // Position callback
	OnHashListReceived     func(*HashListData) // Hash list callback
	OnMapDataReceived      func(*MapData) // Map data callback
	OnChargePilePosition   func(toward int32, x, y float32) // Dock position callback
	OnDeviceStatus         func(sysStatus, chargeState int32) // System/charge status callback
	OnZigZagReceived       func(*ZigZagData) // Planned coverage-path frame callback
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

func (sm *StateManager) UpdateBatteryFromProtobuf(batteryLevel int32) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.Device.BatteryPercentage = int(batteryLevel)
	sm.LastUpdatedAt = time.Now()
	if sm.OnPropertiesReceived != nil {
		sm.OnPropertiesReceived()
	}
}

func (sm *StateManager) Notification(message *LubaMsg) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    sm.LastUpdatedAt = time.Now()

    // Battery data is now extracted in parseMessageForDevice and
    // handled via UpdateBatteryFromProtobuf
}
