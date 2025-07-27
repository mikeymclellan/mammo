package mammotion

import (
	"log"
	"sync"

	aliyuniot "mammo/aliyuniot"
	mqtt "mammo/data/mqtt"
)

type MowingDevice struct {
	iotDevice           aliyuniot.Device
	cloudGateway        aliyuniot.CloudIOTGateway
	mammoCloud          *MammotionCloud
	loop                *sync.Mutex
	isReady             bool
	mqttMessageEvent    DataEvent
	mqttPropertiesEvent DataEvent
	onReadyEvent        DataEvent
	onDisconnectedEvent DataEvent
	onConnectedEvent    DataEvent
	operationLock       sync.Mutex
	MQTTProperties      mqtt.PropertyParams
}

func NewMowingDevice(iotDevice *aliyuniot.Device, cloudGateway aliyuniot.CloudIOTGateway, mammoCloud *MammotionCloud) *MowingDevice {
	device := new(MowingDevice)
	device.iotDevice = *iotDevice
	device.cloudGateway = cloudGateway
	device.loop = &sync.Mutex{}
	device.mqttMessageEvent = NewDataEvent()
	device.mqttPropertiesEvent = NewDataEvent()
	device.onReadyEvent = NewDataEvent()
	device.onDisconnectedEvent = NewDataEvent()
	device.onConnectedEvent = NewDataEvent()
	device.mammoCloud = mammoCloud

	device.mammoCloud.onReadyEvent.AddSubscriber(device.onReady)
	device.mammoCloud.mqttMessageEvent.AddSubscriber(device.onMQTTMessage)

	return device
}

func (d *MowingDevice) GetMammoCloud() *MammotionCloud {
	return d.mammoCloud
}

func (d *MowingDevice) onReady(data interface{}) {
	d.isReady = true
	d.onReadyEvent.Trigger(nil)
}

func (d *MowingDevice) IsConnected() bool {
	return d.mammoCloud.IsConnected()
}

func (d *MowingDevice) Disconnect() {
	d.mammoCloud.Disconnect()
}

func (d *MowingDevice) onConnected() {
	d.onConnectedEvent.Trigger(nil)
}

func (d *MowingDevice) onDisconnected() {
	d.onDisconnectedEvent.Trigger(nil)
}

func (d *MowingDevice) onMQTTMessage(data interface{}) {
	event, ok := data.(*mqtt.ThingEventMessage)
	if !ok {
		log.Printf("Error: invalid event type")
		return
	}
	d.mqttMessageEvent.Trigger(event)
}

