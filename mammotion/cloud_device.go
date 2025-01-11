package mammotion

import (
    "container/list"
    "encoding/json"
    "log"
    "sync"
    "time"

    aliyuniot "mammo/aliyuniot"
    mqtt "mammo/data/mqtt"
)

type MowingDevice struct {
    iotDevice     aliyuniot.Device
    cloudGateway  aliyuniot.CloudIOTGateway
    mammoCloud    *MammotionCloud
    loop          *sync.Mutex
    isReady       bool
    commandQueue  chan Command
    waitingQueue  *list.List
    mqttMessageEvent    DataEvent
    mqttPropertiesEvent DataEvent
    onReadyEvent        DataEvent
    onDisconnectedEvent DataEvent
    onConnectedEvent    DataEvent
    operationLock       sync.Mutex
    mqttClient          *MammotionMQTT
}

func NewMowingDevice(iotDevice *aliyuniot.Device, cloudGateway aliyuniot.CloudIOTGateway) *MowingDevice {
    device := new(MowingDevice)
    device.iotDevice = *iotDevice
    device.cloudGateway = cloudGateway
    device.loop = &sync.Mutex{}
    device.commandQueue = make(chan Command, 100)
    device.waitingQueue = list.New()
    device.mqttMessageEvent = NewDataEvent()
    device.mqttPropertiesEvent = NewDataEvent()
    device.onReadyEvent = NewDataEvent()
    device.onDisconnectedEvent = NewDataEvent()
    device.onConnectedEvent = NewDataEvent()

    device.init()
    return device
}

func (d *MowingDevice) init() {

    mqttClient := NewMammotionMQTT(
        d.cloudGateway.RegionResponse.Data.RegionId,
        d.cloudGateway.AepResponse.Data.ProductKey,
        d.cloudGateway.AepResponse.Data.DeviceName,
        d.cloudGateway.AepResponse.Data.DeviceSecret,
        d.cloudGateway.SessionByAuthCodeResponse.Data.IotToken,
        &d.cloudGateway,
    )
    d.mammoCloud = NewMammotionCloud(mqttClient, &d.cloudGateway)
    d.mammoCloud.ConnectAsync()
}

func (d *MowingDevice) onReady() {
    go d.processQueue()
    d.onReadyEvent.Trigger(nil)
}

func (d *MowingDevice) IsConnected() bool {
    return d.mammoCloud.IsConnected()
}

func (d *MowingDevice) Disconnect() {
    d.mammoCloud.Disconnect()
}

func (d *MowingDevice) ConnectAsync() {
    d.mammoCloud.ConnectAsync()
}

func (d *MowingDevice) SendCommand(iotID string, command []byte) {
    d.mammoCloud.SendCommand(iotID, command)
}

func (d *MowingDevice) onConnected() {
    d.onConnectedEvent.Trigger(nil)
}

func (d *MowingDevice) onDisconnected() {
    d.onDisconnectedEvent.Trigger(nil)
}

func (d *MowingDevice) processQueue() {
    for {
        select {
        case cmd := <-d.commandQueue:
            d.executeCommandLocked(cmd)
        }
    }
}

func (d *MowingDevice) executeCommandLocked(cmd Command) {
    d.operationLock.Lock()
    defer d.operationLock.Unlock()

    log.Printf("Sending command: %s", cmd.key)
    d.mammoCloud.SendCommand(cmd.iotID, cmd.command)

    future := NewMammotionFuture(cmd.iotID)
    d.waitingQueue.PushBack(future)

    timeout := time.After(5 * time.Second)
    select {
    case notifyMsg := <-future.Result:
        cmd.future <- notifyMsg
    case <-timeout:
        log.Printf("command_locked TimeoutError")
        cmd.future <- []byte{}
    }
}

func (d *MowingDevice) onMQTTMessage(topic string, payload []byte, iotID string) {
    log.Printf("MQTT message received on topic %s: %s, iotID: %s", topic, payload, iotID)

    var payloadMap map[string]interface{}
    if err := json.Unmarshal(payload, &payloadMap); err != nil {
        log.Printf("Error unmarshalling payload: %v", err)
        return
    }

    d.handleMQTTMessage(topic, payloadMap)
}

func (d *MowingDevice) handleMQTTMessage(topic string, payload map[string]interface{}) {
    d.parseMQTTResponse(topic, payload)
}

func (d *MowingDevice) parseMQTTResponse(topic string, payload map[string]interface{}) {
    if topic == "/app/down/thing/events" {
        log.Printf("Thing event received")
        event, err := mqtt.FromMap(payload)

        if err != nil {
            log.Printf("Error parsing event: %v", err)
            return
        }
        if event.Type == "device_protobuf_msg_event" && event.Method == "thing.events" {
            log.Printf("Protobuf event")
            d.mqttMessageEvent.Trigger(event)
        }
        if event.Method == "thing.properties" {
            d.mqttPropertiesEvent.Trigger(event)
            log.Printf("%v", event)
        }
    }
}

func (d *MowingDevice) DequeueByIotID(iotID string) *MammotionFuture {
    for e := d.waitingQueue.Front(); e != nil; e = e.Next() {
        if e.Value.(*MammotionFuture).IotID == iotID {
            d.waitingQueue.Remove(e)
            return e.Value.(*MammotionFuture)
        }
    }
    return nil
}

