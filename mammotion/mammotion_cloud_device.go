package mammotion

import (
	"encoding/base64"
	"fmt"
	"log"
	"sync"
	"time"

	mqtt "mammo/data/mqtt"
	pb "mammo/proto"
	"google.golang.org/protobuf/proto"
)

type MammotionBaseCloudDevice struct {
	mqtt                *MammotionCloud
	device              *MowingDevice
	stateManager        *StateManager
	bleSyncTask         *time.Timer
	stopped             bool
	onReadyCallback     func() error
	commandFutures      map[string]chan []byte
	commands            *MammotionCommand
	currentID           string
	operationLock       sync.Mutex
}

func NewMammotionBaseCloudDevice(mqtt *MammotionCloud, device *MowingDevice, stateManager *StateManager) *MammotionBaseCloudDevice {
	mbcd := &MammotionBaseCloudDevice{
		mqtt:           mqtt,
		device:         device,
		stateManager:   stateManager,
		commandFutures: make(map[string]chan []byte),
		commands:       NewMammotionCommand(device.iotDevice.DeviceName),
	}

	device.mqttMessageEvent.AddSubscriber(mbcd.parseMessageForDevice)
	mqtt.mqttPropertiesEvent.AddSubscriber(mbcd.parseMessagePropertiesForDevice)
	mqtt.onReadyEvent.AddSubscriber(mbcd.onReady)
	mqtt.onDisconnectedEvent.AddSubscriber(mbcd.onDisconnect)
	mqtt.onConnectedEvent.AddSubscriber(mbcd.onConnect)

	if mqtt.isReady {
		mbcd.runPeriodicSyncTask()
	}

	return mbcd
}

func (mbcd *MammotionBaseCloudDevice) onReady(data interface{}) {
	if mbcd.stopped {
		return
	}
	if mbcd.onReadyCallback != nil {
		err := mbcd.onReadyCallback()
		if err != nil {
			log.Printf("Device is offline: %v", err)
		}
	}
}

func (mbcd *MammotionBaseCloudDevice) onDisconnect(data interface{}) {
	if mbcd.bleSyncTask != nil {
		mbcd.bleSyncTask.Stop()
	}
	mbcd.mqtt.Disconnect()
}

func (mbcd *MammotionBaseCloudDevice) onConnect(data interface{}) {
	mbcd.bleSync()
	if mbcd.bleSyncTask == nil || mbcd.bleSyncTask.Stop() {
		mbcd.runPeriodicSyncTask()
	}
}

func (mbcd *MammotionBaseCloudDevice) Stop() {
	if mbcd.bleSyncTask != nil {
		mbcd.bleSyncTask.Stop()
	}
	mbcd.stopped = true
}

func (mbcd *MammotionBaseCloudDevice) Start() {
	mbcd.bleSync()
	if mbcd.bleSyncTask == nil || mbcd.bleSyncTask.Stop() {
		mbcd.runPeriodicSyncTask()
	}
	mbcd.stopped = false
	if !mbcd.mqtt.IsConnected() {
		mbcd.mqtt.ConnectAsync()
	}
}

func (mbcd *MammotionBaseCloudDevice) bleSync() {
	commandBytes := mbcd.commands.SendToDevBleSync(3)
	mbcd.mqtt.SendCommand(mbcd.device.iotDevice.IotId, commandBytes)
}

func (mbcd *MammotionBaseCloudDevice) runPeriodicSyncTask() {
	if !mbcd.operationLock.TryLock() || !mbcd.stopped {
		mbcd.bleSync()
	}
	if !mbcd.stopped {
		mbcd.scheduleBleSync()
	}
}

func (mbcd *MammotionBaseCloudDevice) scheduleBleSync() {
	if mbcd.mqtt != nil && mbcd.mqtt.IsConnected() {
		mbcd.bleSyncTask = time.AfterFunc(160*time.Second, func() {
			mbcd.runPeriodicSyncTask()
		})
	}
}

