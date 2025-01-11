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

type MammotionCloud struct {
	cloudClient         *aliyuniot.CloudIOTGateway
	isReady             bool
	commandQueue        chan Command
	waitingQueue        *list.List
	mqttMessageEvent    DataEvent
	mqttPropertiesEvent DataEvent
	onReadyEvent        DataEvent
	onDisconnectedEvent DataEvent
	onConnectedEvent    DataEvent
	operationLock       sync.Mutex
	mqttClient          *MammotionMQTT
}

type Command struct {
	iotID   string
	key     string
	command []byte
	future  chan []byte
}

func NewMammotionCloud(mqttClient *MammotionMQTT, cloudClient *aliyuniot.CloudIOTGateway) *MammotionCloud {
	mc := &MammotionCloud{
		cloudClient:         cloudClient,
		commandQueue:        make(chan Command, 100),
		waitingQueue:        list.New(),
		mqttMessageEvent:    NewDataEvent(),
		mqttPropertiesEvent: NewDataEvent(),
		onReadyEvent:        NewDataEvent(),
		onDisconnectedEvent: NewDataEvent(),
		onConnectedEvent:    NewDataEvent(),
		mqttClient:          mqttClient,
	}

	mc.mqttClient.OnConnected = mc.onConnected
	mc.mqttClient.OnDisconnected = mc.onDisconnected
	mc.mqttClient.OnMessage = mc.onMQTTMessage
	mc.mqttClient.OnReady = mc.onReady

	go mc.processQueue()

	return mc
}

func (mc *MammotionCloud) onReady() {
	go mc.processQueue()
	mc.onReadyEvent.Trigger(nil)
}

func (mc *MammotionCloud) IsConnected() bool {
	return mc.mqttClient.IsConnected
}

func (mc *MammotionCloud) Disconnect() {
	mc.mqttClient.Disconnect()
}

func (mc *MammotionCloud) ConnectAsync() {
	mc.mqttClient.ConnectAsync()
}

func (mc *MammotionCloud) SendCommand(iotID string, command []byte) {
	mc.mqttClient.GetCloudClient().SendCloudCommand(iotID, command)
}

func (mc *MammotionCloud) onConnected() {
	mc.onConnectedEvent.Trigger(nil)
}

func (mc *MammotionCloud) onDisconnected() {
	mc.onDisconnectedEvent.Trigger(nil)
}

func (mc *MammotionCloud) processQueue() {
	for {
		select {
		case cmd := <-mc.commandQueue:
			mc.executeCommandLocked(cmd)
		}
	}
}

func (mc *MammotionCloud) executeCommandLocked(cmd Command) {
	mc.operationLock.Lock()
	defer mc.operationLock.Unlock()

	log.Printf("Sending command: %s", cmd.key)
	mc.mqttClient.GetCloudClient().SendCloudCommand(cmd.iotID, cmd.command)

	future := NewMammotionFuture(cmd.iotID)
	mc.waitingQueue.PushBack(future)

	timeout := time.After(5 * time.Second)
	select {
	case notifyMsg := <-future.Result:
		cmd.future <- notifyMsg
	case <-timeout:
		log.Printf("command_locked TimeoutError")
		cmd.future <- []byte{}
	}
}

func (mc *MammotionCloud) onMQTTMessage(topic string, payload []byte, iotID string) {
	log.Printf("MQTT message received on topic %s: %s, iotID: %s", topic, payload, iotID)

	var payloadMap map[string]interface{}
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		log.Printf("Error unmarshalling payload: %v", err)
		return
	}

	mc.handleMQTTMessage(topic, payloadMap)
}

func (mc *MammotionCloud) handleMQTTMessage(topic string, payload map[string]interface{}) {
	mc.parseMQTTResponse(topic, payload)
}

func (mc *MammotionCloud) parseMQTTResponse(topic string, payload map[string]interface{}) {
	if topic == "/app/down/thing/events" {
		log.Printf("Thing event received")
		event, err := mqtt.FromMap(payload)

        if err != nil {
            log.Printf("Error parsing MQTT response: %v", err)
            return
        }
		if event.Type == "device_protobuf_msg_event" && event.Method == "thing.events" {
			log.Printf("Protobuf event")
			mc.mqttMessageEvent.Trigger(event)
		}
		if event.Method == "thing.properties" {
			mc.mqttPropertiesEvent.Trigger(event)
			log.Printf("%v", event)
		}
	}
}

func (mc *MammotionCloud) DequeueByIotID(iotID string) *MammotionFuture {
	for e := mc.waitingQueue.Front(); e != nil; e = e.Next() {
		if e.Value.(*MammotionFuture).IotID == iotID {
			mc.waitingQueue.Remove(e)
			return e.Value.(*MammotionFuture)
		}
	}
	return nil
}

type DataEvent struct {
	subscribers []func(interface{})
}

func NewDataEvent() DataEvent {
	return DataEvent{
		subscribers: make([]func(interface{}), 0),
	}
}

func (de *DataEvent) AddSubscriber(subscriber func(interface{})) {
	de.subscribers = append(de.subscribers, subscriber)
}

func (de *DataEvent) Trigger(data interface{}) {
	for _, subscriber := range de.subscribers {
		subscriber(data)
	}
}

type MammotionFuture struct {
	IotID  string
	Result chan []byte
}

func NewMammotionFuture(iotID string) *MammotionFuture {
	return &MammotionFuture{
		IotID:  iotID,
		Result: make(chan []byte, 1),
	}
}

func (mf *MammotionFuture) Resolve(result []byte) {
	mf.Result <- result
}
