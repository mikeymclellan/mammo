package mammotion

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"mammo/aliyuniot"
	"os"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MammotionMQTT struct {
	RegionID       string
	ProductKey     string
	DeviceName     string
	DeviceSecret   string
	IotToken       string
	CloudClient    *aliyuniot.CloudIOTGateway
	ClientID       string
	MQTTClientID   string
	MQTTUsername   string
	MQTTPassword   string
	MQTTClient     mqtt.Client
	IsConnected    bool
	IsReady        bool
	OnConnected    func()
	OnReady        func()
	OnError        func(string)
	OnDisconnected func()
	OnMessage      func(topic string, payload []byte, iotID string)
	mu             sync.Mutex
}

// https://www.alibabacloud.com/help/en/iot/use-cases/use-the-paho-mqtt-library-for-go-to-connect-a-device-to-iot-platform
func NewMammotionMQTT(regionID, productKey, deviceName, deviceSecret, iotToken string, cloudClient *aliyuniot.CloudIOTGateway) *MammotionMQTT {
    clientID := cloudClient.ClientID
	if clientID == "" {
		// Add timestamp to ensure unique client ID
		clientID = fmt.Sprintf("golang-%s-%d", deviceName, time.Now().Unix())
	}

    // Use the regionID from the API response (e.g., ap-southeast-1, cn-shanghai, etc.)
    // DO NOT hardcode the region - the iotToken is region-specific!
    auth := calculate_sign(clientID, productKey, deviceName, deviceSecret)
    
    m := &MammotionMQTT{
		RegionID:     regionID,
		ProductKey:   productKey,
		DeviceName:   deviceName,
		DeviceSecret: deviceSecret,
		IotToken:     iotToken,
		CloudClient:  cloudClient,
		ClientID:     clientID,
		MQTTClientID: auth.mqttClientId,
		MQTTUsername: clientID,
		MQTTPassword: auth.password,
	}

    opts := mqtt.NewClientOptions()
    // Now use the actual region since we have the correct ProductKey
    brokerURL := fmt.Sprintf("tls://%s.iot-as-mqtt.%s.aliyuncs.com:8883", productKey, regionID)
    // Connection details (logging disabled for cleaner output)
    opts.AddBroker(brokerURL)
    opts.SetClientID(auth.mqttClientId)
    opts.SetUsername(auth.username)
    opts.SetPassword(auth.password)
    opts.SetCleanSession(true)  // Start with a clean session
    opts.SetKeepAlive(60 * 2 * time.Second)
    opts.SetDefaultPublishHandler(m.OnMessageReceived)
    opts.SetOnConnectHandler(m.OnConnect)
    opts.SetConnectionLostHandler(m.OnDisconnect)
    opts.SetProtocolVersion(4)  // MQTT 3.1.1 (standard version)

    // Enable TLS for securemode=2
    tlsconfig := NewTLSConfig()
    opts.SetTLSConfig(tlsconfig)

    m.MQTTClient = mqtt.NewClient(opts)

    mqtt.ERROR = log.New(os.Stdout, "[ERROR] ", 0)
	mqtt.CRITICAL = log.New(os.Stdout, "[CRIT] ", 0)
	// Disable verbose debug logging
	// mqtt.WARN = log.New(os.Stdout, "[WARN]  ", 0)
	// mqtt.DEBUG = log.New(os.Stdout, "[DEBUG] ", 0)

	return m
}

func (m *MammotionMQTT) ConnectAsync() {

    if (m.MQTTClient.IsConnected()) {
        return
    }
	// Connecting to MQTT broker...
	token := m.MQTTClient.Connect()
	if !token.WaitTimeout(30*time.Second) {
		log.Fatal("Connection timeout after 30 seconds")
	}
	if token.Error() != nil {
        log.Fatal(fmt.Sprintf("Connection error: %s", token.Error()))
	}
    // MQTT connection completed successfully
}