func (mbcd *MammotionBaseCloudDevice) QueueCommand(key string, kwargs map[string]interface{}) ([]byte, error) {
	future := make(chan []byte)
	commandBytes := mbcd.commands.GetCommandBytes(key, kwargs)
	mbcd.mqtt.commandQueue <- Command{
		iotID:   mbcd.device.iotDevice.IotId,
		key:     key,
		command: commandBytes,
		future:  future,
	}
	select {
	case result := <-future:
		return result, nil
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("command timeout")
	}
}

func (mbcd *MammotionBaseCloudDevice) parseMessageForDevice(event interface{}) {
	thingEventMessage, ok := event.(*mqtt.ThingEventMessage)
	if !ok {
		log.Printf("Failed to cast event to *mqtt.ThingEventMessage")
		return
	}

	var iotID, deviceName, productKey string
	var valueContent string

	switch params := thingEventMessage.Params.(type) {
	case mqtt.DeviceProtobufMsgEventParams:
		iotID = params.IotId
		deviceName = params.DeviceName
		productKey = params.ProductKey
		valueContent = params.Value.Content
	default:
		// try to get general params
		if generalParams, ok := thingEventMessage.Params.(mqtt.GeneralParams); ok {
			iotID = generalParams.IotId
			deviceName = generalParams.DeviceName
			productKey = generalParams.ProductKey
			if val, ok := generalParams.Value.(map[string]interface{}); ok {
				if content, ok := val["content"].(string); ok {
					valueContent = content
				}
			}
		} else {
			log.Printf("Unknown event params type: %T", thingEventMessage.Params)
			return
		}
	}

	if iotID != mbcd.device.iotDevice.IotId {
		return
	}

	binaryData, err := base64.StdEncoding.DecodeString(valueContent)
	if err != nil {
		log.Printf("Error decoding message: %v", err)
		return
	}
	mbcd.updateRawData(binaryData)

	// Parse the protobuf message
	var lubaMsg pb.LubaMsg
	err = proto.Unmarshal(binaryData, &lubaMsg)
	if err != nil {
		log.Printf("Error parsing protobuf message: %v", err)
		return
	}

	// Extract battery data and position from system messages
	if sys := lubaMsg.GetSys(); sys != nil {
		if reportData := sys.GetToappReportData(); reportData != nil {
			// Extract battery level
			if devStatus := reportData.GetDev(); devStatus != nil {
				batteryLevel := devStatus.GetBatteryVal()
				mbcd.stateManager.UpdateBatteryFromProtobuf(batteryLevel)
			}

			// Extract position data
			if locations := reportData.GetLocations(); len(locations) > 0 {
				// Use the first location (most recent)
				loc := locations[0]
				x := float32(loc.GetRealPosX())
				y := float32(loc.GetRealPosY())
				angle := loc.GetRealToward()

				if mbcd.stateManager.OnPositionUpdate != nil {
					mbcd.stateManager.OnPositionUpdate(x, y, angle)
				}
			}
		}
	}

	if mbcd.commands.GetDeviceProductKey() == "" && mbcd.commands.GetDeviceName() == deviceName {
		mbcd.commands.SetDeviceProductKey(productKey)
	}
	if mbcd.mqtt.waitingQueue.Len() > 0 {
		fut := mbcd.mqtt.DequeueByIotID(mbcd.device.iotDevice.IotId)
		if fut != nil {
			fut.Resolve(binaryData)
		}
	}

	// Still call the placeholder Notification for compatibility
	newMsg := LubaMsg{}
	newMsg.Parse(binaryData)
	mbcd.stateManager.Notification(&newMsg)
}

func (mbcd *MammotionBaseCloudDevice) parseMessagePropertiesForDevice(event interface{}) {
	thingPropertiesMessage, ok := event.(*mqtt.ThingPropertiesMessage)
	if !ok {
		log.Printf("Failed to cast event to *mqtt.ThingPropertiesMessage")
		return
	}
	if thingPropertiesMessage.Params.IotID != mbcd.device.iotDevice.IotId {
		return
	}
	mbcd.stateManager.Properties(thingPropertiesMessage)
}

func (mbcd *MammotionBaseCloudDevice) updateRawData(data []byte) {
	// Implement this method
}
