package mammotion

import (
	"container/list"
	"encoding/base64"
	"log"
	"sync"
	"time"
	mqtt "mammo/data/mqtt"
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
		commands:       NewMammotionCommand(device.DeviceName),
	}

	mqtt.mqttMessageEvent.AddSubscriber(mbcd.parseMessageForDevice)
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
	mbcd.mqtt.SendCommand(mbcd.device.IotID, commandBytes)
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
		iotID:   mbcd.device.IotID,
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
	thingEventMessage := event.(mqtt.ThingEventMessage)
	if thingEventMessage.Params.IotID != mbcd.device.IotID {
		return
	}
	binaryData, err := base64.StdEncoding.DecodeString(thingEventMessage.Params.Value.Content)
	if err != nil {
		log.Printf("Error decoding message: %v", err)
		return
	}
	mbcd.updateRawData(binaryData)
	newMsg := LubaMsg{}
	err = newMsg.Parse(binaryData)
	if err != nil {
		log.Printf("Error parsing message: %v", err)
		return
	}
	if mbcd.commands.GetDeviceProductKey() == "" && mbcd.commands.GetDeviceName() == thingEventMessage.Params.DeviceName {
		mbcd.commands.SetDeviceProductKey(thingEventMessage.Params.ProductKey)
	}
	if len(mbcd.mqtt.waitingQueue) > 0 {
		fut := mbcd.dequeueByIotID(mbcd.mqtt.waitingQueue, mbcd.device.IotID)
		if fut == nil {
			return
		}
		for fut.future == nil && len(mbcd.mqtt.waitingQueue) > 0 {
			fut = mbcd.dequeueByIotID(mbcd.mqtt.waitingQueue, mbcd.device.IotID)
		}
		if fut.future != nil {
			fut.Resolve(binaryData)
		}
	}
	mbcd.stateManager.Notification(newMsg)
}

func (mbcd *MammotionBaseCloudDevice) parseMessagePropertiesForDevice(event interface{}) {
	thingPropertiesMessage := event.(mqtt.ThingPropertiesMessage)
	if thingPropertiesMessage.Params.IotID != mbcd.device.IotID {
		return
	}
	mbcd.stateManager.Properties(thingPropertiesMessage)
}

func (mbcd *MammotionBaseCloudDevice) dequeueByIotID(queue *list.List, iotID string) *MammotionFuture {
	for e := queue.Front(); e != nil; e = e.Next() {
		if e.Value.(*MammotionFuture).IotID == iotID {
			queue.Remove(e)
			return e.Value.(*MammotionFuture)
		}
	}
	return nil
}