func (m *MammotionMQTT) Disconnect() {
	log.Println("Disconnecting...")
	m.MQTTClient.Disconnect(250)
}

func (m *MammotionMQTT) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) {
	if token := m.MQTTClient.Subscribe(topic, qos, callback); token.WaitTimeout(10*time.Second) && token.Error() != nil {
		log.Fatal(token.Error())
	}
	// Subscription successful (logging disabled for cleaner output)
}

func (m *MammotionMQTT) Publish(topic string, payload interface{}) {
	// Use a buffer with custom encoder to prevent HTML escaping (e.g., & -> \u0026)
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(payload)
	if err != nil {
		log.Fatal(err)
	}
	// Remove trailing newline added by Encode
	data := bytes.TrimSpace(buffer.Bytes())

	if token := m.MQTTClient.Publish(topic, 0, false, data); token.Wait() && token.Error() != nil {
		log.Fatal(token.Error())
	}
}

// PublishRaw publishes raw bytes to a topic (for protobuf messages)
func (m *MammotionMQTT) PublishRaw(topic string, data []byte) error {
	log.Printf("Publishing raw data to topic: %s (%d bytes)", topic, len(data))
	if token := m.MQTTClient.Publish(topic, 0, false, data); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

// SendCommandToDevice sends a protobuf command to a specific device
func (m *MammotionMQTT) SendCommandToDevice(deviceProductKey, deviceName string, commandData []byte) error {
	topic := fmt.Sprintf("/sys/%s/%s/app/up/thing/model/up_raw", deviceProductKey, deviceName)
	return m.PublishRaw(topic, commandData)
}

// SubscribeToDevice subscribes to all relevant topics for a specific device
func (m *MammotionMQTT) SubscribeToDevice(productKey, deviceName string) {
	log.Printf("Subscribing to topics for device: %s (product: %s)", deviceName, productKey)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/events", productKey, deviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/properties", productKey, deviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/model/down_raw", productKey, deviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/status", productKey, deviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/account/bind_reply", productKey, deviceName), 0, m.OnMessageReceived)
}

// BindDevice binds a device to the current session using the iotToken
func (m *MammotionMQTT) BindDevice(productKey, deviceName string) error {
	log.Printf("Binding device: %s (product: %s)", deviceName, productKey)
	bindClientId := fmt.Sprintf("%s&%s", deviceName, productKey)
	bindTopic := fmt.Sprintf("/sys/%s/%s/app/up/account/bind", productKey, deviceName)

	m.Publish(bindTopic, map[string]interface{}{
		"id":      fmt.Sprintf("bind-%s", deviceName),
		"version": "1.0",
		"request": map[string]string{
			"clientId": bindClientId,
		},
		"params": map[string]string{
			"iotToken": m.IotToken,
		},
	})

	// Give it a moment to process
	time.Sleep(500 * time.Millisecond)
	return nil
}

func (m *MammotionMQTT) OnMessageReceived(client mqtt.Client, msg mqtt.Message) {
	// Parse the message
	var payload map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Println("Error unmarshalling payload:", err)
		return
	}

	// Extract iotID if present
	iotID := ""
	if params, ok := payload["params"].(map[string]interface{}); ok {
		if id, ok := params["iotId"].(string); ok {
			iotID = id
		}
	}

	// Call the message handler
	if m.OnMessage != nil {
		m.OnMessage(msg.Topic(), msg.Payload(), iotID)
	}
}

func (m *MammotionMQTT) OnConnect(client mqtt.Client) {
	m.IsConnected = true
	if m.OnConnected != nil {
		m.OnConnected()
	}
	// Use DeviceName in topics, not ClientID
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/account/bind_reply", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/event/property/post_reply", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/wifi/status/notify", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/wifi/connect/event/notify", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/_thing/event/notify", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/events", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/status", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/properties", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)
	m.Subscribe(fmt.Sprintf("/sys/%s/%s/app/down/thing/model/down_raw", m.ProductKey, m.DeviceName), 0, m.OnMessageReceived)

	// Account bind - matching Python exactly
	bindClientId := fmt.Sprintf("%s&%s", m.DeviceName, m.ProductKey)
	m.Publish(fmt.Sprintf("/sys/%s/%s/app/up/account/bind", m.ProductKey, m.DeviceName), map[string]interface{}{
		"id":      "msgid1",
		"version": "1.0",
		"request": map[string]string{
			"clientId": bindClientId,
		},
		"params": map[string]string{
			"iotToken": m.IotToken,
		},
	})

	if m.OnReady != nil {
		m.IsReady = true
		m.OnReady()
	}
}

func (m *MammotionMQTT) SetIotToken(iotToken string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.IotToken = iotToken
}

func (m *MammotionMQTT) OnDisconnect(client mqtt.Client, err error) {
	log.Println("Disconnected")
	m.IsConnected = false
	m.IsReady = false
	if m.OnDisconnected != nil {
		m.OnDisconnected()
	}
}

func (m *MammotionMQTT) GetCloudClient() *aliyuniot.CloudIOTGateway {
	return m.CloudClient
}

type AuthInfo struct {
    password, username, mqttClientId string;
}

func calculate_sign(clientId, productKey, deviceName, deviceSecret string) AuthInfo {
    // Python LinkKit doesn't use timestamp - try without it first
    var raw_passwd bytes.Buffer
    raw_passwd.WriteString("clientId" + clientId)
    raw_passwd.WriteString("deviceName")
    raw_passwd.WriteString(deviceName)
    raw_passwd.WriteString("productKey")
    raw_passwd.WriteString(productKey)

    mac := hmac.New(sha1.New, []byte(deviceSecret))
    mac.Write([]byte(raw_passwd.String()))
    password := fmt.Sprintf("%02x", mac.Sum(nil))
    username := deviceName + "&" + productKey

    var MQTTClientId bytes.Buffer
    MQTTClientId.WriteString(clientId)
    // hmac, use sha1; securemode=2 for TLS on port 8883; NO timestamp (matching Python)
    MQTTClientId.WriteString("|securemode=2,signmethod=hmacsha1|")

    auth := AuthInfo{password:password, username:username, mqttClientId:MQTTClientId.String()}
    return auth
}

func NewTLSConfig() *tls.Config {
    // Use system certificate pool for better compatibility
    certpool, err := x509.SystemCertPool()
    if err != nil {
        log.Printf("Warning: failed to load system cert pool, using empty pool: %v", err)
        certpool = x509.NewCertPool()
    }

    // Try to add Aliyun certificate
    pemCerts, err := ioutil.ReadFile("./x509/aliyun-root.pem")
    if err == nil {
        if ok := certpool.AppendCertsFromPEM([]byte(pemCerts)); !ok {
            log.Printf("Warning: failed to parse Aliyun root certificate")
        }
    } else {
        log.Printf("Warning: No Aliyun certificate file found: %v", err)
    }

    // Also try to add custom certificate if it exists
    pemCerts, err = ioutil.ReadFile("./x509/root.pem")
    if err == nil {
        if ok := certpool.AppendCertsFromPEM([]byte(pemCerts)); !ok {
            log.Printf("Warning: failed to parse custom root certificate")
        }
    }

    // Create tls.Config with desired tls properties
    return &tls.Config{
        // RootCAs = certs used to verify server cert.
        RootCAs: certpool,
        // ClientAuth = whether to request cert from server.
        ClientAuth: tls.NoClientCert,
        // ClientCAs = certs used to validate client cert.
        ClientCAs: nil,
        // TEMPORARILY disable verification to debug connection issue
        InsecureSkipVerify: true,
    }
}


var f mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    fmt.Printf("TOPIC: %s\n", msg.Topic())
    fmt.Printf("MSG: %s\n", msg.Payload())
}
